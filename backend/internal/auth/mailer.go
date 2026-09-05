package auth

import (
	"context"
	"log/slog"
)

// Mailer delivers account emails. SMTP wiring is deferred; dev and test use
// implementations that log or capture instead of sending.
type Mailer interface {
	SendPasswordReset(ctx context.Context, email, token string) error
}

// LogMailer is the dev implementation: it logs the reset token instead of
// sending an email.
type LogMailer struct {
	Log *slog.Logger
}

func (m LogMailer) SendPasswordReset(_ context.Context, email, token string) error {
	m.Log.Info("password reset requested (dev mailer, no email sent)", "email", email, "token", token)
	return nil
}
