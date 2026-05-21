// Package obs wires observability: structured request logging, Prometheus
// metrics, OpenTelemetry tracing, and error reporting. External backends
// (an OTLP collector, Sentry) are config-gated; with nothing configured the
// app still runs with local-only logging + metrics.
package obs

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type ctxKey int

const fieldsCtxKey ctxKey = 0

// requestFields are the structured log fields accumulated over a request.
type requestFields struct {
	accountID string
	traceID   string
}

// WithAccountID stamps the current account onto the request context so the
// request logger can include it. Call from a middleware after the account
// is resolved.
func WithAccountID(ctx context.Context, accountID string) context.Context {
	f := fieldsFrom(ctx)
	f.accountID = accountID
	return context.WithValue(ctx, fieldsCtxKey, f)
}

func fieldsFrom(ctx context.Context) *requestFields {
	if f, ok := ctx.Value(fieldsCtxKey).(*requestFields); ok {
		return f
	}
	return &requestFields{}
}

// RequestLogger is a middleware that emits one structured log line per
// request: method, route, status, duration, request_id, and (when present)
// account_id and trace_id. Place it after RequestID and the tracing
// middleware so those IDs are available.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Seed a fresh fields holder so downstream middleware can fill it.
			ctx := context.WithValue(r.Context(), fieldsCtxKey, &requestFields{
				traceID: TraceIDFromContext(r.Context()),
			})
			r = r.WithContext(ctx)

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			dur := time.Since(start)

			f := fieldsFrom(r.Context())
			attrs := []any{
				"method", r.Method,
				"route", routePattern(r),
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", dur.Milliseconds(),
				"bytes", ww.BytesWritten(),
				"request_id", middleware.GetReqID(r.Context()),
			}
			if f.accountID != "" {
				attrs = append(attrs, "account_id", f.accountID)
			}
			if f.traceID != "" {
				attrs = append(attrs, "trace_id", f.traceID)
			}

			level := slog.LevelInfo
			switch {
			case ww.Status() >= 500:
				level = slog.LevelError
			case ww.Status() >= 400:
				level = slog.LevelWarn
			}
			logger.LogAttrs(r.Context(), level, "http_request", toAttrs(attrs)...)
		})
	}
}

func toAttrs(kv []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, slog.Any(kv[i].(string), kv[i+1]))
	}
	return out
}

// routePattern returns the chi route template (e.g. "/drills/{id}") rather
// than the concrete path, so metric + log cardinality stays bounded. Falls
// back to the path when no pattern matched (404s).
func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return r.URL.Path
}
