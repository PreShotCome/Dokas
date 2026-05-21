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
