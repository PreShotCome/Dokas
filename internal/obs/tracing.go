package obs

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/preshotcome/anything"

// Tracer is the application's span factory. Drill workers and handlers use
// it directly; when tracing is disabled it's a no-op tracer with zero cost.
func Tracer() trace.Tracer { return otel.Tracer(tracerName) }

// setupTracing configures the global OpenTelemetry tracer provider. The
// exporter is chosen by OTEL_TRACES_EXPORTER:
//
//	otlp    → OTLP/HTTP to OTEL_EXPORTER_OTLP_ENDPOINT
//	stdout  → pretty-printed spans to stdout (dev / local verification)
//	(unset) → no-op; spans are created but dropped, no overhead
//
// Returns a shutdown func the caller defers.
func setupTracing(ctx context.Context, env string) (func(context.Context) error, error) {
	mode := os.Getenv("OTEL_TRACES_EXPORTER")

	var exporter sdktrace.SpanExporter
	var err error
	switch mode {
	case "otlp":
		exporter, err = otlptracehttp.New(ctx)
	case "stdout":
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	case "", "none", "noop":
		// No exporter — install a tracer provider with no processors so
		// span creation stays cheap and inert.
		tp := sdktrace.NewTracerProvider()
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.TraceContext{})
		return tp.Shutdown, nil
	default:
		return nil, fmt.Errorf("obs: unknown OTEL_TRACES_EXPORTER %q", mode)
	}
	if err != nil {
		return nil, fmt.Errorf("obs: trace exporter: %w", err)
	}

	// NewSchemaless avoids a schema-URL conflict with resource.Default()
	// (the SDK's default resource pins a different semconv version).
	res := resource.NewSchemaless(
		semconv.ServiceName("soteria"),
		semconv.DeploymentEnvironment(env),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp.Shutdown, nil
}

// TracingMiddleware opens a server span per request and propagates context.
// The route template is added once it's known (after the handler runs).
func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(r.Context(),
			propagation.HeaderCarrier(r.Header))
		ctx, span := Tracer().Start(ctx, "http.request",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.target", r.URL.Path),
			),
		)
		defer span.End()
		next.ServeHTTP(w, r.WithContext(ctx))
		span.SetAttributes(attribute.String("http.route", routePattern(r)))
	})
}

// StartSpan opens a child span with string attributes and returns the new
// context plus an end func to defer. A thin wrapper so callers (drill step
// workers) don't import the otel packages directly.
func StartSpan(ctx context.Context, name string, attrs map[string]string) (context.Context, func()) {
	ctx, span := Tracer().Start(ctx, name)
	for k, v := range attrs {
		span.SetAttributes(attribute.String(k, v))
	}
	return ctx, func() { span.End() }
}

// TraceParentFromContext serializes the active span context to a W3C
// `traceparent` string, or "" when no span is recording. Used to carry a
// trace across a process/job boundary (River jobs).
func TraceParentFromContext(ctx context.Context) string {
	c := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, c)
	return c["traceparent"]
}

// ContextWithTraceParent rebuilds a context from a `traceparent` string so a
// span started on it joins the originating trace. Empty input is a no-op.
func ContextWithTraceParent(ctx context.Context, traceparent string) context.Context {
	if traceparent == "" {
		return ctx
	}
	c := propagation.MapCarrier{"traceparent": traceparent}
	return otel.GetTextMapPropagator().Extract(ctx, c)
}

// TraceIDFromContext returns the active trace ID, or "" when no span is
// recording. The request logger uses it to correlate logs with traces.
func TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}
