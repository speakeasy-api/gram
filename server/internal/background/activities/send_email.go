package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/email"
)

// EmailSender is the slice of the email service the SendEmail activity depends
// on. Keeping it an interface lets tests assert on dispatched emails without a
// real Loops client.
type EmailSender interface {
	SendScheduled(ctx context.Context, msg email.ScheduledEmail) error
}

type SendEmail struct {
	logger *slog.Logger
	email  EmailSender
}

func NewSendEmail(logger *slog.Logger, emailService EmailSender) *SendEmail {
	return &SendEmail{
		logger: logger.With(attr.SlogComponent("send_email")),
		email:  emailService,
	}
}

func (a *SendEmail) Do(ctx context.Context, msg email.ScheduledEmail) error {
	if a.email == nil {
		return fmt.Errorf("send email: email service is not configured")
	}

	if err := a.email.SendScheduled(ctx, msg); err != nil {
		return fmt.Errorf("send scheduled email: %w", err)
	}

	return nil
}
