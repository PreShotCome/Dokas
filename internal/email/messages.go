package email

import "fmt"

// InvitationMessage builds the email sent to an invited teammate.
func InvitationMessage(to, accountName, role, acceptLink string) Message {
	return Message{
		To:      to,
		Subject: fmt.Sprintf("You've been invited to %s on Restore Drill", accountName),
		TextBody: fmt.Sprintf(`You've been invited to join %s on Restore Drill as %s.

Accept the invitation:
%s

This link expires in 7 days. If you weren't expecting this, you can ignore it.

— Restore Drill
`, accountName, role, acceptLink),
	}
}

// VerifyEmailMessage builds the email-verification message sent at signup and
// on request from the verify-your-email banner.
func VerifyEmailMessage(to, verifyLink string) Message {
	return Message{
		To:      to,
		Subject: "Verify your email for Restore Drill",
		TextBody: fmt.Sprintf(`Confirm your email address to finish setting up your
Restore Drill account.

Verify your email:
%s

This link expires in 24 hours. If you didn't create a Restore Drill
account, you can ignore this email.

— Restore Drill
`, verifyLink),
	}
}

// MagicLinkMessage builds the passwordless sign-in email.
func MagicLinkMessage(to, link string) Message {
	return Message{
		To:      to,
		Subject: "Your Restore Drill sign-in link",
		TextBody: fmt.Sprintf(`Use this link to sign in to Restore Drill:

%s

The link expires in 15 minutes and can be used once. If you didn't ask
to sign in, you can ignore this email.

— Restore Drill
`, link),
	}
}

// WelcomeMessage builds the email sent to a new account owner at signup.
func WelcomeMessage(to, accountName string) Message {
	return Message{
		To:      to,
		Subject: "Welcome to Restore Drill",
		TextBody: fmt.Sprintf(`Welcome to Restore Drill.

Your workspace "%s" is ready. Connect a database backup and run your first
drill — we'll restore it in an isolated sandbox, run your assertions, and
produce signed evidence that the backup is actually restorable.

— Restore Drill
`, accountName),
	}
}
