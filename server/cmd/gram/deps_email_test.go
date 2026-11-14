package gram

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNewEmailService_DisabledAcceptsEmptyIDs(t *testing.T) {
	t.Parallel()

	svc, err := newEmailService(t.Context(), newEmailCLIContext(t, "", ""), testenv.NewLogger(t), nil)
	require.NoError(t, err)
	require.NoError(t, svc.Send(t.Context(), "person@example.com", email.TeamInvite{
		InviteLink:       "https://example.com/invite",
		InviterName:      "Example User",
		InviterEmail:     "person@example.com",
		OrganizationName: "Example Organization",
	}))
}

func TestNewEmailService_EnabledValidatesIDs(t *testing.T) {
	t.Parallel()

	_, err := newEmailService(t.Context(), newEmailCLIContext(t, "test-key", ""), testenv.NewLogger(t), nil)
	require.ErrorIs(t, err, email.ErrEmptyTemplateIDs)
}

func newEmailCLIContext(t *testing.T, apiKey, templateIDs string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, name := range []string{"environment", "email-provider", "email-from", "email-reply-to", "resend-api-key", "loops-api-key"} {
		require.NoError(t, (&cli.StringFlag{Name: name}).Apply(set))
	}
	require.NoError(t, (&cli.StringFlag{Name: "email-template-ids"}).Apply(set))
	require.NoError(t, set.Set("loops-api-key", apiKey))
	require.NoError(t, set.Set("email-template-ids", templateIDs))
	return cli.NewContext(cli.NewApp(), set, nil)
}

func TestNewEmailService_ResendConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, provider, environment, key, from, replyTo, wantError string }{
		{name: "unknown provider", provider: "other", wantError: "unsupported email provider"},
		{name: "local disabled", provider: "resend", environment: "local"},
		{name: "local placeholder disabled", provider: "resend", environment: "local", key: "unset"},
		{name: "deployed requires selected key", provider: "resend", environment: "production", wantError: "RESEND_API_KEY"},
		{name: "sender required", provider: "resend", environment: "local", key: "test-key", wantError: "email-from and email-reply-to"},
		{name: "reply-to required", provider: "resend", environment: "local", key: "test-key", from: "platform@notifications.dev.speakeasy.com", wantError: "email-from and email-reply-to"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newEmailCLIContext(t, "loops-key-is-not-a-resend-key", "")
			for name, value := range map[string]string{"email-provider": tc.provider, "environment": tc.environment, "resend-api-key": tc.key, "email-from": tc.from, "email-reply-to": tc.replyTo} {
				require.NoError(t, c.Set(name, value))
			}
			svc, err := newEmailService(t.Context(), c, testenv.NewLogger(t), nil)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
				require.NoError(t, svc.Send(t.Context(), "person@example.com", email.TeamInvite{}))
			}
		})
	}
}

func TestNewEmailService_ResendIgnoresLegacyIDs(t *testing.T) {
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	c := newEmailCLIContext(t, "", "not legacy JSON")
	for name, value := range map[string]string{
		"email-provider": "resend", "environment": "staging", "resend-api-key": "test-key",
		"email-from": "platform@notifications.dev.speakeasy.com", "email-reply-to": "platform@speakeasy.com",
	} {
		require.NoError(t, c.Set(name, value))
	}
	svc, err := newEmailService(t.Context(), c, testenv.NewLogger(t), policy)
	require.NoError(t, err)
	require.NotNil(t, svc)
	// Construction must not send email or require legacy template IDs.
}
