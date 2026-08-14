package gram

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/email"
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
	require.NoError(t, (&cli.StringFlag{Name: "loops-api-key"}).Apply(set))
	require.NoError(t, (&cli.StringFlag{Name: "email-template-ids"}).Apply(set))
	require.NoError(t, set.Set("loops-api-key", apiKey))
	require.NoError(t, set.Set("email-template-ids", templateIDs))
	return cli.NewContext(cli.NewApp(), set, nil)
}
