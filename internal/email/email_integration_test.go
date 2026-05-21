package email

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewSenderFallsBackToLogMailer(t *testing.T) {
	s := NewSender("", "from@example.com", discardLogger())
	if _, ok := s.(*LogMailer); !ok {
		t.Fatalf("empty token should yield LogMailer, got %T", s)
	}
	if s.Enabled() {
		t.Error("LogMailer should report Enabled() == false")
	}
}

func TestMailerSkipsSuppressedRecipient(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	addr := "bouncer-" + randomLocalPart(t) + "@example.com"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM email_suppressions WHERE email = $1`, addr)
	})

	mailer := NewMailer(pool, NewLogMailer(discardLogger()), discardLogger())

	// A fresh address is deliverable.
	if err := mailer.Send(ctx, Message{To: addr, Subject: "hi", TextBody: "body"}); err != nil {
		t.Fatalf("send to fresh address: %v", err)
	}

	// After a bounce is recorded, the address is suppressed.
	if err := mailer.Suppress(ctx, addr, "HardBounce", "mailbox does not exist"); err != nil {
		t.Fatalf("Suppress: %v", err)
	}
	suppressed, err := mailer.IsSuppressed(ctx, addr)
	if err != nil {
		t.Fatalf("IsSuppressed: %v", err)
	}
	if !suppressed {
		t.Fatal("address should be suppressed after a bounce")
	}

	// Send now skips with ErrSuppressed.
	if err := mailer.Send(ctx, Message{To: addr, Subject: "hi", TextBody: "body"}); err != ErrSuppressed {
		t.Fatalf("send to suppressed address = %v, want ErrSuppressed", err)
	}
}

func TestSuppressIsIdempotent(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	addr := "dup-" + randomLocalPart(t) + "@example.com"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM email_suppressions WHERE email = $1`, addr)
	})
	mailer := NewMailer(pool, NewLogMailer(discardLogger()), discardLogger())

	if err := mailer.Suppress(ctx, addr, "HardBounce", ""); err != nil {
		t.Fatalf("first Suppress: %v", err)
	}
	// A second complaint for the same address must not error.
	if err := mailer.Suppress(ctx, addr, "SpamComplaint", "user marked as spam"); err != nil {
		t.Fatalf("second Suppress should be idempotent: %v", err)
	}
}

func TestMessageBuilders(t *testing.T) {
	inv := InvitationMessage("teammate@example.com", "Acme", "member", "https://app/invitations/tok")
	if inv.To != "teammate@example.com" {
		t.Errorf("invitation To = %q", inv.To)
	}
	for _, want := range []string{"Acme", "member", "https://app/invitations/tok"} {
		if !contains(inv.TextBody, want) {
			t.Errorf("invitation body missing %q", want)
		}
	}
	wel := WelcomeMessage("new@example.com", "Acme")
	if !contains(wel.TextBody, "Acme") {
		t.Error("welcome body should mention the account name")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func randomLocalPart(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}
