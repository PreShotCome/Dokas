package drill

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// CadenceInterval maps a drill cadence to its interval. An unrecognised
// cadence (including "off") returns 0.
func CadenceInterval(cadence string) time.Duration {
	switch cadence {
	case "hourly":
		return time.Hour
	case "daily":
		return 24 * time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	case "monthly":
		return 30 * 24 * time.Hour
	default:
		return 0
	}
}

// SchedulerArgs is the periodic job that enqueues drills for due targets.
type SchedulerArgs struct{}

func (SchedulerArgs) Kind() string { return "drill.scheduler" }

// SchedulerWorker enqueues a drill for every target whose schedule is due,
// then advances each target's next-run time. One failing target is logged
// and skipped — it never blocks the rest of the batch.
type SchedulerWorker struct {
	river.WorkerDefaults[SchedulerArgs]
	Store  *Store
	Orch   *Orchestrator
	Logger *slog.Logger
}

func (w *SchedulerWorker) Work(ctx context.Context, _ *river.Job[SchedulerArgs]) error {
	due, err := w.Store.DueTargets(ctx)
	if err != nil {
		return err
	}
	for _, t := range due {
		interval := CadenceInterval(t.DrillCadence)
		if interval == 0 || t.NextDrillAt == nil {
			continue
		}
		// The idempotency key is unique per scheduled slot, so a double
		// scheduler tick cannot create two drills for the same occurrence.
		key := fmt.Sprintf("schedule:%s:%d", t.ID, t.NextDrillAt.Unix())
		drillID, reused, err := w.Store.CreateDrillIdempotent(ctx, t.AccountID, t.CreatedByUserID, t.ID, key)
		if err != nil {
			w.logErr("create scheduled drill", t.ID, err)
			continue
		}
		if !reused {
			if err := w.Orch.EnqueueDrill(ctx, drillID); err != nil {
				w.logErr("enqueue scheduled drill", t.ID, err)
				continue
			}
		}
		// Advance to the next slot boundary, not to now+interval — using
		// now would drift the schedule forward by however late this tick
		// fired (a daily 02:00 drill that fired at 02:04 would next fire
		// at 02:04 the following day, then later, then later). Compute
		// the smallest scheduled boundary that is still in the future.
		next := t.NextDrillAt.Add(interval)
		now := time.Now().UTC()
		for !next.After(now) {
			next = next.Add(interval)
		}
		if err := w.Store.AdvanceTargetSchedule(ctx, t.ID, next); err != nil {
			w.logErr("advance schedule", t.ID, err)
		}
	}
	return nil
}

func (w *SchedulerWorker) logErr(msg string, targetID uuid.UUID, err error) {
	if w.Logger != nil {
		w.Logger.Error("drill scheduler: "+msg, "target_id", targetID, "err", err)
	}
}

func (w *SchedulerWorker) Timeout(*river.Job[SchedulerArgs]) time.Duration {
	return 5 * time.Minute
}

// SchedulerPeriodicJob runs the scheduler every 5 minutes — frequent enough
// that an hourly-scheduled drill fires within a few minutes of its slot.
func SchedulerPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(5*time.Minute),
		func() (river.JobArgs, *river.InsertOpts) {
			return SchedulerArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: true},
	)
}
