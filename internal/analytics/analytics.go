// Package analytics captures product events for growth funnels. Production
// sends to PostHog; without POSTHOG_API_KEY it falls back to a no-op that
// only logs. Capture is always best-effort and asynchronous — it must never
// block or fail the request that triggered it.
package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Analytics captures a named event for a distinct actor (a user or account
// ID). Implementations must return immediately; delivery happens in the
// background.
type Analytics interface {
	Capture(distinctID, event string, props map[string]any)
	// Enabled reports whether a real analytics backend is wired.
	Enabled() bool
}

// NoopAnalytics logs events at debug level and sends nothing. The default
// when POSTHOG_API_KEY is unset.
type NoopAnalytics struct{ logger *slog.Logger }

func NewNoopAnalytics(logger *slog.Logger) *NoopAnalytics {
	return &NoopAnalytics{logger: logger}
}

func (n *NoopAnalytics) Capture(distinctID, event string, props map[string]any) {
	n.logger.Debug("analytics event (noop)", "event", event, "distinct_id", distinctID, "props", props)
}
func (n *NoopAnalytics) Enabled() bool { return false }

// PostHogAnalytics posts events to PostHog's capture API. Each Capture call
// fires a background goroutine so the caller is never blocked; a failed send
// is logged and dropped (events are growth signal, not transactional data).
type PostHogAnalytics struct {
	apiKey   string
	endpoint string
	http     *http.Client
	logger   *slog.Logger
}

// New returns a PostHogAnalytics when apiKey is set, otherwise a
// NoopAnalytics. host defaults to https://app.posthog.com when empty.
func New(apiKey, host string, logger *slog.Logger) Analytics {
	if apiKey == "" {
		return NewNoopAnalytics(logger)
	}
	if host == "" {
		host = "https://app.posthog.com"
	}
	return &PostHogAnalytics{
		apiKey:   apiKey,
		endpoint: host + "/capture/",
		http:     &http.Client{Timeout: 10 * time.Second},
		logger:   logger,
	}
}

func (p *PostHogAnalytics) Enabled() bool { return true }

func (p *PostHogAnalytics) Capture(distinctID, event string, props map[string]any) {
	go p.deliver(distinctID, event, props)
}

func (p *PostHogAnalytics) deliver(distinctID, event string, props map[string]any) {
	body, err := json.Marshal(map[string]any{
		"api_key":     p.apiKey,
		"event":       event,
		"distinct_id": distinctID,
		"properties":  props,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		p.logger.Warn("analytics marshal failed", "event", event, "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		p.logger.Warn("analytics request build failed", "event", event, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		p.logger.Warn("analytics send failed", "event", event, "err", err)
		return
	}
	_ = resp.Body.Close()
}

// Event name constants — one place so the funnel definitions in PostHog and
// the capture call sites can't drift.
const (
	EventSignedUp       = "user.signed_up"
	EventInvitationSent = "invitation.sent"
	EventDrillCompleted = "drill.completed"
	EventDrillFailed    = "drill.failed"
)
