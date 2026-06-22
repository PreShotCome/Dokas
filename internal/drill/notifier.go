package drill

import "context"

// Drill event names — match the strings the audit log and webhooks already
// emit, so a downstream notifier can switch on the same value.
const (
	EventCompleted = "drill.completed"
	EventFailed    = "drill.failed"
)

// Notifier delivers a human-facing alert for a drill terminal outcome.
// Implemented outside the drill package (e.g. push.DrillNotifier) so this
// package stays free of transport dependencies. Optional on the worker.
//
// reason is the drill's failure reason for EventFailed and empty for
// EventCompleted.
type Notifier interface {
	NotifyDrill(ctx context.Context, drill Drill, event, reason string) error
}

// MultiNotifier fans a drill notification out to several notifiers (e.g.
// mobile push + Slack/PagerDuty). A failure in one does not stop the others;
// the first error is returned for logging.
type MultiNotifier []Notifier

func (m MultiNotifier) NotifyDrill(ctx context.Context, drill Drill, event, reason string) error {
	var firstErr error
	for _, n := range m {
		if n == nil {
			continue
		}
		if err := n.NotifyDrill(ctx, drill, event, reason); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
