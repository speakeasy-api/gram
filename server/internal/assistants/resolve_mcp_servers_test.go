package assistants

import (
	"context"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestResolveAssistantMCPServers_EmptyUserToolsetsStillGetsPlatformServer(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, nil, nil, []string{platformtools.AssistantsPlatformToolsetSlug})
	require.Len(t, servers, 1)

	require.Equal(t, "_p-"+platformtools.AssistantsPlatformToolsetSlug, servers[0].ID)
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

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, rows, nil, []string{platformtools.AssistantsPlatformToolsetSlug})
	require.Len(t, servers, 2)

	require.Equal(t, "billing", servers[0].ID)
	require.Equal(t, "https://gram.test/mcp/billing-mcp", servers[0].URL)
	require.Equal(t, "prod", servers[0].Headers["Gram-Environment"])

	require.Equal(t, "_p-"+platformtools.AssistantsPlatformToolsetSlug, servers[1].ID)
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

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, rows, nil, []string{platformtools.AssistantsPlatformToolsetSlug})
	require.Len(t, servers, 2)

	require.Equal(t, "billing", servers[0].ID)
	require.Equal(t, "https://gram.test/mcp/billing-mcp", servers[0].URL)

	require.Equal(t, "_p-"+platformtools.AssistantsPlatformToolsetSlug, servers[1].ID)
}

// The managed assistant is granted the managed-assistant platform toolset (the
// dashboard egress tool) in addition to the always-on assistants toolset; a
// non-managed assistant is given only the assistants slug, so it never sees a
// managed-assistant server.
func TestResolveAssistantMCPServers_GrantsEachRequestedPlatformToolset(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, nil, nil, []string{
		platformtools.AssistantsPlatformToolsetSlug,
		platformtools.ManagedAssistantPlatformToolsetSlug,
	})
	require.Len(t, servers, 2)
	require.Equal(t, "_p-"+platformtools.AssistantsPlatformToolsetSlug, servers[0].ID)
	require.Equal(t, "_p-"+platformtools.ManagedAssistantPlatformToolsetSlug, servers[1].ID)
	require.Equal(t,
		"https://gram.test/platform/mcp/"+platformtools.ManagedAssistantPlatformToolsetSlug,
		servers[1].URL,
	)
}

// A directly-attached mcp_server is exposed to
// the runner as its public /mcp/{endpoint} URL, identified by the server slug,
// carrying the bound environment. Ordering is toolsets, then mcp servers, then
// platform servers.
func TestResolveAssistantMCPServers_AttachedMCPServerAfterToolsetsBeforePlatform(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	toolsets := []assistantToolsetRow{
		{
			ToolsetSlug: "billing",
			McpEnabled:  true,
			McpSlug:     pgtype.Text{String: "billing-mcp", Valid: true},
		},
	}
	mcpServers := []assistantMCPServerRow{
		{
			ServerSlug:      pgtype.Text{String: "remote-saas-a1b2", Valid: true},
			EndpointSlug:    "team-remote-saas",
			EnvironmentSlug: pgtype.Text{String: "prod", Valid: true},
		},
	}

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, toolsets, mcpServers, []string{platformtools.AssistantsPlatformToolsetSlug})
	require.Len(t, servers, 3)

	require.Equal(t, "billing", servers[0].ID)

	require.Equal(t, "remote-saas-a1b2", servers[1].ID)
	require.Equal(t, "https://gram.test/mcp/team-remote-saas", servers[1].URL)
	require.Equal(t, "prod", servers[1].Headers["Gram-Environment"])

	require.Equal(t, "_p-"+platformtools.AssistantsPlatformToolsetSlug, servers[2].ID)
}

// With no bound environment the entry carries no Gram-Environment header, and a
// server whose slug is unset falls back to the endpoint slug as the runtime ID.
func TestResolveAssistantMCPServers_AttachedMCPServerDefaults(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	mcpServers := []assistantMCPServerRow{
		{
			ServerSlug:   pgtype.Text{Valid: false},
			EndpointSlug: "team-remote-saas",
		},
	}

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, nil, mcpServers, nil)
	require.Len(t, servers, 1)
	require.Equal(t, "team-remote-saas", servers[0].ID)
	require.Equal(t, "https://gram.test/mcp/team-remote-saas", servers[0].URL)
	require.Empty(t, servers[0].Headers)
}

// Defensive: a row that reached the resolver without a Gram-hosted endpoint
// (loadAssistantMcpServers already filters these) is skipped rather than
// producing a slugless /mcp/ URL.
func TestResolveAssistantMCPServers_AttachedMCPServerWithoutEndpointOmitted(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	mcpServers := []assistantMCPServerRow{
		{
			ServerSlug:   pgtype.Text{String: "no-endpoint", Valid: true},
			EndpointSlug: "",
		},
	}

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, nil, mcpServers, nil)
	require.Empty(t, servers)
}

// A server disabled after attach 404s at the /mcp serving path, so the runtime
// skips it; the attachment itself stays visible on API reads.
func TestResolveAssistantMCPServers_DisabledMCPServerOmitted(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	mcpServers := []assistantMCPServerRow{
		{
			ServerSlug:   pgtype.Text{String: "switched-off", Valid: true},
			Visibility:   mcpservers.VisibilityDisabled,
			EndpointSlug: "switched-off-endpoint",
		},
		{
			ServerSlug:   pgtype.Text{String: "remote-saas", Valid: true},
			Visibility:   mcpservers.VisibilityPublic,
			EndpointSlug: "team-remote-saas",
		},
	}

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, nil, mcpServers, nil)
	require.Len(t, servers, 1)
	require.Equal(t, "remote-saas", servers[0].ID)
}

// Toolset slugs and mcp_servers slugs are separate slug spaces, so a direct
// attachment can collide with an attached toolset's runtime ID. The toolset
// wins and the colliding server is skipped so the runner never sees duplicate
// MCP server IDs.
func TestResolveAssistantMCPServers_CollidingServerSlugOmitted(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	toolsets := []assistantToolsetRow{
		{
			ToolsetSlug: "billing",
			McpEnabled:  true,
			McpSlug:     pgtype.Text{String: "billing-mcp", Valid: true},
		},
	}
	mcpServers := []assistantMCPServerRow{
		{
			ServerSlug:   pgtype.Text{String: "billing", Valid: true},
			EndpointSlug: "billing-remote",
		},
	}

	servers := resolveAssistantMCPServers(context.Background(), testenv.NewLogger(t), serverURL, toolsets, mcpServers, nil)
	require.Len(t, servers, 1)
	require.Equal(t, "billing", servers[0].ID)
	require.Equal(t, "https://gram.test/mcp/billing-mcp", servers[0].URL)
}

// mcp_servers slugs have no length cap, but agentkit tool names
// (mcp_<server_id>_<tool>) are rejected by providers past 64 characters, so
// overlong slugs are capped to a deterministic truncated ID with a hash
// suffix.
func TestResolveAssistantMCPServers_OverlongServerSlugCapped(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)

	mcpServers := []assistantMCPServerRow{
		{
			ServerSlug: pgtype.Text{
				String: "a-very-long-mcp-server-slug-created-from-a-long-display-name",
				Valid:  true,
			},
			EndpointSlug: "long-remote",
		},
	}

	servers := resolveAssistantMCPServers(t.Context(), testenv.NewLogger(t), serverURL, nil, mcpServers, nil)
	require.Len(t, servers, 1)
	require.Equal(t, "a-very-long-mcp-aa65ccda", servers[0].ID)
	require.Equal(t, "https://gram.test/mcp/long-remote", servers[0].URL)
}
