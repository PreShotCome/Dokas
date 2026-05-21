package drill

import (
	"context"
	"encoding/json"

	"github.com/riverqueue/river"

	"github.com/preshotcome/anything/internal/obs"
)

// traceMetaKey is the key under which the W3C traceparent is stored in a
// River job's metadata blob.
const traceMetaKey = "traceparent"

// TraceOpts builds River InsertOpts that carry the current trace context in
// the job's metadata, so the worked job's span joins the same trace. Returns
// nil when there's no active trace (River accepts a nil opts).
func TraceOpts(ctx context.Context) *river.InsertOpts {
	tp := obs.TraceParentFromContext(ctx)
	if tp == "" {
		return nil
	}
	meta, err := json.Marshal(map[string]string{traceMetaKey: tp})
	if err != nil {
		return nil
	}
	return &river.InsertOpts{Metadata: meta}
}

// ContextFromJobMeta rebuilds a trace-carrying context from a River job's
// metadata blob. A span started on the result joins the trace that enqueued
// the job — stitching the drill's per-step spans into one tree.
func ContextFromJobMeta(ctx context.Context, meta []byte) context.Context {
	if len(meta) == 0 {
		return ctx
	}
	var m map[string]string
	if json.Unmarshal(meta, &m) != nil {
		return ctx
	}
	return obs.ContextWithTraceParent(ctx, m[traceMetaKey])
}
