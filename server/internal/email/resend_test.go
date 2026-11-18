package email

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/resend"
	"github.com/stretchr/testify/require"
)

func TestResendTemplates(t *testing.T) {
	t.Parallel()

	for _, template := range RegisteredTemplates {
		t.Run(string(template.Key()), func(t *testing.T) {
			t.Parallel()

			selected, err := resendTemplate(template)
			require.NoError(t, err)
			require.Equal(t, "gram.v2."+string(template.Key()), selected.ID)
			require.NotContains(t, selected.Variables, "email")
			require.NotContains(t, selected.Variables, "is_reminder")
			require.NotContains(t, selected.Variables, "show_estimated_spend")
			require.NotContains(t, selected.Variables, "exhausted")
		})
	}
	for _, tc := range []struct {
		template Template
		alias    string
	}{
		{WeeklyUsageSummary{ShowEstimatedSpend: "true", TotalCostUSD: "10"}, "gram.v2.weekly_usage_summary_with_spend"},
		{DirectoryReviewPending{IsReminder: "true"}, "gram.v2.directory_review_pending_reminder"},
		{OpenRouterChatCreditsThreshold{OrganizationName: "Example Organization", ThresholdPercent: "100", Exhausted: true}, "gram.v2.openrouter_chat_credits_exhausted"},
		{OpenRouterInternalCreditsThreshold{OrganizationName: "Example Organization", ThresholdPercent: "100", Exhausted: true}, "gram.v2.openrouter_internal_credits_exhausted"},
	} {
		selected, err := resendTemplate(tc.template)
		require.NoError(t, err)
		require.Equal(t, tc.alias, selected.ID)
		if tc.template.Key() == TemplateKeyWeeklyUsageSummary {
			require.Equal(t, "10", selected.Variables["total_cost_usd"])
		} else if tc.template.Key() != TemplateKeyDirectoryReviewPending {
			// Both exhausted templates require these variables in their subjects.
			require.Equal(t, map[string]string{
				"organization_name": "Example Organization",
				"threshold_percent": "100",
			}, selected.Variables)
		}
	}
	selected, err := resendTemplate(CustomDomainUnhealthy{Email: "person@example.com"})
	require.NoError(t, err)
	require.Equal(t, "person@example.com", selected.Variables["recipient_email"])
	selected, err = resendTemplate(WeeklyUsageSummary{TotalCostUSD: "10"})
	require.NoError(t, err)
	require.NotContains(t, selected.Variables, "total_cost_usd")
}

type resendSenderFunc func(context.Context, resend.SendInput) error

func (f resendSenderFunc) Send(ctx context.Context, in resend.SendInput) error { return f(ctx, in) }

type contactRecorder struct {
	loops.WorkflowClient
	calls       int
	deadline    time.Time
	hasDeadline bool
	contextErr  func() error
	email       string
	err         error
}

func (c *contactRecorder) UpdateContact(ctx context.Context, input loops.UpdateContactInput) error {
	c.deadline, c.hasDeadline = ctx.Deadline()
	c.contextErr = ctx.Err
	c.email = input.Email
	c.calls++
	return c.err
}

func TestResendDeliveryAndAudience(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		template   Template
		sendErr    error
		contactErr error
		wantCalls  int
	}{
		{"invite audience", TeamInvite{}, nil, nil, 1},
		{"onboarding audience", EnterpriseAdminOnboarding{}, nil, nil, 1},
		{"failed send", TeamInvite{}, errors.New("send failed"), nil, 0},
		{"contact failure stays successful", TeamInvite{}, nil, errors.New("contact failed"), 1},
		{"no audience", WeeklyUsageSummary{}, nil, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			contacts := &contactRecorder{err: tc.contactErr}
			sender := resendSenderFunc(func(_ context.Context, in resend.SendInput) error {
				require.Zero(t, contacts.calls)
				require.Equal(t, "invite/123", in.IdempotencyKey)
				require.Equal(t, []string{"person@example.com"}, in.To)
				require.Equal(t, "platform@notifications.dev.speakeasy.com", in.From)
				require.Equal(t, "platform@speakeasy.com", in.ReplyTo)
				return tc.sendErr
			})
			service := NewResendService(testenv.NewLogger(t), sender, contacts, "platform@notifications.dev.speakeasy.com", "platform@speakeasy.com", true)
			err := service.SendIdempotent(t.Context(), "person@example.com", "invite/123", tc.template)
			if tc.sendErr != nil {
				require.ErrorIs(t, err, tc.sendErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantCalls, contacts.calls)
			if contacts.calls > 0 {
				require.Equal(t, "person@example.com", contacts.email)
				require.True(t, contacts.hasDeadline, "best-effort contact upsert must have a deadline")
				require.WithinDuration(t, time.Now(), contacts.deadline, 3*time.Second)
				require.ErrorIs(t, contacts.contextErr(), context.Canceled)
			}
		})
	}
}

func TestResendDisabled(t *testing.T) {
	t.Parallel()

	service := NewResendService(testenv.NewLogger(t), resend.NewNoop(), nil, "", "", false)
	require.NoError(t, service.Send(t.Context(), "person@example.com", TeamInvite{}))
	require.ErrorIs(t, service.Send(t.Context(), "", TeamInvite{}), ErrEmptyRecipient)
}
