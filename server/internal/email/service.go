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

// ErrUnregisteredTemplate is returned when the deployment has no Loops ID for
// the supplied template key.
var ErrUnregisteredTemplate = errors.New("email: template has no configured transactional ID")

// Service is the application-facing facade for sending transactional emails.
// Callers depend on this type instead of the underlying transport so we can
// swap providers without touching feature code.
type Service struct {
	logger *slog.Logger
	sender loops.Client
	ids    TemplateIDs
}

// NewService returns an email Service backed by the supplied Loops client.
// The sender is expected to be a usable client — pass loops.New(...) which
// returns a no-op when the API key is unset.
func NewService(logger *slog.Logger, sender loops.Client, ids TemplateIDs) *Service {
	return &Service{
		logger: logger.With(attr.SlogComponent("email")),
		sender: sender,
		ids:    ids,
	}
}

// Send dispatches a transactional email rendered from the supplied template.
// The template carries strongly typed variables and a stable key resolved
// against the deployment's environment-specific Loops IDs.
func (s *Service) Send(ctx context.Context, recipient string, template Template) error {
	return s.SendIdempotent(ctx, recipient, "", template)
}

// SendIdempotent dispatches a transactional email with a provider-level
// idempotency key.
func (s *Service) SendIdempotent(ctx context.Context, recipient string, idempotencyKey string, template Template) error {
	if recipient == "" {
		return ErrEmptyRecipient
	}
	// Empty IDs are accepted only when startup has established that Loops is
	// disabled, preserving the transport's no-op behavior in local development.
	if len(s.ids) == 0 {
		return nil
	}

	key := template.Key()
	id := s.ids[key]
	if id == "" {
		return fmt.Errorf("%w: %q", ErrUnregisteredTemplate, key)
	}

	if err := s.sender.SendTransactional(ctx, loops.SendTransactionalInput{
		TransactionalID: string(id),
		Email:           recipient,
		DataVariables:   template.Variables(),
		AddToAudience:   template.AddToAudience(),
		IdempotencyKey:  idempotencyKey,
	}); err != nil {
		return fmt.Errorf("send email %q: %w", id, err)
	}

	return nil
}
