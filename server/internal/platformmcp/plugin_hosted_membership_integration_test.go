package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestGetPluginReadsToolsetKeyedEntryThroughCanonicalServer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_hosted_membership")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Hosted MCPs", "hosted-mcps")

	newToolset := func(name string) toolsetsrepo.Toolset {
		toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
			OrganizationID: principal.OrganizationID, ProjectID: project.ID, Name: name, Slug: name + "-" + uuid.NewString()[:8],
			McpSlug: conv.ToPGText(name + "-" + uuid.NewString()[:8]), McpEnabled: true,
		})
		require.NoError(t, err)
		return toolset
	}
	hosted := newToolset("hosted")
	_, err = mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: hosted.ID, ProjectID: project.ID, Name: conv.ToPGText(hosted.Name), Slug: hosted.McpSlug,
		ToolsetID: uuid.NullUUID{UUID: hosted.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: project.ID, McpServerID: uuid.NullUUID{UUID: hosted.ID, Valid: true}, Slug: hosted.McpSlug.String,
	})
	require.NoError(t, err)
	legacy := newToolset("legacy")
	for i, toolset := range []toolsetsrepo.Toolset{hosted, legacy} {
		_, err = pluginsrepo.New(conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
			PluginID: plugin.ID, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
			DisplayName: toolset.Name, Policy: "required", SortOrder: int32(i),
		})
		require.NoError(t, err)
	}

	got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Len(t, got.Servers, 2)
	require.Equal(t, "mcp_server", got.Servers[0].Backend, "a toolset-keyed entry reads as its canonical server")
	require.Equal(t, hosted.ID.String(), got.Servers[0].TargetID)
	require.Equal(t, hosted.McpSlug.String, got.Servers[0].MCPSlug)
	require.True(t, got.Servers[0].Enabled)
	require.Equal(t, "toolset", got.Servers[1].Backend, "a toolset with no canonical server keeps its toolset backend")
	require.Equal(t, legacy.ID.String(), got.Servers[1].TargetID)
	require.Equal(t, legacy.McpSlug.String, got.Servers[1].MCPSlug)

	settings, err := platformrepo.New(conn).GetPlatformMCPConnectionSettings(ctx, platformrepo.GetPlatformMCPConnectionSettingsParams{
		TargetKind: "mcp_server", TargetID: hosted.ID, OrganizationID: principal.OrganizationID, ProjectID: project.ID,
	})
	require.NoError(t, err)
	require.Contains(t, string(settings.PluginMemberships), plugin.ID.String(), "connection settings list the toolset-keyed entry")

	_, err = conn.Exec(ctx, "UPDATE mcp_servers SET visibility = 'disabled' WHERE id = $1 AND project_id = $2", hosted.ID, project.ID)
	require.NoError(t, err)
	got, err = service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Equal(t, "toolset", got.Servers[0].Backend, "a disabled canonical server falls back to the toolset, as generation does")
	require.Equal(t, hosted.McpSlug.String, got.Servers[0].MCPSlug)
	require.True(t, got.Servers[0].Enabled)
}
