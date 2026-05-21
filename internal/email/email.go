// Package email sends transactional mail. Production uses Postmark; dev/CI
// uses LogMailer, which renders the message to the log instead of sending.
// Both honour the suppression list so a bounced or complaining address is
// never emailed again.
package email

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Message is one transactional email.
type Message struct {
	To       string
	Subject  string
	TextBody string
}

// ErrSuppressed is returned by Send when the recipient is on the
// suppression list. Callers treat it as a non-fatal skip.
var ErrSuppressed = errors.New("email: recipient is suppressed")

// Sender delivers a single message. PostmarkMailer and LogMailer implement it.
type Sender interface {
	send(ctx context.Context, m Message) error
	// Enabled reports whether a real provider is wired.
	Enabled() bool
}

// Mailer is the public entry point: it checks the suppression list, then
// delegates to the configured Sender. Handlers and workers depend on
// *Mailer, not on the provider.
type Mailer struct {
	pool   *pgxpool.Pool
	sender Sender
	logger *slog.Logger
}

func NewMailer(pool *pgxpool.Pool, sender Sender, logger *slog.Logger) *Mailer {
	return &Mailer{pool: pool, sender: sender, logger: logger}
}

// Send delivers a message unless the recipient is suppressed. Suppression
// returns ErrSuppressed; the caller logs and moves on.
func (m *Mailer) Send(ctx context.Context, msg Message) error {
	suppressed, err := m.isSuppressed(ctx, msg.To)
	if err != nil {
		return err
	}
	if suppressed {
		m.logger.InfoContext(ctx, "email skipped: recipient suppressed", "to", msg.To)
		return ErrSuppressed
	}
	return m.sender.send(ctx, msg)
}

// ProviderEnabled reports whether a real email provider is configured.
func (m *Mailer) ProviderEnabled() bool { return m.sender.Enabled() }

func (m *Mailer) isSuppressed(ctx context.Context, addr string) (bool, error) {
	var exists bool
	err := m.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM email_suppressions WHERE email = $1)
	`, addr).Scan(&exists)
	return exists, err
}

// Suppress adds an address to the suppression list. Called by the Postmark
// bounce/complaint webhook handler. Idempotent.
func (m *Mailer) Suppress(ctx context.Context, addr, reason, detail string) error {
	_, err := m.pool.Exec(ctx, `
		INSERT INTO email_suppressions (email, reason, detail)
		VALUES ($1, $2, NULLIF($3, ''))
		ON CONFLICT (email) DO UPDATE SET
		    reason = EXCLUDED.reason,
		    detail = EXCLUDED.detail,
		    suppressed_at = now()
	`, addr, reason, detail)
	return err
}

// IsSuppressed is the exported check, used by tests and admin tooling.
func (m *Mailer) IsSuppressed(ctx context.Context, addr string) (bool, error) {
	return m.isSuppressed(ctx, addr)
}
