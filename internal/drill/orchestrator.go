package drill

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preshotcome/anything/internal/audit"
)

// --- Job args (one per step). Each is its own River job kind so retries,
// metrics, and queue routing happen per step rather than per drill.

type ProvisionArgs struct {
	DrillID uuid.UUID `json:"drill_id"`
}
type FetchArgs struct {
	DrillID uuid.UUID `json:"drill_id"`
}
type RestoreArgs struct {
	DrillID  uuid.UUID `json:"drill_id"`
	FilePath string    `json:"file_path"`
}
type AssertArgs struct {
	DrillID uuid.UUID `json:"drill_id"`
}
type ReportArgs struct {
	DrillID uuid.UUID `json:"drill_id"`
}
type TeardownArgs struct {
	DrillID uuid.UUID `json:"drill_id"`
	// FailureReason, when set, marks the drill failed after teardown runs.
	// It's how earlier steps signal "clean up and stop" without leaking
	// sandbox resources.
	FailureReason string `json:"failure_reason,omitempty"`
}

func (ProvisionArgs) Kind() string { return "drill.provision" }
func (FetchArgs) Kind() string     { return "drill.fetch" }
func (RestoreArgs) Kind() string   { return "drill.restore" }
func (AssertArgs) Kind() string    { return "drill.assert" }
func (ReportArgs) Kind() string    { return "drill.report" }
func (TeardownArgs) Kind() string  { return "drill.teardown" }

// RiverInserter is the small subset of *river.Client we actually use. The
// orchestrator and step workers go through this interface so tests can swap
// in a fake without standing up a real River client.
type RiverInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// Orchestrator records the step rows for a new drill, audits the creation,
// and enqueues the provision job. Every subsequent step is enqueued by the
// previous step's worker.
type Orchestrator struct {
	store  *Store
	client RiverInserter
	audit  *audit.Logger
}

func NewOrchestrator(store *Store, client RiverInserter, auditLog *audit.Logger) *Orchestrator {
	return &Orchestrator{store: store, client: client, audit: auditLog}
}

func (o *Orchestrator) Store() *Store         { return o.store }
func (o *Orchestrator) Client() RiverInserter { return o.client }

// EnqueueDrill is called after the drill row exists. It seeds the six
// drill_steps rows as pending (so the UI can render them immediately),
// records a drill.created audit event, and inserts the first River job.
// Idempotent on the step rows; the audit event will fire each call, so
// callers must not invoke it for an existing reused drill.
func (o *Orchestrator) EnqueueDrill(ctx context.Context, drillID uuid.UUID) error {
	for i, name := range AllSteps {
		idem := fmt.Sprintf("%s.%s", drillID, name)
		if _, err := o.store.CreateStepIfMissing(ctx, drillID, name, i, idem); err != nil {
			return fmt.Errorf("create step %s: %w", name, err)
		}
	}

	// Audit before the enqueue so a queue failure doesn't leave the creation
	// invisible to the user.
	dr, err := o.store.GetDrillByID(ctx, drillID)
	if err == nil && o.audit != nil {
		acct := dr.AccountID
		_ = o.audit.Record(ctx, audit.Event{
			AccountID:  &acct,
			ActorID:    &dr.CreatedByUserID,
			Action:     "drill.created",
			TargetKind: "drill",
			TargetID:   drillID.String(),
			Metadata:   map[string]any{"target_id": dr.TargetID.String()},
		})
	}

	if _, err := o.client.Insert(ctx, ProvisionArgs{DrillID: drillID}, nil); err != nil {
		return fmt.Errorf("enqueue provision: %w", err)
	}
	return nil
}
