package webhooks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// DeliverArgs is the River job for one delivery attempt. The job carries only
// the delivery ID; everything else is re-read from the row so a replay always
// uses the current endpoint URL/secret.
type DeliverArgs struct {
	DeliveryID uuid.UUID `json:"delivery_id"`
}

func (DeliverArgs) Kind() string { return "webhook.deliver" }

// DeliveryMetrics is the metric sink for delivery outcomes. The obs.Metrics
// type satisfies it; nil disables recording (tests).
type DeliveryMetrics interface {
	RecordWebhookDelivery(outcome string)
}

// DeliverWorker performs one HTTP POST attempt. A non-2xx response or a
// transport error returns an error so River retries with backoff; the
// delivery row records every attempt either way.
type DeliverWorker struct {
	river.WorkerDefaults[DeliverArgs]
	Store   *Store
	HTTP    *http.Client
	Metrics DeliveryMetrics
}

// NewDeliverWorker wires a worker with a sane default HTTP client.
func NewDeliverWorker(store *Store) *DeliverWorker {
	return &DeliverWorker{
		Store: store,
		HTTP:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (w *DeliverWorker) recordOutcome(outcome string) {
	if w.Metrics != nil {
		w.Metrics.RecordWebhookDelivery(outcome)
	}
}

func (w *DeliverWorker) Timeout(*river.Job[DeliverArgs]) time.Duration {
	return 30 * time.Second
}

func (w *DeliverWorker) Work(ctx context.Context, job *river.Job[DeliverArgs]) error {
	d, err := w.Store.GetDelivery(ctx, job.Args.DeliveryID)
	if err != nil {
		return fmt.Errorf("load delivery: %w", err)
	}
	if d.Status == StatusDelivered {
		return nil // already done — replay or duplicate job
	}

	endpoint, err := w.Store.GetEndpointByID(ctx, d.EndpointID)
	if err != nil {
		// Endpoint deleted between enqueue and delivery — mark failed and
		// stop retrying.
		_ = w.Store.RecordAttempt(ctx, d.ID, StatusFailed, 0, "endpoint no longer exists")
		return nil
	}

	statusCode, attemptErr := w.post(ctx, endpoint, d)
	if attemptErr == nil && statusCode >= 200 && statusCode < 300 {
		_ = w.Store.RecordAttempt(ctx, d.ID, StatusDelivered, statusCode, "")
		w.recordOutcome("delivered")
		return nil
	}

	reason := ""
	if attemptErr != nil {
		reason = attemptErr.Error()
	} else {
		reason = fmt.Sprintf("non-2xx response: %d", statusCode)
	}
	_ = w.Store.RecordAttempt(ctx, d.ID, StatusFailed, statusCode, reason)
	w.recordOutcome("failed")

	// Return an error so River retries (up to its MaxAttempts). The status
	// stays "failed" between attempts; a later success flips it to delivered.
	return fmt.Errorf("webhook delivery failed: %s", reason)
}

func (w *DeliverWorker) post(ctx context.Context, e Endpoint, d Delivery) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL, bytes.NewReader(d.Payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "RestoreDrill-Webhooks/1")
	req.Header.Set("X-RestoreDrill-Event", d.Event)
	req.Header.Set("X-RestoreDrill-Delivery", d.ID.String())
	req.Header.Set(SignatureHeader, Sign(e.Secret, d.Payload))

	resp, err := w.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// Drain a bounded amount so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}
