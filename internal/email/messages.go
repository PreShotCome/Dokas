package email

import "fmt"

// InvitationMessage builds the email sent to an invited teammate.
func InvitationMessage(to, accountName, role, acceptLink string) Message {
	return Message{
		To:      to,
		Subject: fmt.Sprintf("You've been invited to %s on Selket", accountName),
		TextBody: fmt.Sprintf(`You've been invited to join %s on Selket as %s.

Accept the invitation:
%s

This link expires in 7 days. If you weren't expecting this, you can ignore it.

— Selket
`, accountName, role, acceptLink),
	}
}

// VerifyEmailMessage builds the email-verification message sent at signup and
// on request from the verify-your-email banner.
func VerifyEmailMessage(to, verifyLink string) Message {
	return Message{
		To:      to,
		Subject: "Verify your email for Selket",
		TextBody: fmt.Sprintf(`Confirm your email address to finish setting up your
Selket account.

Verify your email:
%s

This link expires in 24 hours. If you didn't create a Selket
account, you can ignore this email.

— Selket
`, verifyLink),
	}
}

// MagicLinkMessage builds the passwordless sign-in email.
func MagicLinkMessage(to, link string) Message {
	return Message{
		To:      to,
		Subject: "Your Selket sign-in link",
		TextBody: fmt.Sprintf(`Use this link to sign in to Selket:

%s

The link expires in 15 minutes and can be used once. If you didn't ask
to sign in, you can ignore this email.

— Selket
`, link),
	}
}

// HeartbeatDownMessage builds the alert sent when a backup check-in goes
// overdue (or a job reports an explicit failure).
func HeartbeatDownMessage(to, monitorName, accountName, link string) Message {
	return Message{
		To:      to,
		Subject: fmt.Sprintf("[Soteria] DOWN: %s", monitorName),
		TextBody: fmt.Sprintf(`A backup check-in is overdue.

Monitor: %s
Workspace: %s

Soteria has not received the expected check-in for this monitor, so the
backup job it watches may have failed or stopped running. Investigate the
job, then check the monitor:

%s

You'll get an "UP" email automatically once a check-in arrives again.

— Soteria
`, monitorName, accountName, link),
	}
}

// HeartbeatUpMessage builds the recovery email sent when a previously-down
// monitor receives a check-in again.
func HeartbeatUpMessage(to, monitorName, accountName, link string) Message {
	return Message{
		To:      to,
		Subject: fmt.Sprintf("[Soteria] UP: %s", monitorName),
		TextBody: fmt.Sprintf(`A backup check-in has recovered.

Monitor: %s
Workspace: %s

Soteria received a check-in for this monitor again — it's back to healthy.

%s

— Soteria
`, monitorName, accountName, link),
	}
}

// WelcomeMessage builds the email sent to a new account owner at signup.
func WelcomeMessage(to, accountName string) Message {
	return Message{
		To:      to,
		Subject: "Welcome to Selket",
		TextBody: fmt.Sprintf(`Welcome to Selket.

Your workspace "%s" is ready. Connect a database backup and run your first
drill — we'll restore it in an isolated sandbox, run your assertions, and
produce signed evidence that the backup is actually restorable.

— Selket
`, accountName),
	}
}
