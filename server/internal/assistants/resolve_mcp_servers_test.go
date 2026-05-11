package assistants

import (
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

func TestResolveAssistantMCPServers_EmptyUserToolsetsStillGetsPlatformServer(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	servers, err := resolveAssistantMCPServers(serverURL, nil)
	require.NoError(t, err)
	require.Len(t, servers, 1)

	require.Equal(t, "_platform-"+platformtools.AssistantsPlatformToolsetSlug, servers[0].ID)
	require.Equal(t,
		"https://gram.test/x/platform-mcp/"+platformtools.AssistantsPlatformToolsetSlug,
		servers[0].URL,
	)
	require.Empty(t, servers[0].Headers)
}

func TestResolveAssistantMCPServers_UserToolsetsListedBeforePlatformServer(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	rows := []assistantToolsetRow{
		{
			ToolsetSlug:     "billing",
			McpEnabled:      true,
			McpSlug:         pgtype.Text{String: "billing-mcp", Valid: true},
			EnvironmentSlug: pgtype.Text{String: "prod", Valid: true},
		},
	}

	servers, err := resolveAssistantMCPServers(serverURL, rows)
	require.NoError(t, err)
	require.Len(t, servers, 2)

	require.Equal(t, "billing", servers[0].ID)
	require.Equal(t, "https://gram.test/mcp/billing-mcp", servers[0].URL)
	require.Equal(t, "prod", servers[0].Headers["Gram-Environment"])

	require.Equal(t, "_platform-"+platformtools.AssistantsPlatformToolsetSlug, servers[1].ID)
	require.Equal(t,
		"https://gram.test/x/platform-mcp/"+platformtools.AssistantsPlatformToolsetSlug,
		servers[1].URL,
	)
}
