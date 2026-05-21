package compliance

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/evidence"
)

// Retention windows. Evidence and audit logs are auditor-grade and kept for
// seven years; login attempts are a short-lived security signal; API
// idempotency records only need to outlive a client's retry window.
const (
	AuditRetention          = 7 * 365 * 24 * time.Hour
	LoginAttemptsRetention  = 30 * 24 * time.Hour
	APIIdempotencyRetention = 24 * time.Hour
)

// Sweeper enforces retention: it purges evidence past retain_until, old
// audit events, and stale login attempts.
type Sweeper struct {
	pool     *pgxpool.Pool
	evidence *evidence.Service
	throttle *auth.LoginThrottle
}

func NewSweeper(pool *pgxpool.Pool, ev *evidence.Service, throttle *auth.LoginThrottle) *Sweeper {
	return &Sweeper{pool: pool, evidence: ev, throttle: throttle}
}

// SweepResult reports what a sweep removed.
type SweepResult struct {
	EvidencePurged    int
	AuditPruned       int64
	LoginsPruned      int64
	IdempotencyPruned int64
}

// Sweep runs one retention pass. Each step is independent; a failure in one
// is returned but does not skip the others already done.
func (s *Sweeper) Sweep(ctx context.Context) (SweepResult, error) {
	var res SweepResult

	n, err := s.evidence.PurgeExpired(ctx)
	res.EvidencePurged = n
	if err != nil {
		return res, err
	}

	tag, err := s.pool.Exec(ctx, `
		DELETE FROM audit_events WHERE at < $1
	`, time.Now().UTC().Add(-AuditRetention))
	if err != nil {
		return res, err
	}
	res.AuditPruned = tag.RowsAffected()

	if err := s.throttle.Prune(ctx, LoginAttemptsRetention); err != nil {
		return res, err
	}
	res.LoginsPruned = -1 // -1 = "ran, count not tracked"

	idemTag, err := s.pool.Exec(ctx, `
		DELETE FROM api_idempotency WHERE created_at < $1
	`, time.Now().UTC().Add(-APIIdempotencyRetention))
	if err != nil {
		return res, err
	}
	res.IdempotencyPruned = idemTag.RowsAffected()

	return res, nil
}

// --- River periodic job ---

// RetentionSweepArgs is the periodic retention job.
type RetentionSweepArgs struct{}

func (RetentionSweepArgs) Kind() string { return "compliance.retention_sweep" }

// RetentionWorker runs Sweep on River's periodic schedule.
type RetentionWorker struct {
	river.WorkerDefaults[RetentionSweepArgs]
	Sweeper *Sweeper
	Logger  *slog.Logger
}

func (w *RetentionWorker) Work(ctx context.Context, _ *river.Job[RetentionSweepArgs]) error {
	res, err := w.Sweeper.Sweep(ctx)
	if err != nil {
		return err
	}
	if w.Logger != nil {
		w.Logger.Info("retention sweep complete",
			"evidence_purged", res.EvidencePurged,
			"audit_pruned", res.AuditPruned,
		)
	}
	return nil
}

func (w *RetentionWorker) Timeout(*river.Job[RetentionSweepArgs]) time.Duration {
	return 10 * time.Minute
}

// PeriodicJob returns the River periodic-job definition: a retention sweep
// every 24h. Register it in the River client config.
func PeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(24*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) {
			return RetentionSweepArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: false},
	)
}
