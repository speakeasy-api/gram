package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// A live toolset-keyed row left from before the server key reads as unresolved instead of failing the plugin read.
func TestGetPluginReportsLeftoverToolsetKeyedEntryAsUnresolved(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_unresolved_membership")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Leftovers", "leftovers")

	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, Name: "legacy", Slug: "legacy-" + uuid.NewString()[:8],
		McpSlug: conv.ToPGText("legacy-" + uuid.NewString()[:8]), McpEnabled: true,
	})
	require.NoError(t, err)
	_, err = testrepo.New(conn).InsertToolsetKeyedPluginServerFixture(ctx, testrepo.InsertToolsetKeyedPluginServerFixtureParams{
		PluginID: plugin.ID, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, DisplayName: "Legacy", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)

	got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	require.Equal(t, "unresolved", got.Servers[0].Backend)
	require.Empty(t, got.Servers[0].TargetID)

	member, err := platformrepo.New(conn).ListPlatformMCPPluginServers(ctx, platformrepo.ListPlatformMCPPluginServersParams{
		PluginID: plugin.ID, ProjectID: project.ID, OrganizationID: principal.OrganizationID, ResultLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, member, 1)
	require.True(t, member[0].Unresolved)
	require.False(t, member[0].Enabled)
}
