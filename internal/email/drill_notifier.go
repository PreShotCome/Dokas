// Package email — DrillNotifier implements drill.Notifier by emailing the
// account owner when a drill finishes. Sits in the drill.MultiNotifier
// fan-out alongside push (mobile) and alerting (Slack/PagerDuty).
package email

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/drill"
)

// DrillNotifier emails the account owner on drill.completed / drill.failed.
// Best-effort — a mail failure never propagates back to the drill worker.
type DrillNotifier struct {
	mailer   *Mailer
	accounts *account.Store
	drills   *drill.Store
	baseURL  string
	logger   *slog.Logger
}

// NewDrillNotifier constructs an email notifier. Callers should pass a valid
// mailer (production Postmark or the LogMailer in dev); a nil mailer is
// checked at send time and silently no-ops.
func NewDrillNotifier(mailer *Mailer, accounts *account.Store, drills *drill.Store, baseURL string, logger *slog.Logger) *DrillNotifier {
	return &DrillNotifier{mailer: mailer, accounts: accounts, drills: drills, baseURL: baseURL, logger: logger}
}

// NotifyDrill fires DrillCompletedMessage to the account owner. Both completed
// and failed events send — a failed drill is the exact moment the customer
// needs to know, and the drill detail page will explain what broke.
func (n *DrillNotifier) NotifyDrill(ctx context.Context, dr drill.Drill, event, reason string) error {
	if n == nil || n.mailer == nil {
		return nil
	}

	// Resolve the account's owner. If the account has no Stripe customer
	// (all trial accounts), we fall back to any account owner via the
	// memberships table.
	acct, err := n.accounts.GetAccount(ctx, dr.AccountID)
	if err != nil {
		n.warn("email drill notify: get account", "err", err)
		return nil
	}
	owner, err := n.ownerEmail(ctx, acct.ID)
	if err != nil || owner == "" {
		n.warn("email drill notify: no owner email", "account_id", acct.ID, "err", err)
		return nil
	}

	// System notification path — authorized account-wide, not team-scoped.
	target, err := n.drills.GetTarget(ctx, dr.AccountID, dr.TargetID, drill.ScopeAll())
	if err != nil {
		n.warn("email drill notify: get target", "err", err)
		return nil
	}

	verdict := "completed"
	switch event {
	case drill.EventCompleted:
		verdict = "completed"
	case drill.EventFailed:
		verdict = "FAILED"
	}
	link := n.baseURL + "/drills/" + dr.ID.String()

	if err := n.mailer.Send(ctx, DrillCompletedMessage(owner, target.Name, verdict, link)); err != nil &&
		!errors.Is(err, ErrSuppressed) {
		n.warn("email drill notify: send", "to", owner, "err", err)
	}
	return nil
}

// ownerEmail finds the account's owner's email. Uses ListMembers and filters
// to the RoleOwner — one owner per account by construction, so the first hit
// is the answer.
func (n *DrillNotifier) ownerEmail(ctx context.Context, accountID uuid.UUID) (string, error) {
	members, err := n.accounts.ListMembers(ctx, accountID)
	if err != nil {
		return "", err
	}
	for _, m := range members {
		if m.Role == account.RoleOwner {
			return m.Email, nil
		}
	}
	return "", nil
}

func (n *DrillNotifier) warn(msg string, args ...any) {
	if n.logger != nil {
		n.logger.Warn(msg, args...)
	}
}
