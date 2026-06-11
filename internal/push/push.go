// Package push delivers mobile push notifications for the responder app. It
// owns the device-token registry and a Sender abstraction so the actual
// transport (Firebase Cloud Messaging) can be swapped for a logging no-op when
// credentials aren't configured — the same pattern as the log mailer.
package push

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preshotcome/anything/internal/drill"
	"github.com/preshotcome/anything/internal/heartbeat"
)

// Notification is one push message: a short title/body plus structured data the
// app uses to deep-link (e.g. {"type":"heartbeat","id":"…"}).
type Notification struct {
	Title string
	Body  string
	Data  map[string]string
}

// Sender delivers a notification to a set of device tokens. Implementations:
// LogSender (default, no credentials) and a future FCM HTTP v1 sender.
type Sender interface {
	Send(ctx context.Context, tokens []string, n Notification) error
}

// LogSender records what would be sent. Active whenever a real transport isn't
// configured, so the rest of the pipeline (registry, fan-out, event wiring) is
// exercised and observable without a Firebase project.
type LogSender struct{ Logger *slog.Logger }

func (s LogSender) Send(_ context.Context, tokens []string, n Notification) error {
	l := s.Logger
	if l == nil {
		l = slog.Default()
	}
	l.Info("push (log sender — FCM not configured)",
		"devices", len(tokens), "title", n.Title, "body", n.Body, "data", n.Data)
	return nil
}

// Store is the device-token registry.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Device is a registered push target.
type Device struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	AccountID  uuid.UUID
	Token      string
	Platform   string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Register upserts a device token. Re-registering the same token (the common
// case on every app launch) just rebinds it to the current user/account and
// bumps last_seen_at. Returns the row id.
func (s *Store) Register(ctx context.Context, userID, accountID uuid.UUID, token, platform string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO user_fcm_tokens (user_id, account_id, token, platform)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (token) DO UPDATE
		   SET user_id = EXCLUDED.user_id,
		       account_id = EXCLUDED.account_id,
		       platform = EXCLUDED.platform,
		       last_seen_at = now()
		RETURNING id
	`, userID, accountID, token, platform).Scan(&id)
	return id, err
}

// Delete removes one of the user's registered devices (e.g. on logout).
func (s *Store) Delete(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM user_fcm_tokens WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// TokensForAccount returns every registered device token in an account.
func (s *Store) TokensForAccount(ctx context.Context, accountID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT token FROM user_fcm_tokens WHERE account_id = $1`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// HeartbeatNotifier implements heartbeat.Notifier: it pushes to an account's
// devices when a backup check-in goes down or recovers. Composed alongside the
// existing email notifier (see heartbeat.MultiNotifier).
type HeartbeatNotifier struct {
	devices *Store
	sender  Sender
	logger  *slog.Logger
}

func NewHeartbeatNotifier(devices *Store, sender Sender, logger *slog.Logger) *HeartbeatNotifier {
	return &HeartbeatNotifier{devices: devices, sender: sender, logger: logger}
}

// DrillNotifier implements drill.Notifier: it pushes to an account's devices
// when a drill finishes (failed or completed).
type DrillNotifier struct {
	devices *Store
	sender  Sender
	logger  *slog.Logger
}

func NewDrillNotifier(devices *Store, sender Sender, logger *slog.Logger) *DrillNotifier {
	return &DrillNotifier{devices: devices, sender: sender, logger: logger}
}

func (n *DrillNotifier) NotifyDrill(ctx context.Context, dr drill.Drill, event, reason string) error {
	tokens, err := n.devices.TokensForAccount(ctx, dr.AccountID)
	if err != nil || len(tokens) == 0 {
		return err
	}
	title, body := "Drill passed", "A restore drill completed successfully."
	if event == drill.EventFailed {
		title = "Drill FAILED"
		body = "A restore drill failed"
		if reason != "" {
			body += " — " + reason
		} else {
			body += "."
		}
	}
	data := map[string]string{"type": "drill", "id": dr.ID.String(), "event": event}
	if reason != "" {
		data["reason"] = reason
	}
	return n.sender.Send(ctx, tokens, Notification{Title: title, Body: body, Data: data})
}

func (n *HeartbeatNotifier) Notify(ctx context.Context, hb heartbeat.Heartbeat, event string) error {
	tokens, err := n.devices.TokensForAccount(ctx, hb.AccountID)
	if err != nil || len(tokens) == 0 {
		return err
	}
	title, body := "Backup check-in recovered", hb.Name+" is reporting again."
	if event == heartbeat.EventDown {
		title, body = "Backup check-in DOWN", hb.Name+" is overdue — the backup job may have stopped."
	}
	return n.sender.Send(ctx, tokens, Notification{
		Title: title, Body: body,
		Data: map[string]string{"type": "heartbeat", "id": hb.ID.String(), "event": event},
	})
}
