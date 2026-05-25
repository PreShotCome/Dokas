// Package steps defines the River worker for each drill step. Each step is
// its own job kind so retries, timeouts, and failure handling happen per
// step rather than per drill.
//
// Idempotency model: every worker first re-reads the step row. If it's
// already terminal (succeeded), the worker no-ops and just chains the next
// step. If it's pending or running, it does the work and writes the result.
// This makes job retries (River's built-in retry on transient failure) safe
// even though the underlying work (CREATE DATABASE, pg_restore) isn't.
package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preshotcome/anything/internal/account"
	"github.com/preshotcome/anything/internal/analytics"
	"github.com/preshotcome/anything/internal/assertions"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/drill"
	"github.com/preshotcome/anything/internal/evidence"
	"github.com/preshotcome/anything/internal/obs"
	"github.com/preshotcome/anything/internal/report"
	"github.com/preshotcome/anything/internal/runner"
	"github.com/preshotcome/anything/internal/webhooks"
)

// Deps is the bundle of dependencies every step worker needs.
type Deps struct {
	Store    *drill.Store
	Runner   runner.Runner
	Inserter drill.RiverInserter
	Audit    *audit.Logger
	// Evidence stores + signs the rendered report PDF.
	Evidence *evidence.Service
	// Webhooks fans drill.completed/drill.failed out to customer endpoints.
	// Optional: nil disables webhook dispatch (e.g. in unit tests).
	Webhooks *webhooks.Dispatcher
	// Metrics records drill outcomes. Optional: nil disables metric
	// recording (e.g. in unit tests).
	Metrics *obs.Metrics
	// Analytics captures drill funnel events. Optional: nil disables it.
	Analytics analytics.Analytics
	// Billing reports billable drill usage to the billing provider.
	// Optional: nil disables usage reporting.
	Billing UsageReporter
	// Accounts resolves an account's Stripe customer for usage reporting.
	// Optional: nil disables usage reporting.
	Accounts AccountLookup
}

// UsageReporter records billable drill usage with the billing provider.
// billing.Service satisfies it.
type UsageReporter interface {
	ReportUsage(ctx context.Context, customerID, identifier string) error
	Enabled() bool
}

// AccountLookup resolves an account to its billing identity. account.Store
// satisfies it.
type AccountLookup interface {
	GetAccount(ctx context.Context, id uuid.UUID) (account.Account, error)
}

// reportUsage records one billable drill, best-effort. A drill is one unit
// of metered usage; the Stripe meter handles any included allowance. The
// drill ID is the dedup identifier, so a teardown retry counts once. A
// billing hiccup must never fail a drill, so the error is swallowed.
func (d Deps) reportUsage(ctx context.Context, dr drill.Drill) {
	if d.Billing == nil || d.Accounts == nil || !d.Billing.Enabled() {
		return
	}
	acct, err := d.Accounts.GetAccount(ctx, dr.AccountID)
	if err != nil || acct.StripeCustomerID == nil || *acct.StripeCustomerID == "" {
		return
	}
	_ = d.Billing.ReportUsage(ctx, *acct.StripeCustomerID, dr.ID.String())
}

// captureDrill records a drill funnel event, if analytics is wired.
func (d Deps) captureDrill(event string, dr drill.Drill) {
	if d.Analytics == nil {
		return
	}
	d.Analytics.Capture(dr.AccountID.String(), event, map[string]any{
		"drill_id":  dr.ID.String(),
		"target_id": dr.TargetID.String(),
	})
}

// recordDrillMetric records a terminal drill outcome, if metrics are wired.
func (d Deps) recordDrillMetric(status string, dr drill.Drill) {
	if d.Metrics == nil {
		return
	}
	var dur time.Duration
	if dr.StartedAt != nil {
		dur = time.Since(*dr.StartedAt)
	}
	d.Metrics.RecordDrill(status, dur)
}

// Register attaches every step worker to the given River workers registry.
// Call once at startup before creating the River client.
func Register(workers *river.Workers, d Deps) {
	river.AddWorker(workers, &ProvisionWorker{D: d})
	river.AddWorker(workers, &FetchWorker{D: d})
	river.AddWorker(workers, &RestoreWorker{D: d})
	river.AddWorker(workers, &AssertWorker{D: d})
	river.AddWorker(workers, &ReportWorker{D: d})
	river.AddWorker(workers, &TeardownWorker{D: d})
}

// ---- helpers shared across workers ----

func (d Deps) sandbox(ctx context.Context, drillID uuid.UUID) (*runner.Sandbox, error) {
	dr, err := d.Store.GetDrillByID(ctx, drillID)
	if err != nil {
		return nil, err
	}
	if dr.SandboxDB == nil || *dr.SandboxDB == "" {
		return nil, errors.New("sandbox db not recorded")
	}
	// The full sandbox DSN isn't persisted — only the bare DB name. Ask the
	// runner to rebuild the handle from it; Rehydrate is part of the Runner
	// contract, so this stays runner-agnostic.
	return d.Runner.Rehydrate(drillID, *dr.SandboxDB)
}

// failAndCleanup terminates a drill on a *genuine* step failure: it marks the
// step failed and enqueues teardown so the sandbox is cleaned up.
//
// It is only for failures of the step's real work (Provision/Fetch/Restore,
// assertion queries, report rendering). Transient infrastructure errors —
// store reads/writes, sandbox rehydration — are instead returned raw from the
// worker so River retries them, rather than flipping a drill permanently
// failed on a momentary database blip.
func (d Deps) failAndCleanup(ctx context.Context, drillID uuid.UUID, step drill.StepName, reason string) error {
	_ = d.Store.MarkStepFailed(ctx, drillID, step, reason)
	for _, later := range stepsAfter(step) {
		_ = d.Store.MarkStepSkipped(ctx, drillID, later)
	}
	_, err := d.Inserter.Insert(ctx, drill.TeardownArgs{
		DrillID:       drillID,
		FailureReason: fmt.Sprintf("%s: %s", step, reason),
	}, drill.TraceOpts(ctx))
	if err != nil {
		return fmt.Errorf("enqueue teardown after failure: %w", err)
	}
	return nil
}

func stepsAfter(name drill.StepName) []drill.StepName {
	out := []drill.StepName{}
	seen := false
	for _, s := range drill.AllSteps {
		if seen && s != drill.StepTeardown {
			out = append(out, s)
		}
		if s == name {
			seen = true
		}
	}
	return out
}

// alreadyDone returns true if the step is already in a terminal-success state.
// We treat "succeeded" as the only terminal-skip-the-work case so retries
// after partial failure still re-attempt.
func (d Deps) alreadyDone(ctx context.Context, drillID uuid.UUID, name drill.StepName) (bool, error) {
	step, err := d.Store.GetStep(ctx, drillID, name)
	if err != nil {
		return false, err
	}
	return step.Status == drill.StatusSucceeded, nil
}

// ---- Provision ----

type ProvisionWorker struct {
	river.WorkerDefaults[drill.ProvisionArgs]
	D Deps
}

func (w *ProvisionWorker) Work(ctx context.Context, job *river.Job[drill.ProvisionArgs]) error {
	drillID := job.Args.DrillID
	ctx = drill.ContextFromJobMeta(ctx, job.Metadata)
	ctx, endSpan := obs.StartSpan(ctx, "drill.provision", map[string]string{"drill.id": drillID.String()})
	defer endSpan()
	if done, err := w.D.alreadyDone(ctx, drillID, drill.StepProvision); err != nil {
		return err
	} else if done {
		return w.chain(ctx, drillID)
	}
	if err := w.D.Store.MarkDrillRunning(ctx, drillID); err != nil {
		return err
	}
	if err := w.D.Store.MarkStepRunning(ctx, drillID, drill.StepProvision); err != nil {
		return err
	}

	sb, err := w.D.Runner.Provision(ctx, drillID)
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepProvision, err.Error())
	}
	if err := w.D.Store.SetSandboxDB(ctx, drillID, sb.Name); err != nil {
		// Persisting the sandbox name failed — without it, a River retry
		// of this job would call Provision again and spawn a second
		// machine, leaving the first one running forever. Tear the just-
		// provisioned sandbox down before bubbling the transient error.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		_ = w.D.Runner.Teardown(cleanupCtx, sb)
		cancel()
		return err
	}
	if err := w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepProvision); err != nil {
		return err
	}
	return w.chain(ctx, drillID)
}

func (w *ProvisionWorker) chain(ctx context.Context, drillID uuid.UUID) error {
	_, err := w.D.Inserter.Insert(ctx, drill.FetchArgs{DrillID: drillID}, drill.TraceOpts(ctx))
	return err
}

// Timeout: provisioning is fast (CREATE DATABASE) — 30s is generous.
func (w *ProvisionWorker) Timeout(*river.Job[drill.ProvisionArgs]) time.Duration {
	return 30 * time.Second
}

// ---- Fetch ----

type FetchWorker struct {
	river.WorkerDefaults[drill.FetchArgs]
	D Deps
}

func (w *FetchWorker) Work(ctx context.Context, job *river.Job[drill.FetchArgs]) error {
	drillID := job.Args.DrillID
	ctx = drill.ContextFromJobMeta(ctx, job.Metadata)
	ctx, endSpan := obs.StartSpan(ctx, "drill.fetch", map[string]string{"drill.id": drillID.String()})
	defer endSpan()
	if done, err := w.D.alreadyDone(ctx, drillID, drill.StepFetch); err != nil {
		return err
	} else if done {
		return w.chainAlreadyFetched(ctx, drillID)
	}
	if err := w.D.Store.MarkStepRunning(ctx, drillID, drill.StepFetch); err != nil {
		return err
	}

	dr, target, err := w.D.lookupDrillAndTarget(ctx, drillID)
	if err != nil {
		return err // transient store read — let River retry
	}

	sb, err := w.D.sandbox(ctx, drillID)
	if err != nil {
		return err // transient store read — let River retry
	}

	localPath, sourceHash, err := w.D.Runner.Fetch(ctx, sb, target.SourceURI)
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepFetch, err.Error())
	}
	// Persist the input anchor of the evidence chain. A SetSourceHash
	// failure here is transient (DB blip); River will retry the whole
	// fetch step — which is safe because Fetch + hashDump are pure.
	if err := w.D.Store.SetSourceHash(ctx, drillID, sourceHash); err != nil {
		return err
	}

	if err := w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepFetch); err != nil {
		return err
	}
	_, _ = dr, target
	_, err = w.D.Inserter.Insert(ctx, drill.RestoreArgs{DrillID: drillID, FilePath: localPath}, drill.TraceOpts(ctx))
	return err
}

// chainAlreadyFetched re-runs fetch (cheap) to recover the local path, then
// enqueues restore. Used on retry of the chain after a crash mid-flight.
func (w *FetchWorker) chainAlreadyFetched(ctx context.Context, drillID uuid.UUID) error {
	_, target, err := w.D.lookupDrillAndTarget(ctx, drillID)
	if err != nil {
		return err
	}
	sb, err := w.D.sandbox(ctx, drillID)
	if err != nil {
		return err
	}
	localPath, _, err := w.D.Runner.Fetch(ctx, sb, target.SourceURI)
	if err != nil {
		return err
	}
	_, err = w.D.Inserter.Insert(ctx, drill.RestoreArgs{DrillID: drillID, FilePath: localPath}, drill.TraceOpts(ctx))
	return err
}

func (w *FetchWorker) Timeout(*river.Job[drill.FetchArgs]) time.Duration {
	return 5 * time.Minute
}

// ---- Restore ----

type RestoreWorker struct {
	river.WorkerDefaults[drill.RestoreArgs]
	D Deps
}

func (w *RestoreWorker) Work(ctx context.Context, job *river.Job[drill.RestoreArgs]) error {
	drillID := job.Args.DrillID
	ctx = drill.ContextFromJobMeta(ctx, job.Metadata)
	ctx, endSpan := obs.StartSpan(ctx, "drill.restore", map[string]string{"drill.id": drillID.String()})
	defer endSpan()
	if done, err := w.D.alreadyDone(ctx, drillID, drill.StepRestore); err != nil {
		return err
	} else if done {
		_, err := w.D.Inserter.Insert(ctx, drill.AssertArgs{DrillID: drillID}, drill.TraceOpts(ctx))
		return err
	}
	if err := w.D.Store.MarkStepRunning(ctx, drillID, drill.StepRestore); err != nil {
		return err
	}

	sb, err := w.D.sandbox(ctx, drillID)
	if err != nil {
		return err // transient store read — let River retry
	}
	output, restoreErr := w.D.Runner.Restore(ctx, sb, job.Args.FilePath)
	// Persist the captured output regardless of pass/fail — a failed
	// restore's output is the most important evidence of WHY it failed.
	if logErr := w.D.Store.SetStepOutput(ctx, drillID, drill.StepRestore, output); logErr != nil {
		// Best-effort; don't fail the step on a logging blip.
		// The signed PDF will simply omit the snippet.
		_ = logErr
	}
	if restoreErr != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepRestore, restoreErr.Error())
	}

	if err := w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepRestore); err != nil {
		return err
	}
	_, err = w.D.Inserter.Insert(ctx, drill.AssertArgs{DrillID: drillID}, drill.TraceOpts(ctx))
	return err
}

func (w *RestoreWorker) Timeout(*river.Job[drill.RestoreArgs]) time.Duration {
	return 30 * time.Minute
}

// ---- Assert ----

type AssertWorker struct {
	river.WorkerDefaults[drill.AssertArgs]
	D Deps
}

func (w *AssertWorker) Work(ctx context.Context, job *river.Job[drill.AssertArgs]) error {
	drillID := job.Args.DrillID
	ctx = drill.ContextFromJobMeta(ctx, job.Metadata)
	ctx, endSpan := obs.StartSpan(ctx, "drill.assert", map[string]string{"drill.id": drillID.String()})
	defer endSpan()
	if done, err := w.D.alreadyDone(ctx, drillID, drill.StepAssert); err != nil {
		return err
	} else if done {
		_, err := w.D.Inserter.Insert(ctx, drill.ReportArgs{DrillID: drillID}, drill.TraceOpts(ctx))
		return err
	}
	if err := w.D.Store.MarkStepRunning(ctx, drillID, drill.StepAssert); err != nil {
		return err
	}

	_, target, err := w.D.lookupDrillAndTarget(ctx, drillID)
	if err != nil {
		return err // transient store read — let River retry
	}
	specs, err := w.D.Store.ListTargetAssertions(ctx, target.ID)
	if err != nil {
		return err // transient store read — let River retry
	}
	sb, err := w.D.sandbox(ctx, drillID)
	if err != nil {
		return err // transient store read — let River retry
	}

	if err := w.runAssertions(ctx, drillID, sb, specs); err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepAssert, err.Error())
	}

	// The assert step itself succeeds whenever every assertion *ran*; a failed
	// assertion is a drill-verdict concern, computed by the report worker from
	// the recorded assertion_results rows.
	if err := w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepAssert); err != nil {
		return err
	}
	_, err = w.D.Inserter.Insert(ctx, drill.ReportArgs{DrillID: drillID}, drill.TraceOpts(ctx))
	return err
}

// runAssertions dials the restored sandbox and runs every configured
// assertion, recording one assertion_results row each. Prior results for the
// drill are cleared first so a step retry stays idempotent.
func (w *AssertWorker) runAssertions(ctx context.Context, drillID uuid.UUID, sb *runner.Sandbox, specs []drill.Assertion) error {
	if err := w.D.Store.ClearAssertionResults(ctx, drillID); err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, sb.DSN)
	if err != nil {
		return fmt.Errorf("connect sandbox: %w", err)
	}
	defer conn.Close(ctx)
	q := pgxQuerier{conn: conn}

	for _, spec := range specs {
		var cfg map[string]any
		if err := json.Unmarshal(spec.Config, &cfg); err != nil {
			return fmt.Errorf("assertion %s: bad config: %w", spec.ID, err)
		}
		out, err := assertions.Run(ctx, q, assertions.Spec{Kind: spec.Kind, Config: cfg})
		if err != nil {
			return fmt.Errorf("assertion %s (%s): %w", spec.ID, spec.Kind, err)
		}
		if err := w.D.Store.RecordAssertion(ctx, drill.AssertionResult{
			DrillID: drillID, Kind: out.Kind,
			Expected: out.Expected, Actual: out.Actual, Passed: out.Passed,
		}); err != nil {
			return err
		}
	}
	return nil
}

// pgxQuerier adapts a *pgx.Conn to the assertions.Querier interface.
type pgxQuerier struct{ conn *pgx.Conn }

func (q pgxQuerier) QueryRow(ctx context.Context, sql string, args ...any) interface {
	Scan(dest ...any) error
} {
	return q.conn.QueryRow(ctx, sql, args...)
}

func (q pgxQuerier) Query(ctx context.Context, sql string, args ...any) (assertions.Rows, error) {
	rows, err := q.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgxRows{rows: rows}, nil
}

// pgxRows narrows pgx.Rows to the minimal surface assertions.Rows needs.
type pgxRows struct{ rows pgx.Rows }

func (r pgxRows) Next() bool                  { return r.rows.Next() }
func (r pgxRows) Values() ([]any, error)      { return r.rows.Values() }
func (r pgxRows) Err() error                  { return r.rows.Err() }
func (r pgxRows) Close()                      { r.rows.Close() }
func (r pgxRows) FieldDescriptions() []assertions.FieldDescription {
	fds := r.rows.FieldDescriptions()
	out := make([]assertions.FieldDescription, len(fds))
	for i, f := range fds {
		out[i] = assertions.FieldDescription{Name: string(f.Name)}
	}
	return out
}

func (w *AssertWorker) Timeout(*river.Job[drill.AssertArgs]) time.Duration {
	return 5 * time.Minute
}

// ---- Report ----

type ReportWorker struct {
	river.WorkerDefaults[drill.ReportArgs]
	D Deps
}

func (w *ReportWorker) Work(ctx context.Context, job *river.Job[drill.ReportArgs]) error {
	drillID := job.Args.DrillID
	ctx = drill.ContextFromJobMeta(ctx, job.Metadata)
	ctx, endSpan := obs.StartSpan(ctx, "drill.report", map[string]string{"drill.id": drillID.String()})
	defer endSpan()
	if done, err := w.D.alreadyDone(ctx, drillID, drill.StepReport); err != nil {
		return err
	} else if done {
		_, err := w.D.Inserter.Insert(ctx, drill.TeardownArgs{DrillID: drillID}, drill.TraceOpts(ctx))
		return err
	}
	if err := w.D.Store.MarkStepRunning(ctx, drillID, drill.StepReport); err != nil {
		return err
	}

	dr, target, err := w.D.lookupDrillAndTarget(ctx, drillID)
	if err != nil {
		return err // transient store read — let River retry
	}
	steps, err := w.D.Store.ListSteps(ctx, drillID)
	if err != nil {
		return err // transient store read — let River retry
	}
	ars, err := w.D.Store.ListAssertions(ctx, drillID)
	if err != nil {
		return err // transient store read — let River retry
	}

	// Determine verdict from assertions: any failed assertion → drill failed.
	verdictPass := true
	for _, a := range ars {
		if !a.Passed {
			verdictPass = false
			break
		}
	}
	// Reflect verdict in the in-memory drill so the PDF header renders right.
	if !verdictPass {
		s := drill.StatusFailed
		dr.Status = s
		msg := "one or more assertions failed"
		dr.Error = &msg
	}

	// Render the PDF to memory, then hand it to the evidence service, which
	// stores it and records a detached signature + retention horizon.
	var buf bytes.Buffer
	if err := report.Render(&buf, report.Data{
		Drill: dr, Target: target, Steps: steps, Assertions: ars,
		GeneratedAt: time.Now().UTC(),
	}); err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepReport, err.Error())
	}
	path, err := w.D.Evidence.Finalize(ctx, drillID, dr.AccountID, buf.Bytes())
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepReport, err.Error())
	}

	// Record the evidence path *before* marking the step succeeded. If this
	// write fails the step stays unfinished and River retries, so teardown
	// can never mark a drill succeeded with an empty evidence_path.
	if err := w.D.Store.MarkEvidence(ctx, drillID, path); err != nil {
		return err
	}
	if err := w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepReport); err != nil {
		return err
	}

	if !verdictPass {
		// Mark the drill failed now; teardown still runs to clean up.
		_ = w.D.Store.MarkDrillFailed(ctx, drillID, "one or more assertions failed")
		acct := dr.AccountID
		_ = w.D.Audit.Record(ctx, audit.Event{
			AccountID: &acct,
			ActorID:   &dr.CreatedByUserID, Action: "drill.failed",
			TargetKind: "drill", TargetID: drillID.String(),
			Metadata: map[string]any{"reason": "assertion_failed"},
		})
		_, err := w.D.Inserter.Insert(ctx, drill.TeardownArgs{
			DrillID: drillID, FailureReason: "assertion_failed",
		}, drill.TraceOpts(ctx))
		return err
	}

	_, err = w.D.Inserter.Insert(ctx, drill.TeardownArgs{DrillID: drillID}, drill.TraceOpts(ctx))
	return err
}

func (w *ReportWorker) Timeout(*river.Job[drill.ReportArgs]) time.Duration {
	return 1 * time.Minute
}

// ---- Teardown ----

type TeardownWorker struct {
	river.WorkerDefaults[drill.TeardownArgs]
	D Deps
}

func (w *TeardownWorker) Work(ctx context.Context, job *river.Job[drill.TeardownArgs]) error {
	drillID := job.Args.DrillID
	ctx = drill.ContextFromJobMeta(ctx, job.Metadata)
	ctx, endSpan := obs.StartSpan(ctx, "drill.teardown", map[string]string{"drill.id": drillID.String()})
	defer endSpan()

	dr, err := w.D.Store.GetDrillByID(ctx, drillID)
	if err != nil {
		return err
	}

	// A drill that reached teardown ran — record one unit of metered usage.
	// Best-effort and idempotent (deduped on the drill ID).
	w.D.reportUsage(ctx, dr)

	// Best-effort: even if rehydration fails (sandbox never provisioned),
	// the runner.Teardown of a nil sandbox is a no-op.
	var sb *runner.Sandbox
	if dr.SandboxDB != nil && *dr.SandboxDB != "" {
		sb, _ = w.D.sandbox(ctx, drillID)
	}
	if err := w.D.Runner.Teardown(ctx, sb); err != nil {
		// Teardown failure is logged but doesn't poison the workflow; we
		// already have the evidence file written. Mark the step failed so
		// it's visible in the report, but treat the drill as successful if
		// every prior step passed.
		_ = w.D.Store.MarkStepFailed(ctx, drillID, drill.StepTeardown, err.Error())
	} else {
		_ = w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepTeardown)
	}

	// Final drill status:
	//   - failure_reason set → drill is failed (already may be marked, no-op)
	//   - else if any step failed → failed
	//   - else → succeeded
	acct := dr.AccountID
	if job.Args.FailureReason != "" {
		_ = w.D.Store.MarkDrillFailed(ctx, drillID, job.Args.FailureReason)
		_ = w.D.Audit.Record(ctx, audit.Event{
			AccountID: &acct,
			ActorID:   &dr.CreatedByUserID, Action: "drill.failed",
			TargetKind: "drill", TargetID: drillID.String(),
			Metadata: map[string]any{"reason": job.Args.FailureReason},
		})
		w.D.recordDrillMetric("failed", dr)
		w.D.captureDrill(analytics.EventDrillFailed, dr)
		w.D.dispatchWebhook(ctx, acct, "drill.failed", map[string]any{
			"drill_id": drillID.String(),
			"reason":   job.Args.FailureReason,
		})
		return nil
	}
	if dr.Status == drill.StatusFailed {
		// Already marked by report worker for assertion failures.
		w.D.recordDrillMetric("failed", dr)
		w.D.captureDrill(analytics.EventDrillFailed, dr)
		w.D.dispatchWebhook(ctx, acct, "drill.failed", map[string]any{
			"drill_id": drillID.String(),
			"reason":   "assertion_failed",
		})
		return nil
	}

	// Evidence path was set in the report step. Re-read.
	dr2, err := w.D.Store.GetDrillByID(ctx, drillID)
	if err != nil {
		return err
	}
	evidence := ""
	if dr2.EvidencePath != nil {
		evidence = *dr2.EvidencePath
	}
	if err := w.D.Store.MarkDrillSucceeded(ctx, drillID, evidence); err != nil {
		return err
	}
	_ = w.D.Audit.Record(ctx, audit.Event{
		AccountID: &acct,
		ActorID:   &dr.CreatedByUserID, Action: "drill.completed",
		TargetKind: "drill", TargetID: drillID.String(),
	})
	w.D.recordDrillMetric("succeeded", dr)
	w.D.captureDrill(analytics.EventDrillCompleted, dr)
	w.D.dispatchWebhook(ctx, acct, "drill.completed", map[string]any{
		"drill_id": drillID.String(),
		"status":   "succeeded",
	})
	return nil
}

// dispatchWebhook fans a drill event out to the account's webhook endpoints.
// Best-effort: webhook problems must not fail the drill, so errors are
// swallowed (the delivery log surfaces them on its own).
func (d Deps) dispatchWebhook(ctx context.Context, accountID uuid.UUID, event string, data map[string]any) {
	if d.Webhooks == nil {
		return
	}
	_ = d.Webhooks.Dispatch(ctx, accountID, event, data)
}

func (w *TeardownWorker) Timeout(*river.Job[drill.TeardownArgs]) time.Duration {
	return 1 * time.Minute
}

// ---- shared lookups ----

func (d Deps) lookupDrillAndTarget(ctx context.Context, drillID uuid.UUID) (drill.Drill, drill.Target, error) {
	dr, err := d.Store.GetDrillByID(ctx, drillID)
	if err != nil {
		return drill.Drill{}, drill.Target{}, err
	}
	t, err := d.Store.GetTargetByID(ctx, dr.TargetID)
	if err != nil {
		return drill.Drill{}, drill.Target{}, err
	}
	return dr, t, nil
}

// Suppress unused import (rivertype) — kept for future Middleware hookups.
var _ = rivertype.JobRow{}
