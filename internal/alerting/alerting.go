// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

// Package alerting delivers drill-failure and backup-check-in alerts to a
// team's own Slack and PagerDuty, alongside the built-in email + mobile push.
// It implements both the heartbeat and drill notifier interfaces, so it slots
// into the existing fan-outs. Delivery is best-effort: a webhook failure is
// logged, never propagated, so it can't wedge the sweeper or a drill.
package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/dokaz/internal/drill"
	"github.com/preshotcome/dokaz/internal/heartbeat"
)

// pagerDutyEnqueueURL is the Events API v2 endpoint. A var (not a const) so
// tests can point it at a local server.
var pagerDutyEnqueueURL = "https://events.pagerduty.com/v2/enqueue"

// Channels is an account's outbound alert configuration.
type Channels struct {
	SlackWebhookURL     string
	PagerDutyRoutingKey string
}

// Configured reports whether at least one channel is set.
func (c Channels) Configured() bool {
	return c.SlackWebhookURL != "" || c.PagerDutyRoutingKey != ""
}

// Store reads and writes per-account alert channels.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Get returns an account's channels, or a zero (unconfigured) value when none
// are set.
func (s *Store) Get(ctx context.Context, accountID uuid.UUID) (Channels, error) {
	var c Channels
	err := s.pool.QueryRow(ctx, `
		SELECT slack_webhook_url, pagerduty_routing_key
		  FROM account_alert_channels WHERE account_id = $1
	`, accountID).Scan(&c.SlackWebhookURL, &c.PagerDutyRoutingKey)
	if err != nil {
		// No row → unconfigured, which is not an error to callers.
		return Channels{}, nil
	}
	return c, nil
}

// Set upserts an account's channels.
func (s *Store) Set(ctx context.Context, accountID uuid.UUID, c Channels) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO account_alert_channels (account_id, slack_webhook_url, pagerduty_routing_key, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (account_id) DO UPDATE
		   SET slack_webhook_url = EXCLUDED.slack_webhook_url,
		       pagerduty_routing_key = EXCLUDED.pagerduty_routing_key,
		       updated_at = now()
	`, accountID, c.SlackWebhookURL, c.PagerDutyRoutingKey)
	return err
}

// Notifier delivers alerts to an account's configured channels.
type Notifier struct {
	store  *Store
	http   *http.Client
	logger *slog.Logger
}

func New(store *Store, logger *slog.Logger) *Notifier {
	return &Notifier{store: store, http: &http.Client{Timeout: 5 * time.Second}, logger: logger}
}

// Notify implements heartbeat.Notifier: a check-in going down (or recovering)
// fires a Slack message and a PagerDuty trigger/resolve keyed on the monitor.
func (n *Notifier) Notify(ctx context.Context, hb heartbeat.Heartbeat, event string) error {
	ch, _ := n.store.Get(ctx, hb.AccountID)
	if !ch.Configured() {
		return nil
	}
	switch event {
	case heartbeat.EventDown:
		n.slack(ctx, ch, "🔴 Backup check-in DOWN: *"+hb.Name+"* missed its expected ping.")
		n.pagerDuty(ctx, ch, "trigger", "hb-"+hb.ID.String(), "Backup check-in down: "+hb.Name, "critical")
	case heartbeat.EventUp:
		n.slack(ctx, ch, "✅ Backup check-in recovered: *"+hb.Name+"*.")
		n.pagerDuty(ctx, ch, "resolve", "hb-"+hb.ID.String(), "Backup check-in recovered: "+hb.Name, "info")
	}
	return nil
}

// NotifyDrill implements drill.Notifier: only failures are alerted — a passing
// drill is the expected case and would be noise.
func (n *Notifier) NotifyDrill(ctx context.Context, d drill.Drill, event, reason string) error {
	if event != drill.EventFailed {
		return nil
	}
	ch, _ := n.store.Get(ctx, d.AccountID)
	if !ch.Configured() {
		return nil
	}
	summary := "Restore drill failed"
	msg := "🔴 Restore drill FAILED"
	if reason != "" {
		msg += ": " + reason
		summary += ": " + reason
	}
	n.slack(ctx, ch, msg)
	n.pagerDuty(ctx, ch, "trigger", "drill-"+d.ID.String(), summary, "critical")
	return nil
}

func (n *Notifier) slack(ctx context.Context, ch Channels, text string) {
	if ch.SlackWebhookURL == "" {
		return
	}
	n.post(ctx, ch.SlackWebhookURL, map[string]any{"text": text})
}

func (n *Notifier) pagerDuty(ctx context.Context, ch Channels, action, dedupKey, summary, severity string) {
	if ch.PagerDutyRoutingKey == "" {
		return
	}
	n.post(ctx, pagerDutyEnqueueURL, map[string]any{
		"routing_key":  ch.PagerDutyRoutingKey,
		"event_action": action,
		"dedup_key":    dedupKey,
		"payload": map[string]any{
			"summary":  summary,
			"source":   "dokaz",
			"severity": severity,
		},
	})
}

// post sends a JSON body and logs (but never returns) any failure.
func (n *Notifier) post(ctx context.Context, url string, body any) {
	buf, err := json.Marshal(body)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.http.Do(req)
	if err != nil {
		n.logger.Warn("alert delivery failed", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		n.logger.Warn("alert delivery rejected", "status", resp.StatusCode)
	}
}
