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
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

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
	// We don't persist the full sandbox DSN; reconstruct it from the runner
	// when needed. The mock runner can rebuild it; the field on the drill is
	// the bare DB name for human-readable logging.
	//
	// In Phase 2 the LocalRunner is the only real runner; we ask it to build
	// the DSN. To keep this code runner-agnostic, the runner.Runner contract
	// doesn't expose a "rehydrate sandbox" — instead each worker passes the
	// sandbox object forward via the next step's args... but that's a lot of
	// args. Compromise: the LocalRunner has a public RehydrateSandbox method.
	if lr, ok := d.Runner.(*runner.LocalRunner); ok {
		return lr.Rehydrate(drillID, *dr.SandboxDB)
	}
	return nil, errors.New("runner does not support rehydration")
}

// failAndCleanup is called by any step that errors. It marks the step failed
// and enqueues teardown with the failure reason so the sandbox is cleaned up.
func (d Deps) failAndCleanup(ctx context.Context, drillID uuid.UUID, step drill.StepName, reason string) error {
	_ = d.Store.MarkStepFailed(ctx, drillID, step, reason)
	for _, later := range stepsAfter(step) {
		_ = d.Store.MarkStepSkipped(ctx, drillID, later)
	}
	_, err := d.Inserter.Insert(ctx, drill.TeardownArgs{
		DrillID:       drillID,
		FailureReason: fmt.Sprintf("%s: %s", step, reason),
	}, nil)
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
		return w.D.failAndCleanup(ctx, drillID, drill.StepProvision, err.Error())
	}
	if err := w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepProvision); err != nil {
		return err
	}
	return w.chain(ctx, drillID)
}

func (w *ProvisionWorker) chain(ctx context.Context, drillID uuid.UUID) error {
	_, err := w.D.Inserter.Insert(ctx, drill.FetchArgs{DrillID: drillID}, nil)
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
		return w.D.failAndCleanup(ctx, drillID, drill.StepFetch, err.Error())
	}

	sb, err := w.D.sandbox(ctx, drillID)
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepFetch, err.Error())
	}

	localPath, err := w.D.Runner.Fetch(ctx, sb, target.SourceURI)
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepFetch, err.Error())
	}

	if err := w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepFetch); err != nil {
		return err
	}
	_, _ = dr, target
	_, err = w.D.Inserter.Insert(ctx, drill.RestoreArgs{DrillID: drillID, FilePath: localPath}, nil)
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
	localPath, err := w.D.Runner.Fetch(ctx, sb, target.SourceURI)
	if err != nil {
		return err
	}
	_, err = w.D.Inserter.Insert(ctx, drill.RestoreArgs{DrillID: drillID, FilePath: localPath}, nil)
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
	ctx, endSpan := obs.StartSpan(ctx, "drill.restore", map[string]string{"drill.id": drillID.String()})
	defer endSpan()
	if done, err := w.D.alreadyDone(ctx, drillID, drill.StepRestore); err != nil {
		return err
	} else if done {
		_, err := w.D.Inserter.Insert(ctx, drill.AssertArgs{DrillID: drillID}, nil)
		return err
	}
	if err := w.D.Store.MarkStepRunning(ctx, drillID, drill.StepRestore); err != nil {
		return err
	}

	sb, err := w.D.sandbox(ctx, drillID)
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepRestore, err.Error())
	}
	if err := w.D.Runner.Restore(ctx, sb, job.Args.FilePath); err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepRestore, err.Error())
	}

	if err := w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepRestore); err != nil {
		return err
	}
	_, err = w.D.Inserter.Insert(ctx, drill.AssertArgs{DrillID: drillID}, nil)
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
	ctx, endSpan := obs.StartSpan(ctx, "drill.assert", map[string]string{"drill.id": drillID.String()})
	defer endSpan()
	if done, err := w.D.alreadyDone(ctx, drillID, drill.StepAssert); err != nil {
		return err
	} else if done {
		_, err := w.D.Inserter.Insert(ctx, drill.ReportArgs{DrillID: drillID}, nil)
		return err
	}
	if err := w.D.Store.MarkStepRunning(ctx, drillID, drill.StepAssert); err != nil {
		return err
	}

	_, target, err := w.D.lookupDrillAndTarget(ctx, drillID)
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepAssert, err.Error())
	}
	sb, err := w.D.sandbox(ctx, drillID)
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepAssert, err.Error())
	}

	kind, exp, act, passed, err := assertions.RowCount(ctx, w.D.Runner, sb, assertions.RowCountSpec{
		Table:   target.AssertionTable,
		MinRows: target.AssertionMinRows,
	})
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepAssert, err.Error())
	}
	if err := w.D.Store.RecordAssertion(ctx, drill.AssertionResult{
		DrillID: drillID, Kind: kind, Expected: exp, Actual: act, Passed: passed,
	}); err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepAssert, err.Error())
	}

	if !passed {
		_ = w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepAssert)
		// Assertion failure is a drill failure but we still want a PDF that
		// records it, so chain to report (it will produce a FAILED verdict)
		// and let report mark the drill failed.
		_, err := w.D.Inserter.Insert(ctx, drill.ReportArgs{DrillID: drillID}, nil)
		return err
	}

	if err := w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepAssert); err != nil {
		return err
	}
	_, err = w.D.Inserter.Insert(ctx, drill.ReportArgs{DrillID: drillID}, nil)
	return err
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
	ctx, endSpan := obs.StartSpan(ctx, "drill.report", map[string]string{"drill.id": drillID.String()})
	defer endSpan()
	if done, err := w.D.alreadyDone(ctx, drillID, drill.StepReport); err != nil {
		return err
	} else if done {
		_, err := w.D.Inserter.Insert(ctx, drill.TeardownArgs{DrillID: drillID}, nil)
		return err
	}
	if err := w.D.Store.MarkStepRunning(ctx, drillID, drill.StepReport); err != nil {
		return err
	}

	dr, target, err := w.D.lookupDrillAndTarget(ctx, drillID)
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepReport, err.Error())
	}
	steps, err := w.D.Store.ListSteps(ctx, drillID)
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepReport, err.Error())
	}
	ars, err := w.D.Store.ListAssertions(ctx, drillID)
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepReport, err.Error())
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
	path, err := w.D.Evidence.Finalize(ctx, drillID, buf.Bytes())
	if err != nil {
		return w.D.failAndCleanup(ctx, drillID, drill.StepReport, err.Error())
	}

	if err := w.D.Store.MarkStepSucceeded(ctx, drillID, drill.StepReport); err != nil {
		return err
	}

	if !verdictPass {
		// Mark the drill failed now; teardown still runs to clean up.
		_ = w.D.Store.MarkDrillFailed(ctx, drillID, "one or more assertions failed")
		// Record the evidence path so the user can download the failure PDF.
		_ = w.D.Store.MarkEvidence(ctx, drillID, path)
		acct := dr.AccountID
		_ = w.D.Audit.Record(ctx, audit.Event{
			AccountID: &acct,
			ActorID:   &dr.CreatedByUserID, Action: "drill.failed",
			TargetKind: "drill", TargetID: drillID.String(),
			Metadata: map[string]any{"reason": "assertion_failed"},
		})
		_, err := w.D.Inserter.Insert(ctx, drill.TeardownArgs{
			DrillID: drillID, FailureReason: "assertion_failed",
		}, nil)
		return err
	}

	// Pass path: stash evidence path now so the user can download the moment
	// the next worker hops.
	_ = w.D.Store.MarkEvidence(ctx, drillID, path)
	_, err = w.D.Inserter.Insert(ctx, drill.TeardownArgs{DrillID: drillID}, nil)
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
	ctx, endSpan := obs.StartSpan(ctx, "drill.teardown", map[string]string{"drill.id": drillID.String()})
	defer endSpan()

	dr, err := w.D.Store.GetDrillByID(ctx, drillID)
	if err != nil {
		return err
	}

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
