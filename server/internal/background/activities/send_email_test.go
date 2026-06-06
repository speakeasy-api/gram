package activities

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type stubEmailSender struct {
	got  email.ScheduledEmail
	err  error
	sent bool
}

func (s *stubEmailSender) SendScheduled(_ context.Context, msg email.ScheduledEmail) error {
	s.sent = true
	s.got = msg
	return s.err
}

func TestSendEmail_DispatchesToSender(t *testing.T) {
	t.Parallel()

	sender := &stubEmailSender{got: email.ScheduledEmail{}, err: nil, sent: false}
	act := NewSendEmail(testenv.NewLogger(t), sender)

	msg := email.ScheduledEmail{
		Recipient:       "carol@example.com",
		TransactionalID: "tmpl-id",
		Variables:       map[string]string{"a": "b"},
		AddToAudience:   true,
	}

	err := act.Do(t.Context(), msg)
	require.NoError(t, err)
	require.True(t, sender.sent)
	require.Equal(t, msg, sender.got)
}

func TestSendEmail_PropagatesSenderError(t *testing.T) {
	t.Parallel()

	boom := errors.New("loops boom")
	sender := &stubEmailSender{got: email.ScheduledEmail{}, err: boom, sent: false}
	act := NewSendEmail(testenv.NewLogger(t), sender)

	err := act.Do(t.Context(), email.ScheduledEmail{
		Recipient:       "carol@example.com",
		TransactionalID: "tmpl-id",
		Variables:       nil,
		AddToAudience:   false,
	})
	require.ErrorIs(t, err, boom)
}

func TestSendEmail_MissingSenderReturnsError(t *testing.T) {
	t.Parallel()

	act := NewSendEmail(testenv.NewLogger(t), nil)

	err := act.Do(t.Context(), email.ScheduledEmail{
		Recipient:       "carol@example.com",
		TransactionalID: "tmpl-id",
		Variables:       nil,
		AddToAudience:   false,
	})
	require.Error(t, err)
}
