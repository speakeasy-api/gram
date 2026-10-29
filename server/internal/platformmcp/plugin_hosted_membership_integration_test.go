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
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestGetPluginReadsHostedEntryThroughCanonicalServer(t *testing.T) {
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
		})
		require.NoError(t, err)
		return toolset
	}
	hosted := newToolset("hosted")
	hostedSlug := "hosted-" + uuid.NewString()[:8]
	_, err = mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: hosted.ID, ProjectID: project.ID, Name: conv.ToPGText(hosted.Name), Slug: conv.ToPGText(hostedSlug),
		ToolsetID: uuid.NullUUID{UUID: hosted.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: project.ID, McpServerID: uuid.NullUUID{UUID: hosted.ID, Valid: true}, Slug: hostedSlug,
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID: plugin.ID, McpServerID: uuid.NullUUID{UUID: hosted.ID, Valid: true},
		DisplayName: hosted.Name, Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)

	got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1)
	require.Equal(t, "mcp_server", got.Servers[0].Backend)
	require.Equal(t, hosted.ID.String(), got.Servers[0].TargetID)
	require.Equal(t, hostedSlug, got.Servers[0].MCPSlug)
	require.True(t, got.Servers[0].Enabled)

	settings, err := platformrepo.New(conn).GetPlatformMCPConnectionSettings(ctx, platformrepo.GetPlatformMCPConnectionSettingsParams{
		TargetKind: "mcp_server", TargetID: hosted.ID, OrganizationID: principal.OrganizationID, ProjectID: project.ID,
	})
	require.NoError(t, err)
	require.Contains(t, string(settings.PluginMemberships), plugin.ID.String(), "connection settings list the hosted entry")

	err = testrepo.New(conn).SetMCPServerVisibilityFixture(ctx, testrepo.SetMCPServerVisibilityFixtureParams{Visibility: "disabled", ID: hosted.ID, ProjectID: project.ID})
	require.NoError(t, err)
	got, err = service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Equal(t, "mcp_server", got.Servers[0].Backend)
	require.Equal(t, hostedSlug, got.Servers[0].MCPSlug)
	require.False(t, got.Servers[0].Enabled, "a disabled canonical server reads as disabled")
}

// An endpointless canonical wrapper reports its toolset legacy alias; a fresh-id server does not.
func TestGetPluginEndpointlessWrapperKeepsToolsetSlug(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_endpointless_wrapper")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Endpointless", "endpointless")

	newToolset := func(name string) toolsetsrepo.Toolset {
		toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
			OrganizationID: principal.OrganizationID, ProjectID: project.ID, Name: name, Slug: name + "-" + uuid.NewString()[:8],
		})
		require.NoError(t, err)
		return toolset
	}
	newServer := func(id uuid.UUID, toolset toolsetsrepo.Toolset) {
		_, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
			ID: id, ProjectID: project.ID, Name: conv.ToPGText(toolset.Name), Slug: conv.ToPGText(toolset.Slug + "-server"),
			ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private",
		})
		require.NoError(t, err)
	}
	serverKeyed := newToolset("server-keyed")
	serverKeyedAlias := "server-keyed-" + uuid.NewString()[:8]
	testenv.SetLegacyAlias(t, ctx, conn, project.ID, serverKeyed.ID, serverKeyedAlias, uuid.NullUUID{})
	newServer(serverKeyed.ID, serverKeyed)
	freshBacking := newToolset("fresh-backing")
	testenv.SetLegacyAlias(t, ctx, conn, project.ID, freshBacking.ID, "fresh-backing-"+uuid.NewString()[:8], uuid.NullUUID{})
	freshID := uuid.New()
	newServer(freshID, freshBacking)

	for i, entry := range []pluginsrepo.AddPluginServerParams{
		{McpServerID: uuid.NullUUID{UUID: serverKeyed.ID, Valid: true}, DisplayName: "b"},
		{McpServerID: uuid.NullUUID{UUID: freshID, Valid: true}, DisplayName: "c"},
	} {
		entry.PluginID, entry.Policy, entry.SortOrder = plugin.ID, "required", int32(i)
		_, err = pluginsrepo.New(conn).AddPluginServer(ctx, entry)
		require.NoError(t, err)
	}

	got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Len(t, got.Servers, 2)
	require.Equal(t, serverKeyedAlias, got.Servers[0].MCPSlug)
	require.Empty(t, got.Servers[1].MCPSlug)
	for _, server := range got.Servers {
		require.Equal(t, "mcp_server", server.Backend)
	}

	// The member-facing projection resolves the same fallback.
	member, err := platformrepo.New(conn).ListPlatformMCPPluginServers(ctx, platformrepo.ListPlatformMCPPluginServersParams{
		PluginID: plugin.ID, ProjectID: project.ID, OrganizationID: principal.OrganizationID, ResultLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, member, 2)
	require.Equal(t, uuid.NullUUID{UUID: serverKeyed.ID, Valid: true}, member[0].AliasToolsetID)
	require.False(t, member[1].AliasToolsetID.Valid)
	aliases, err := pluginMemberAliases(ctx, conn, project.ID, member, func(row platformrepo.ListPlatformMCPPluginServersRow) (string, uuid.NullUUID) {
		return row.McpSlug, row.AliasToolsetID
	})
	require.NoError(t, err)
	require.Equal(t, serverKeyedAlias, memberSlug(member[0].McpSlug, member[0].AliasToolsetID, aliases))
	require.Empty(t, memberSlug(member[1].McpSlug, member[1].AliasToolsetID, aliases))
}
