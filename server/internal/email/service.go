package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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

// ErrNoScheduler is returned by Enqueue and ScheduleRepeatEmail when the
// Service has no scheduler configured. Deferred delivery requires a background
// execution engine to be wired in via SetScheduler.
var ErrNoScheduler = errors.New("email: no scheduler configured for deferred delivery")

// ErrNonPositivePeriod is returned by ScheduleRepeatEmail when the supplied
// period is zero or negative — a repeating schedule needs a positive interval.
var ErrNonPositivePeriod = errors.New("email: repeat period must be positive")

// ScheduledEmail is the serializable description of a single transactional
// email. Templates are Go interfaces and cannot cross a workflow boundary, so
// deferred delivery carries this flattened form instead. Callers build it via
// Enqueue / ScheduleRepeatEmail; the Scheduler hands it back to SendScheduled
// when the email is finally dispatched.
type ScheduledEmail struct {
	// Recipient is the destination email address.
	Recipient string
	// TransactionalID is the Loops template identifier to render.
	TransactionalID string
	// Variables are the merge variables substituted into the template. May be
	// nil for templates with no dynamic content.
	Variables map[string]string
	// AddToAudience reports whether sending should upsert the recipient into
	// the Loops audience.
	AddToAudience bool
}

// Scheduler defers email delivery to a background execution engine. It is
// implemented by the background package (backed by Temporal) and injected via
// SetScheduler so the email package stays free of any Temporal dependency.
type Scheduler interface {
	// EnqueueEmail schedules msg to be sent once, after delay has elapsed
	// relative to now. A delay of zero sends as soon as a worker picks it up.
	EnqueueEmail(ctx context.Context, msg ScheduledEmail, delay time.Duration) error
	// ScheduleRepeatEmail schedules msg to be sent repeatedly, once every
	// period, until the schedule is removed. Calling it again for the same
	// recipient and template updates the existing schedule in place.
	ScheduleRepeatEmail(ctx context.Context, msg ScheduledEmail, period time.Duration) error
}

// Service is the application-facing facade for sending transactional emails.
// Callers depend on this type instead of the underlying transport so we can
// swap providers without touching feature code.
type Service struct {
	logger    *slog.Logger
	sender    loops.Client
	scheduler Scheduler
}

// NewService returns an email Service backed by the supplied Loops client.
// The sender is expected to be a usable client — pass loops.New(...) which
// returns a no-op when the API key is unset.
//
// Deferred delivery (Enqueue, ScheduleRepeatEmail) additionally requires a
// Scheduler to be wired in via SetScheduler; synchronous Send works without
// one.
func NewService(logger *slog.Logger, sender loops.Client) *Service {
	return &Service{
		logger:    logger.With(attr.SlogComponent("email")),
		sender:    sender,
		scheduler: nil,
	}
}

// SetScheduler wires the background scheduler used by Enqueue and
// ScheduleRepeatEmail. It is kept separate from NewService because the
// scheduler (Temporal-backed) is constructed after the email Service in the
// dependency graph.
func (s *Service) SetScheduler(scheduler Scheduler) {
	s.scheduler = scheduler
}

// Send dispatches a transactional email rendered from the supplied template.
// The template carries the strongly typed variables and the transactional ID
// it targets, so a misuse such as passing the wrong variable shape is a
// compile-time error.
func (s *Service) Send(ctx context.Context, recipient string, template Template) error {
	msg, err := buildScheduledEmail(recipient, template)
	if err != nil {
		return err
	}

	return s.SendScheduled(ctx, msg)
}

// SendScheduled dispatches an email from its serializable form. It is the
// delivery path used by the background worker that drains enqueued and repeated
// emails; feature code should call Send with a typed template instead.
func (s *Service) SendScheduled(ctx context.Context, msg ScheduledEmail) error {
	if msg.Recipient == "" {
		return ErrEmptyRecipient
	}
	if msg.TransactionalID == "" {
		return ErrUnregisteredTemplate
	}

	if err := s.sender.SendTransactional(ctx, loops.SendTransactionalInput{
		TransactionalID: msg.TransactionalID,
		Email:           msg.Recipient,
		DataVariables:   msg.Variables,
		AddToAudience:   msg.AddToAudience,
	}); err != nil {
		return fmt.Errorf("send email %q: %w", msg.TransactionalID, err)
	}

	return nil
}

// Enqueue schedules a transactional email to be sent once, after delay has
// elapsed relative to now. A delay of zero sends as soon as a worker picks up
// the work. The email is rendered from template at enqueue time and the
// resulting variables are carried through to delivery.
func (s *Service) Enqueue(ctx context.Context, recipient string, template Template, delay time.Duration) error {
	if s.scheduler == nil {
		return ErrNoScheduler
	}

	msg, err := buildScheduledEmail(recipient, template)
	if err != nil {
		return err
	}

	if err := s.scheduler.EnqueueEmail(ctx, msg, delay); err != nil {
		return fmt.Errorf("enqueue email %q: %w", msg.TransactionalID, err)
	}

	return nil
}

// ScheduleRepeatEmail schedules a transactional email to be sent repeatedly,
// once every period, until the schedule is removed. Scheduling again for the
// same recipient and template updates the existing schedule rather than
// creating a duplicate.
func (s *Service) ScheduleRepeatEmail(ctx context.Context, recipient string, template Template, period time.Duration) error {
	if s.scheduler == nil {
		return ErrNoScheduler
	}
	if period <= 0 {
		return ErrNonPositivePeriod
	}

	msg, err := buildScheduledEmail(recipient, template)
	if err != nil {
		return err
	}

	if err := s.scheduler.ScheduleRepeatEmail(ctx, msg, period); err != nil {
		return fmt.Errorf("schedule repeat email %q: %w", msg.TransactionalID, err)
	}

	return nil
}

// buildScheduledEmail validates the recipient and template and flattens them
// into the serializable ScheduledEmail used by every delivery path.
func buildScheduledEmail(recipient string, template Template) (ScheduledEmail, error) {
	if recipient == "" {
		return ScheduledEmail{}, ErrEmptyRecipient
	}

	id := template.TransactionalID()
	if id == "" {
		return ScheduledEmail{}, ErrUnregisteredTemplate
	}

	return ScheduledEmail{
		Recipient:       recipient,
		TransactionalID: string(id),
		Variables:       template.Variables(),
		AddToAudience:   template.AddToAudience(),
	}, nil
}
