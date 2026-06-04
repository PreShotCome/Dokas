package heartbeat

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/preshotcome/anything/internal/audit"
)

// Event names emitted to webhooks and the audit log. The "up" edge is fired by
// the ping handler (on a transition); the "down" edge is fired here.
const (
	EventUp   = "heartbeat.up"
	EventDown = "heartbeat.down"
)

// Dispatcher is the slice of the webhook dispatcher the sweeper needs.
type Dispatcher interface {
	Dispatch(ctx context.Context, accountID uuid.UUID, event string, data map[string]any) error
}

// Auditor is the slice of the audit logger the sweeper needs.
type Auditor interface {
	Record(ctx context.Context, e audit.Event) error
}

// Notifier delivers a human-facing alert for a monitor transition (e.g. an
// email to the account's members). Implemented outside the domain package so
// heartbeat stays free of email/account dependencies. Optional on the worker.
type Notifier interface {
	Notify(ctx context.Context, hb Heartbeat, event string) error
}

// EventData builds the webhook/audit payload for a monitor transition. Shared
// by the sweeper (down) and the ping handler (up) so both edges carry the same
// shape.
func EventData(h Heartbeat) map[string]any {
	data := map[string]any{
		"heartbeat_id":   h.ID.String(),
		"name":           h.Name,
		"status":         string(h.Status),
		"period_seconds": h.PeriodSeconds,
		"grace_seconds":  h.GraceSeconds,
	}
	if h.LastPingAt != nil {
		data["last_ping_at"] = h.LastPingAt.UTC().Format(time.RFC3339)
	}
	return data
}

// SweeperArgs is the periodic job that flips overdue monitors to down.
type SweeperArgs struct{}

func (SweeperArgs) Kind() string { return "heartbeat.sweeper" }

// SweeperWorker scans for monitors whose check-in is overdue, flips each to
// "down", and fans the down event out to the account's webhooks plus the audit
// log. One failing notification is logged and skipped — it never blocks the
// rest of the batch, and the flip itself has already been committed.
type SweeperWorker struct {
	river.WorkerDefaults[SweeperArgs]
	Store    *Store
	Dispatch Dispatcher
	Audit    Auditor
	Notify   Notifier
	Logger   *slog.Logger
}

func (w *SweeperWorker) Work(ctx context.Context, _ *river.Job[SweeperArgs]) error {
	downed, err := w.Store.MarkOverdueDown(ctx)
	if err != nil {
		return err
	}
	for _, h := range downed {
		data := EventData(h)
		if w.Dispatch != nil {
			if err := w.Dispatch.Dispatch(ctx, h.AccountID, EventDown, data); err != nil {
				w.logErr("dispatch heartbeat.down", h.ID, err)
			}
		}
		if w.Audit != nil {
			acct := h.AccountID
			if err := w.Audit.Record(ctx, audit.Event{
				AccountID:  &acct,
				Action:     EventDown,
				TargetKind: "heartbeat",
				TargetID:   h.ID.String(),
				Metadata:   data,
			}); err != nil {
				w.logErr("audit heartbeat.down", h.ID, err)
			}
		}
		if w.Notify != nil {
			if err := w.Notify.Notify(ctx, h, EventDown); err != nil {
				w.logErr("notify heartbeat.down", h.ID, err)
			}
		}
		if w.Logger != nil {
			w.Logger.Warn("heartbeat overdue", "heartbeat_id", h.ID, "account_id", h.AccountID, "name", h.Name)
		}
	}
	return nil
}

func (w *SweeperWorker) logErr(msg string, id uuid.UUID, err error) {
	if w.Logger != nil {
		w.Logger.Error("heartbeat sweeper: "+msg, "heartbeat_id", id, "err", err)
	}
}

func (w *SweeperWorker) Timeout(*river.Job[SweeperArgs]) time.Duration {
	return 2 * time.Minute
}

// SweeperPeriodicJob runs the overdue scan every minute — tight enough that a
// missed check-in is caught within ~a minute of its grace window closing.
func SweeperPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(time.Minute),
		func() (river.JobArgs, *river.InsertOpts) {
			return SweeperArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: true},
	)
}
