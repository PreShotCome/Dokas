package obs

import (
	"context"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
)

// ErrorReporter receives panics and unexpected errors for off-box tracking.
// Production uses SentryReporter; dev/CI uses NoopReporter, which only logs.
type ErrorReporter interface {
	// CaptureError reports a non-fatal error with optional structured tags.
	CaptureError(ctx context.Context, err error, tags map[string]string)
	// CapturePanic reports a recovered panic value.
	CapturePanic(ctx context.Context, recovered any, tags map[string]string)
	// Flush blocks until buffered events are sent, up to timeout.
	Flush(timeout time.Duration)
}

// NoopReporter logs through slog and reports nowhere. It's the default when
// SENTRY_DSN is unset, so the app is fully functional without Sentry.
type NoopReporter struct{ logger *slog.Logger }

func NewNoopReporter(logger *slog.Logger) *NoopReporter {
	return &NoopReporter{logger: logger}
}

func (n *NoopReporter) CaptureError(ctx context.Context, err error, tags map[string]string) {
	n.logger.LogAttrs(ctx, slog.LevelError, "error_captured",
		slog.String("err", err.Error()), slog.Any("tags", tags))
}

func (n *NoopReporter) CapturePanic(ctx context.Context, recovered any, tags map[string]string) {
	n.logger.LogAttrs(ctx, slog.LevelError, "panic_captured",
		slog.Any("recovered", recovered), slog.Any("tags", tags))
}

func (n *NoopReporter) Flush(time.Duration) {}

// SentryReporter forwards to Sentry. Constructed only when SENTRY_DSN is set.
type SentryReporter struct{ logger *slog.Logger }

// NewErrorReporter returns a SentryReporter when dsn is non-empty, otherwise
// a NoopReporter. env tags every event with the deployment environment.
func NewErrorReporter(dsn, env string, logger *slog.Logger) (ErrorReporter, error) {
	if dsn == "" {
		return NewNoopReporter(logger), nil
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:         dsn,
		Environment: env,
	}); err != nil {
		return nil, err
	}
	return &SentryReporter{logger: logger}, nil
}

func (s *SentryReporter) CaptureError(ctx context.Context, err error, tags map[string]string) {
	hub := sentry.CurrentHub().Clone()
	hub.ConfigureScope(func(scope *sentry.Scope) {
		for k, v := range tags {
			scope.SetTag(k, v)
		}
	})
	hub.CaptureException(err)
	s.logger.LogAttrs(ctx, slog.LevelError, "error_captured", slog.String("err", err.Error()))
}

func (s *SentryReporter) CapturePanic(ctx context.Context, recovered any, tags map[string]string) {
	hub := sentry.CurrentHub().Clone()
	hub.ConfigureScope(func(scope *sentry.Scope) {
		for k, v := range tags {
			scope.SetTag(k, v)
		}
	})
	hub.Recover(recovered)
	s.logger.LogAttrs(ctx, slog.LevelError, "panic_captured", slog.Any("recovered", recovered))
}

func (s *SentryReporter) Flush(timeout time.Duration) { sentry.Flush(timeout) }
