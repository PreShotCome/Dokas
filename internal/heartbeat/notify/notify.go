// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

// Package notify implements heartbeat.Notifier by emailing an account's
// members when a monitor changes state. It lives outside the heartbeat domain
// package so that package stays free of email/account dependencies.
package notify

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/email"
	"github.com/preshotcome/dokaz/internal/heartbeat"
)

// Sender is the slice of *email.Mailer this package uses.
type Sender interface {
	Send(ctx context.Context, m email.Message) error
}

// Accounts is the slice of *account.Store this package uses.
type Accounts interface {
	ListMembers(ctx context.Context, accountID uuid.UUID) ([]account.MembershipWithUser, error)
	GetAccount(ctx context.Context, accountID uuid.UUID) (account.Account, error)
}

// MailNotifier emails every member of a monitor's account on a state change.
type MailNotifier struct {
	mailer   Sender
	accounts Accounts
	baseURL  string
	logger   *slog.Logger
}

func New(mailer Sender, accounts Accounts, baseURL string, logger *slog.Logger) *MailNotifier {
	return &MailNotifier{mailer: mailer, accounts: accounts, baseURL: baseURL, logger: logger}
}

// Notify implements heartbeat.Notifier. It emails each member a down or up
// alert. A suppressed recipient is skipped, not failed, and one bad send never
// blocks the rest — the webhook + audit edge has already been recorded.
func (n *MailNotifier) Notify(ctx context.Context, hb heartbeat.Heartbeat, event string) error {
	members, err := n.accounts.ListMembers(ctx, hb.AccountID)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	acct, err := n.accounts.GetAccount(ctx, hb.AccountID)
	if err != nil {
		return err
	}
	link := n.baseURL + "/heartbeats/" + hb.ID.String()

	for _, m := range members {
		var msg email.Message
		switch event {
		case heartbeat.EventDown:
			msg = email.HeartbeatDownMessage(m.Email, hb.Name, acct.Name, link)
		case heartbeat.EventUp:
			msg = email.HeartbeatUpMessage(m.Email, hb.Name, acct.Name, link)
		default:
			return nil
		}
		if err := n.mailer.Send(ctx, msg); err != nil && !errors.Is(err, email.ErrSuppressed) {
			if n.logger != nil {
				n.logger.WarnContext(ctx, "heartbeat alert email failed", "to", m.Email, "event", event, "err", err)
			}
		}
	}
	return nil
}
