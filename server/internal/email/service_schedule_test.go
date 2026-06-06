package email

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
)

type mockScheduler struct {
	mock.Mock
}

func (m *mockScheduler) EnqueueEmail(ctx context.Context, msg ScheduledEmail, delay time.Duration) error {
	args := m.Called(ctx, msg, delay)
	return args.Error(0)
}

func (m *mockScheduler) ScheduleRepeatEmail(ctx context.Context, msg ScheduledEmail, period time.Duration) error {
	args := m.Called(ctx, msg, period)
	return args.Error(0)
}

func newMockScheduler(t *testing.T) *mockScheduler {
	t.Helper()
	m := &mockScheduler{}
	t.Cleanup(func() { m.AssertExpectations(t) })
	return m
}

func TestService_Enqueue_SchedulesFlattenedTemplate(t *testing.T) {
	t.Parallel()

	scheduler := newMockScheduler(t)
	svc := NewService(testenv.NewLogger(t), newMockSender(t))
	svc.SetScheduler(scheduler)

	tmpl := TeamInvite{
		InviteLink:       "https://app.gram.sh/invite?token=xyz",
		InviterName:      "Bob",
		InviterEmail:     "bob@example.com",
		OrganizationName: "Acme Corp",
	}

	expected := ScheduledEmail{
		Recipient:       "carol@example.com",
		TransactionalID: string(transactionalIDTeamInvite),
		Variables: map[string]string{
			"invite_link":       "https://app.gram.sh/invite?token=xyz",
			"inviter_name":      "Bob",
			"inviter_email":     "bob@example.com",
			"organization_name": "Acme Corp",
		},
		AddToAudience: true,
	}

	scheduler.On("EnqueueEmail", mock.Anything, expected, time.Hour).Return(nil).Once()

	err := svc.Enqueue(t.Context(), "carol@example.com", tmpl, time.Hour)
	require.NoError(t, err)
}

func TestService_Enqueue_NoSchedulerReturnsSentinel(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), newMockSender(t))

	err := svc.Enqueue(t.Context(), "carol@example.com", TeamInvite{
		InviteLink:       "https://example.com",
		InviterName:      "Bob",
		InviterEmail:     "bob@example.com",
		OrganizationName: "Acme Corp",
	}, time.Minute)
	require.ErrorIs(t, err, ErrNoScheduler)
}

func TestService_Enqueue_EmptyRecipientReturnsSentinel(t *testing.T) {
	t.Parallel()

	scheduler := newMockScheduler(t)
	svc := NewService(testenv.NewLogger(t), newMockSender(t))
	svc.SetScheduler(scheduler)

	// scheduler must not be invoked when validation fails.
	err := svc.Enqueue(t.Context(), "", TeamInvite{
		InviteLink:       "https://example.com",
		InviterName:      "Bob",
		InviterEmail:     "bob@example.com",
		OrganizationName: "Acme Corp",
	}, time.Minute)
	require.ErrorIs(t, err, ErrEmptyRecipient)
}

func TestService_Enqueue_PropagatesSchedulerError(t *testing.T) {
	t.Parallel()

	scheduler := newMockScheduler(t)
	svc := NewService(testenv.NewLogger(t), newMockSender(t))
	svc.SetScheduler(scheduler)

	boom := errors.New("temporal boom")
	scheduler.On("EnqueueEmail", mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()

	err := svc.Enqueue(t.Context(), "carol@example.com", TeamInvite{
		InviteLink:       "https://example.com",
		InviterName:      "Bob",
		InviterEmail:     "bob@example.com",
		OrganizationName: "Acme Corp",
	}, time.Minute)
	require.ErrorIs(t, err, boom)
	require.Contains(t, err.Error(), string(transactionalIDTeamInvite))
}

func TestService_ScheduleRepeatEmail_SchedulesWithPeriod(t *testing.T) {
	t.Parallel()

	scheduler := newMockScheduler(t)
	svc := NewService(testenv.NewLogger(t), newMockSender(t))
	svc.SetScheduler(scheduler)

	expected := ScheduledEmail{
		Recipient:       "carol@example.com",
		TransactionalID: string(transactionalIDTeamInvite),
		Variables: map[string]string{
			"invite_link":       "https://example.com",
			"inviter_name":      "Bob",
			"inviter_email":     "bob@example.com",
			"organization_name": "Acme Corp",
		},
		AddToAudience: true,
	}

	scheduler.On("ScheduleRepeatEmail", mock.Anything, expected, 24*time.Hour).Return(nil).Once()

	err := svc.ScheduleRepeatEmail(t.Context(), "carol@example.com", TeamInvite{
		InviteLink:       "https://example.com",
		InviterName:      "Bob",
		InviterEmail:     "bob@example.com",
		OrganizationName: "Acme Corp",
	}, 24*time.Hour)
	require.NoError(t, err)
}

func TestService_ScheduleRepeatEmail_NonPositivePeriodReturnsSentinel(t *testing.T) {
	t.Parallel()

	scheduler := newMockScheduler(t)
	svc := NewService(testenv.NewLogger(t), newMockSender(t))
	svc.SetScheduler(scheduler)

	err := svc.ScheduleRepeatEmail(t.Context(), "carol@example.com", TeamInvite{
		InviteLink:       "https://example.com",
		InviterName:      "Bob",
		InviterEmail:     "bob@example.com",
		OrganizationName: "Acme Corp",
	}, 0)
	require.ErrorIs(t, err, ErrNonPositivePeriod)
}

func TestService_ScheduleRepeatEmail_NoSchedulerReturnsSentinel(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), newMockSender(t))

	err := svc.ScheduleRepeatEmail(t.Context(), "carol@example.com", TeamInvite{
		InviteLink:       "https://example.com",
		InviterName:      "Bob",
		InviterEmail:     "bob@example.com",
		OrganizationName: "Acme Corp",
	}, time.Hour)
	require.ErrorIs(t, err, ErrNoScheduler)
}

func TestService_SendScheduled_DispatchesFlattenedEmail(t *testing.T) {
	t.Parallel()

	sender := newMockSender(t)
	svc := NewService(testenv.NewLogger(t), sender)

	expected := loops.SendTransactionalInput{
		TransactionalID: "tmpl-id",
		Email:           "carol@example.com",
		DataVariables:   map[string]string{"a": "b"},
		AddToAudience:   false,
	}
	sender.On("SendTransactional", mock.Anything, expected).Return(nil).Once()

	err := svc.SendScheduled(t.Context(), ScheduledEmail{
		Recipient:       "carol@example.com",
		TransactionalID: "tmpl-id",
		Variables:       map[string]string{"a": "b"},
		AddToAudience:   false,
	})
	require.NoError(t, err)
}

func TestService_SendScheduled_EmptyRecipientReturnsSentinel(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), newMockSender(t))

	err := svc.SendScheduled(t.Context(), ScheduledEmail{
		Recipient:       "",
		TransactionalID: "tmpl-id",
		Variables:       nil,
		AddToAudience:   false,
	})
	require.ErrorIs(t, err, ErrEmptyRecipient)
}

func TestService_SendScheduled_UnregisteredTemplateReturnsSentinel(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), newMockSender(t))

	err := svc.SendScheduled(t.Context(), ScheduledEmail{
		Recipient:       "carol@example.com",
		TransactionalID: "",
		Variables:       nil,
		AddToAudience:   false,
	})
	require.ErrorIs(t, err, ErrUnregisteredTemplate)
}
