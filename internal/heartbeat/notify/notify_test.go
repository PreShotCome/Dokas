package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/account"
	"github.com/preshotcome/anything/internal/email"
	"github.com/preshotcome/anything/internal/heartbeat"
)

type fakeSender struct {
	sent       []email.Message
	suppressed map[string]bool
}

func (f *fakeSender) Send(_ context.Context, m email.Message) error {
	if f.suppressed[m.To] {
		return email.ErrSuppressed
	}
	f.sent = append(f.sent, m)
	return nil
}

type fakeAccounts struct {
	members []account.MembershipWithUser
	acct    account.Account
}

func (f *fakeAccounts) ListMembers(_ context.Context, _ uuid.UUID) ([]account.MembershipWithUser, error) {
	return f.members, nil
}
func (f *fakeAccounts) GetAccount(_ context.Context, _ uuid.UUID) (account.Account, error) {
	return f.acct, nil
}

func newFakes() (*fakeSender, *fakeAccounts) {
	s := &fakeSender{suppressed: map[string]bool{}}
	a := &fakeAccounts{
		acct: account.Account{Name: "Acme"},
		members: []account.MembershipWithUser{
			{Email: "a@example.com"},
			{Email: "b@example.com"},
		},
	}
	return s, a
}

func TestNotifyDownEmailsAllMembers(t *testing.T) {
	s, a := newFakes()
	n := New(s, a, "https://app.example", nil)
	hb := heartbeat.Heartbeat{ID: uuid.New(), Name: "nightly backup"}

	if err := n.Notify(context.Background(), hb, heartbeat.EventDown); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(s.sent) != 2 {
		t.Fatalf("sent %d emails, want 2", len(s.sent))
	}
	if !strings.Contains(s.sent[0].Subject, "DOWN") || !strings.Contains(s.sent[0].Subject, "nightly backup") {
		t.Fatalf("unexpected subject: %q", s.sent[0].Subject)
	}
	if !strings.Contains(s.sent[0].TextBody, "https://app.example/heartbeats/"+hb.ID.String()) {
		t.Fatal("body missing monitor link")
	}
}

func TestNotifyUpUsesRecoverySubject(t *testing.T) {
	s, a := newFakes()
	n := New(s, a, "https://app.example", nil)
	hb := heartbeat.Heartbeat{ID: uuid.New(), Name: "nightly backup"}

	if err := n.Notify(context.Background(), hb, heartbeat.EventUp); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(s.sent) != 2 || !strings.Contains(s.sent[0].Subject, "UP") {
		t.Fatalf("up alert subjects wrong: %+v", s.sent)
	}
}

func TestNotifySkipsSuppressed(t *testing.T) {
	s, a := newFakes()
	s.suppressed["a@example.com"] = true
	n := New(s, a, "https://app.example", nil)

	if err := n.Notify(context.Background(), heartbeat.Heartbeat{ID: uuid.New(), Name: "x"}, heartbeat.EventDown); err != nil {
		t.Fatalf("notify: %v", err)
	}
	// b@ still gets the mail; a@ is silently skipped (not an error).
	if len(s.sent) != 1 || s.sent[0].To != "b@example.com" {
		t.Fatalf("suppression not honoured: %+v", s.sent)
	}
}

func TestNotifyUnknownEventSendsNothing(t *testing.T) {
	s, a := newFakes()
	n := New(s, a, "https://app.example", nil)
	if err := n.Notify(context.Background(), heartbeat.Heartbeat{ID: uuid.New()}, "heartbeat.sideways"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(s.sent) != 0 {
		t.Fatalf("unknown event sent %d emails, want 0", len(s.sent))
	}
}
