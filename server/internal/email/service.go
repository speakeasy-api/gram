package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
)

// ErrEmptyRecipient is returned when Send is called without a recipient
// address. It is a sentinel so tests and callers can distinguish input errors
// from transport failures.
var ErrEmptyRecipient = errors.New("email: recipient is required")

// ErrUnregisteredTemplate is returned when the supplied template's
// TransactionalID is empty, indicating the template has not been registered
// with a Loops transactional ID.
var ErrUnregisteredTemplate = errors.New("email: template has no transactional ID")

// Service is the application-facing facade for sending transactional emails.
// Callers depend on this type instead of the underlying transport so we can
// swap providers without touching feature code.
type Service struct {
	logger *slog.Logger
	sender loops.Client
}

// NewService returns an email Service backed by the supplied Loops client.
// The sender is expected to be a usable client — pass loops.New(...) which
// returns a no-op when the API key is unset.
func NewService(logger *slog.Logger, sender loops.Client) *Service {
	return &Service{
		logger: logger.With(attr.SlogComponent("email")),
		sender: sender,
	}
}

// Send dispatches a transactional email rendered from the supplied template.
// The template carries the strongly typed variables and the transactional ID
// it targets, so a misuse such as passing the wrong variable shape is a
// compile-time error.
func (s *Service) Send(ctx context.Context, recipient string, template Template) error {
	if recipient == "" {
		return ErrEmptyRecipient
	}

	id := template.TransactionalID()
	if id == "" {
		return ErrUnregisteredTemplate
	}

	if err := s.sender.SendTransactional(ctx, loops.SendTransactionalInput{
		TransactionalID: string(id),
		Email:           recipient,
		DataVariables:   template.Variables(),
		AddToAudience:   template.AddToAudience(),
	}); err != nil {
		return fmt.Errorf("send email %q: %w", id, err)
	}

	return nil
}
