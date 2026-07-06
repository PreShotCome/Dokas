// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package email

import (
	"fmt"

	"github.com/preshotcome/dokaz/internal/branding"
)

// InvitationMessage builds the email sent to an invited teammate.
func InvitationMessage(to, accountName, role, acceptLink string) Message {
	p := branding.ProductName
	return Message{
		To:      to,
		Subject: fmt.Sprintf("You've been invited to %s on %s", accountName, p),
		TextBody: fmt.Sprintf(`You've been invited to join %s on %s as %s.

Accept the invitation:
%s

This link expires in 7 days. If you weren't expecting this, you can ignore it.

— %s
`, accountName, p, role, acceptLink, p),
	}
}

// VerifyEmailMessage builds the email-verification message sent at signup and
// on request from the verify-your-email banner.
func VerifyEmailMessage(to, verifyLink string) Message {
	p := branding.ProductName
	return Message{
		To:      to,
		Subject: fmt.Sprintf("Verify your email for %s", p),
		TextBody: fmt.Sprintf(`Confirm your email address to finish setting up your
%s account.

Verify your email:
%s

This link expires in 24 hours. If you didn't create a %s
account, you can ignore this email.

— %s
`, p, verifyLink, p, p),
	}
}

// MagicLinkMessage builds the passwordless sign-in email.
func MagicLinkMessage(to, link string) Message {
	p := branding.ProductName
	return Message{
		To:      to,
		Subject: fmt.Sprintf("Your %s sign-in link", p),
		TextBody: fmt.Sprintf(`Use this link to sign in to %s:

%s

The link expires in 15 minutes and can be used once. If you didn't ask
to sign in, you can ignore this email.

— %s
`, p, link, p),
	}
}

// HeartbeatDownMessage builds the alert sent when a backup check-in goes
// overdue (or a job reports an explicit failure).
func HeartbeatDownMessage(to, monitorName, accountName, link string) Message {
	p := branding.ProductName
	return Message{
		To:      to,
		Subject: fmt.Sprintf("[%s] DOWN: %s", p, monitorName),
		TextBody: fmt.Sprintf(`A backup check-in is overdue.

Monitor: %s
Workspace: %s

%s has not received the expected check-in for this monitor, so the
backup job it watches may have failed or stopped running. Investigate the
job, then check the monitor:

%s

You'll get an "UP" email automatically once a check-in arrives again.

— %s
`, monitorName, accountName, p, link, p),
	}
}

// HeartbeatUpMessage builds the recovery email sent when a previously-down
// monitor receives a check-in again.
func HeartbeatUpMessage(to, monitorName, accountName, link string) Message {
	p := branding.ProductName
	return Message{
		To:      to,
		Subject: fmt.Sprintf("[%s] UP: %s", p, monitorName),
		TextBody: fmt.Sprintf(`A backup check-in has recovered.

Monitor: %s
Workspace: %s

%s received a check-in for this monitor again — it's back to healthy.

%s

— %s
`, monitorName, accountName, p, link, p),
	}
}

// WelcomeMessage builds the email sent to a new account owner at signup.
func WelcomeMessage(to, accountName string) Message {
	p := branding.ProductName
	return Message{
		To:      to,
		Subject: fmt.Sprintf("Welcome to %s", p),
		TextBody: fmt.Sprintf(`Welcome to %s.

Your workspace "%s" is ready. Connect a database backup and run your first
drill — we'll restore it in an isolated sandbox, run your assertions, and
produce signed evidence that the backup is actually restorable.

— %s
`, p, accountName, p),
	}
}

// PaymentFailedMessage is the dunning email sent when Stripe reports
// invoice.payment_failed on a subscription. It must not scold — a declined
// card is usually a bank-side false-positive, not a customer problem — but
// it must name the consequence of ignoring it (subscription cancellation,
// then a 7-day grace, then purge).
func PaymentFailedMessage(to, accountName, portalLink string) Message {
	p := branding.ProductName
	return Message{
		To:      to,
		Subject: fmt.Sprintf("Payment issue on your %s subscription", p),
		TextBody: fmt.Sprintf(`We couldn't process the last payment on your %s subscription for "%s".

Card declines usually clear themselves on the next retry — your bank may
have flagged a routine charge — but we wanted to let you know so it
doesn't slip past.

Update your card here, and %s will re-attempt the payment automatically:

%s

If nothing changes after Stripe's retries, the subscription will be
canceled and your workspace will drop into a 7-day grace window. Signed
reports stay available throughout — no drills stop.

— %s
`, p, accountName, p, portalLink, p),
	}
}

// DrillCompletedMessage builds the email sent when a drill finishes. It
// carries the outcome verdict, the database name, and a link back to the
// signed report — closing the loop on the welcome email's promise that "we'll
// email you a signed PDF."
func DrillCompletedMessage(to, targetName, verdict, drillLink string) Message {
	p := branding.ProductName
	return Message{
		To:      to,
		Subject: fmt.Sprintf("%s drill %s: %s", p, verdict, targetName),
		TextBody: fmt.Sprintf(`Your latest %s drill on "%s" finished with verdict: %s.

Open the drill for the signed Proof-of-Recovery PDF, the assertion
receipts, and (if it failed) the captured restore log:

%s

— %s
`, p, targetName, verdict, drillLink, p),
	}
}
