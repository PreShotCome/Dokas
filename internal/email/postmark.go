package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// LogMailer is the dev/CI sender: it logs the message instead of sending.
// During local development the invitation link is therefore visible in the
// server log, exactly as before this package existed.
type LogMailer struct{ logger *slog.Logger }

func NewLogMailer(logger *slog.Logger) *LogMailer { return &LogMailer{logger: logger} }

func (l *LogMailer) Enabled() bool { return false }

func (l *LogMailer) send(ctx context.Context, m Message) error {
	l.logger.InfoContext(ctx, "email (log mailer — not actually sent)",
		"to", m.To, "subject", m.Subject, "body", m.TextBody)
	return nil
}

// PostmarkMailer sends through Postmark's REST API.
type PostmarkMailer struct {
	token string
	from  string
	http  *http.Client
}

// NewSender returns a PostmarkMailer when token is set, otherwise a
// LogMailer. from is the verified sender address.
func NewSender(token, from string, logger *slog.Logger) Sender {
	if token == "" {
		return NewLogMailer(logger)
	}
	return &PostmarkMailer{
		token: token,
		from:  from,
		http:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *PostmarkMailer) Enabled() bool { return true }

func (p *PostmarkMailer) send(ctx context.Context, m Message) error {
	payload, err := json.Marshal(map[string]string{
		"From":          p.from,
		"To":            m.To,
		"Subject":       m.Subject,
		"TextBody":      m.TextBody,
		"MessageStream": "outbound",
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.postmarkapp.com/email", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Postmark-Server-Token", p.token)

	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("postmark send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("postmark send: status %s", resp.Status)
	}
	return nil
}
