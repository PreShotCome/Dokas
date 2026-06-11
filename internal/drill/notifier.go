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
