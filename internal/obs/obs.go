package obs

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// Provider bundles the observability components the app wires into its
// router and workers.
type Provider struct {
	Metrics  *Metrics
	Reporter ErrorReporter
	shutdown func(context.Context) error
}

// Config is the subset of app config obs needs.
type Config struct {
	Environment string
	SentryDSN   string
}

// Setup initializes tracing, metrics, and error reporting. The returned
// Provider is shared across the app; call Shutdown on exit.
func Setup(ctx context.Context, cfg Config, logger *slog.Logger) (*Provider, error) {
	traceShutdown, err := setupTracing(ctx, cfg.Environment)
	if err != nil {
		return nil, err
	}
	reporter, err := NewErrorReporter(cfg.SentryDSN, cfg.Environment, logger)
	if err != nil {
		return nil, err
	}
	return &Provider{
		Metrics:  NewMetrics(),
		Reporter: reporter,
		shutdown: traceShutdown,
	}, nil
}

// Shutdown flushes traces and pending error reports.
func (p *Provider) Shutdown(ctx context.Context) {
	if p.shutdown != nil {
		_ = p.shutdown(ctx)
	}
	p.Reporter.Flush(5 * time.Second)
}

// Recoverer is a panic-recovery middleware that reports the panic through
// the error reporter, then returns 500. It replaces chi's stock Recoverer
// so panics reach Sentry.
func (p *Provider) Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				p.Reporter.CapturePanic(r.Context(), rec, map[string]string{
					"path":       r.URL.Path,
					"trace_id":   TraceIDFromContext(r.Context()),
					"stack_hint": firstStackLine(),
				})
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ReadinessHandler returns a /readyz handler that runs the supplied checks
// (e.g. a DB ping). Any failure yields 503 with the failing check named.
func ReadinessHandler(checks map[string]func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		for name, check := range checks {
			if err := check(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("not ready: " + name + ": " + err.Error()))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	}
}

func firstStackLine() string {
	stack := string(debug.Stack())
	// The stack starts with "goroutine N [running]:"; the caller frame is a
	// few lines down. Return a short hint, not the whole dump.
	if len(stack) > 240 {
		return stack[:240]
	}
	return stack
}
