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
