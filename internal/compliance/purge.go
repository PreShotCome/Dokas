package compliance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/evidence"
)

// ErrNotEligible means an account isn't in a state where a hard delete can
// run (not soft-deleted, or still inside its grace window). It's not a
// failure — the scheduled job treats it as a no-op.
var ErrNotEligible = errors.New("compliance: account not eligible for hard delete")

// DefaultGracePeriod is how long a soft-deleted account lingers before the
// hard delete runs — a window for the customer to reverse a mistake.
const DefaultGracePeriod = 30 * 24 * time.Hour

// Purger handles the account-deletion lifecycle.
type Purger struct {
	pool     *pgxpool.Pool
	evidence *evidence.Service
	audit    *audit.Logger
	grace    time.Duration
}

func NewPurger(pool *pgxpool.Pool, ev *evidence.Service, auditLog *audit.Logger, grace time.Duration) *Purger {
	if grace < 0 {
		grace = 0
	}
	return &Purger{pool: pool, evidence: ev, audit: auditLog, grace: grace}
}

// GracePeriod is the configured soft-delete window.
func (p *Purger) GracePeriod() time.Duration { return p.grace }

// SoftDelete marks an account deleted and sets its purge horizon. Returns the
// time the hard delete becomes due so the caller can schedule the job.
func (p *Purger) SoftDelete(ctx context.Context, accountID, actorID uuid.UUID) (time.Time, error) {
	now := time.Now().UTC()
	purgeAfter := now.Add(p.grace)
	tag, err := p.pool.Exec(ctx, `
		UPDATE accounts
		   SET deleted_at = $2, purge_after = $3
		 WHERE id = $1 AND deleted_at IS NULL
	`, accountID, now, purgeAfter)
	if err != nil {
		return time.Time{}, err
	}
	if tag.RowsAffected() == 0 {
		return time.Time{}, fmt.Errorf("compliance: account %s not found or already deleted", accountID)
	}
	if p.audit != nil {
		_ = p.audit.Record(ctx, audit.Event{
			AccountID: &accountID, ActorID: &actorID, Action: "account.soft_deleted",
			TargetKind: "account", TargetID: accountID.String(),
			Metadata: map[string]any{"purge_after": purgeAfter.Format(time.RFC3339)},
		})
	}
	return purgeAfter, nil
}

// HardDelete permanently removes an account: it shreds every evidence file,
// then deletes the account row (cascading memberships, targets, drills,
// steps, assertions, signatures, webhooks). audit_events.account_id is
// ON DELETE SET NULL, so the audit trail of the deletion itself survives.
//
// Refuses to run unless the account is soft-deleted and past purge_after,
// so a stale or malicious job can't wipe a live account.
func (p *Purger) HardDelete(ctx context.Context, accountID uuid.UUID) error {
	var purgeAfter *time.Time
	var deletedAt *time.Time
	err := p.pool.QueryRow(ctx, `
		SELECT deleted_at, purge_after FROM accounts WHERE id = $1
	`, accountID).Scan(&deletedAt, &purgeAfter)
	if err != nil {
		return fmt.Errorf("load account: %w", err)
	}
	if deletedAt == nil {
		return fmt.Errorf("%w: %s is not soft-deleted", ErrNotEligible, accountID)
	}
	if purgeAfter != nil && time.Now().UTC().Before(*purgeAfter) {
		return fmt.Errorf("%w: %s still in grace until %s", ErrNotEligible, accountID, purgeAfter)
	}

	// Shred evidence files before the cascade removes the drill rows that
	// point at them.
	rows, err := p.pool.Query(ctx, `
		SELECT evidence_path FROM drills
		 WHERE account_id = $1 AND evidence_path IS NOT NULL
	`, accountID)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			return err
		}
		paths = append(paths, path)
	}
	rows.Close()

	for _, path := range paths {
		if err := p.evidence.DeleteKey(ctx, path); err != nil {
			return fmt.Errorf("shred evidence %s: %w", path, err)
		}
	}

	if _, err := p.pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	if p.audit != nil {
		// account_id is now dangling (SET NULL), so record it in metadata.
		_ = p.audit.Record(ctx, audit.Event{
			Action:     "account.hard_deleted",
			TargetKind: "account", TargetID: accountID.String(),
			Metadata: map[string]any{"evidence_files_shredded": len(paths)},
		})
	}
	return nil
}

// --- River job ---

// PurgeAccountArgs is the scheduled hard-delete job. It's enqueued at
// soft-delete time with ScheduledAt = purge_after.
type PurgeAccountArgs struct {
	AccountID uuid.UUID `json:"account_id"`
}

func (PurgeAccountArgs) Kind() string { return "compliance.purge_account" }

// PurgeWorker runs the hard delete. HardDelete itself re-checks the grace
// window, so a job that fires early (clock skew, manual retry) is safe.
type PurgeWorker struct {
	river.WorkerDefaults[PurgeAccountArgs]
	Purger *Purger
}

func (w *PurgeWorker) Work(ctx context.Context, job *river.Job[PurgeAccountArgs]) error {
	err := w.Purger.HardDelete(ctx, job.Args.AccountID)
	if errors.Is(err, ErrNotEligible) {
		// Account was restored or already purged — done, don't retry.
		return nil
	}
	// Genuine errors (DB down, etc.) propagate so River retries.
	return err
}

func (w *PurgeWorker) Timeout(*river.Job[PurgeAccountArgs]) time.Duration {
	return 2 * time.Minute
}
