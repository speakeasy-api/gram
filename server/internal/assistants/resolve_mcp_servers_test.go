package assistants

import (
	"context"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestResolveAssistantMCPServers_EmptyUserToolsetsStillGetsPlatformServer(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, nil)
	require.Len(t, servers, 1)

	require.Equal(t, "_platform-"+platformtools.AssistantsPlatformToolsetSlug, servers[0].ID)
	require.Equal(t,
		"https://gram.test/platform/mcp/"+platformtools.AssistantsPlatformToolsetSlug,
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

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, rows)
	require.Len(t, servers, 2)

	require.Equal(t, "billing", servers[0].ID)
	require.Equal(t, "https://gram.test/mcp/billing-mcp", servers[0].URL)
	require.Equal(t, "prod", servers[0].Headers["Gram-Environment"])

	require.Equal(t, "_platform-"+platformtools.AssistantsPlatformToolsetSlug, servers[1].ID)
	require.Equal(t,
		"https://gram.test/platform/mcp/"+platformtools.AssistantsPlatformToolsetSlug,
		servers[1].URL,
	)
}

// A toolset that is attached to an assistant but whose MCP is disabled or
// has no mcp_slug used to abort the entire bootstrap with a silent 500.
// We now skip the broken toolset so the rest of the thread admits — the
// assistant just won't see those tools.
func TestResolveAssistantMCPServers_MisconfiguredToolsetIsOmitted(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	rows := []assistantToolsetRow{
		{
			ToolsetSlug: "no-mcp-slug",
			McpEnabled:  true,
			McpSlug:     pgtype.Text{Valid: false},
		},
		{
			ToolsetSlug: "mcp-disabled",
			McpEnabled:  false,
			McpSlug:     pgtype.Text{String: "mcp-disabled-mcp", Valid: true},
		},
		{
			ToolsetSlug: "billing",
			McpEnabled:  true,
			McpSlug:     pgtype.Text{String: "billing-mcp", Valid: true},
		},
	}

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, rows)
	require.Len(t, servers, 2)

	require.Equal(t, "billing", servers[0].ID)
	require.Equal(t, "https://gram.test/mcp/billing-mcp", servers[0].URL)

	require.Equal(t, "_platform-"+platformtools.AssistantsPlatformToolsetSlug, servers[1].ID)
}
