// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package drill

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/preshotcome/dokaz/internal/account"
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
	for _, ds := range due {
		t := ds.Target
		interval := CadenceInterval(t.DrillCadence)
		if interval == 0 || t.NextDrillAt == nil {
			continue
		}
		// Re-check the cadence against the account's *current* plan every
		// fire. A Scale→Growth downgrade leaves the stored `daily` cadence
		// untouched (SyncSubscription only writes the plan column), and
		// without this check the schedule would keep firing daily against a
		// plan that only allows weekly. When disallowed, we don't just skip
		// the fire — we downshift the stored cadence to the plan's
		// highest-allowed so the account isn't wedged in a broken state.
		// Unlimited accounts bypass the check entirely — they can pick any
		// cadence including hourly.
		if !ds.Unlimited && !account.CadenceAllowed(account.Plan(ds.Plan), t.DrillCadence) {
			fallback := account.TopCadence(account.Plan(ds.Plan))
			if w.Logger != nil {
				w.Logger.Warn("scheduler: cadence disallowed for plan; downshifting",
					"target_id", t.ID, "plan", ds.Plan,
					"from", t.DrillCadence, "to", fallback)
			}
			// If even the top cadence for the plan is "off", stop scheduling.
			nextInterval := CadenceInterval(fallback)
			if nextInterval == 0 {
				if err := w.Store.SetTargetSchedule(ctx, t.AccountID, t.ID, "off", nil); err != nil {
					w.logErr("downshift schedule off", t.ID, err)
				}
				continue
			}
			nextAt := time.Now().UTC().Add(nextInterval)
			if err := w.Store.SetTargetSchedule(ctx, t.AccountID, t.ID, fallback, &nextAt); err != nil {
				w.logErr("downshift schedule", t.ID, err)
			}
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
			// Priority off the account plan: paid tiers (1) preempt trial
			// (4) on the shared queue when the backlog builds. Unlimited
			// accounts get top priority too.
			opts := priorityForPlan(ds.Plan)
			if ds.Unlimited {
				opts = &river.InsertOpts{Priority: 1}
			}
			if err := w.Orch.EnqueueDrill(ctx, drillID, opts); err != nil {
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

// priorityForPlan maps an account plan to a River insert priority so paid
// tiers preempt trial jobs on the shared queue, and higher-tier customers
// jump the queue over lower-tier ones. River priority 1 (highest) → 4
// (lowest); the scheduler picks lower numbers first.
//
//	Grounded (scale) : 1  — advertised on the pricing page as top-priority
//	Growth  (pro)    : 2  — advertised as priority queue
//	Starter          : 3  — standard queue
//	Trial            : 4  — deferrable, prevents a free tenant from starving paid
//
// Keep in sync with the web handlers' drillInsertOpts.
func priorityForPlan(plan string) *river.InsertOpts {
	priority := 3
	switch account.Plan(plan) {
	case account.PlanScale:
		priority = 1
	case account.PlanPro:
		priority = 2
	case account.PlanStarter:
		priority = 3
	case account.PlanTrial:
		priority = 4
	}
	return &river.InsertOpts{Priority: priority}
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
