package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/servernames"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServerIdentity_CanonicalWrapperOwnsToolsetIdentityBesideGatewayMember(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "server_identity_canonical")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID,
		Name: "Hosted", Slug: "hosted-" + uuid.NewString()[:8],
		McpSlug: conv.ToPGText("hosted-" + uuid.NewString()[:8]), McpEnabled: true,
	})
	require.NoError(t, err)
	servers := mcpserversrepo.New(conn)
	member, err := servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: project.ID, Name: conv.ToPGText("Gateway member"),
		Slug: conv.ToPGText("member-" + uuid.NewString()[:8]), ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility: "private",
	})
	require.NoError(t, err)
	canonical, err := servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: project.ID, Name: conv.ToPGText(toolset.Name), Slug: toolset.McpSlug,
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)

	plugins := pluginsrepo.New(conn)
	plugin, err := plugins.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID, Name: "Team", Slug: "team"})
	require.NoError(t, err)
	_, err = plugins.AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{PluginID: plugin.ID, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, DisplayName: "Hosted entry", Policy: "required"})
	require.NoError(t, err)
	_, err = plugins.AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{PluginID: plugin.ID, McpServerID: uuid.NullUUID{UUID: member.ID, Valid: true}, DisplayName: "Member entry", Policy: "required"})
	require.NoError(t, err)

	service := &DiagnosticsService{db: conn}
	configured, err := service.listConfiguredServers(ctx, principal.OrganizationID, project.ID.String())
	require.NoError(t, err)
	byID := map[string]servernames.ConfiguredServer{}
	for _, server := range configured {
		byID[server.ID] = server
	}
	require.Equal(t, toolset.Slug, byID[canonical.ID.String()].ToolsetSlug)
	require.Equal(t, []servernames.PluginMembership{{PluginSlug: "team", DisplayName: "Hosted entry"}}, byID[canonical.ID.String()].Plugins)
	require.Empty(t, byID[member.ID.String()].ToolsetSlug)
	require.Equal(t, []servernames.PluginMembership{{PluginSlug: "team", DisplayName: "Member entry"}}, byID[member.ID.String()].Plugins)

	resolver := servernames.NewResolver(configured)
	for reported, want := range map[string]uuid.UUID{toolset.Slug: canonical.ID, "Hosted entry": canonical.ID, "Member entry": member.ID} {
		got, ok := resolver.Resolve(reported)
		require.True(t, ok, reported)
		require.Equal(t, want.String(), got, reported)
	}

	canonicalIdentity, err := service.serverIdentity(ctx, principal.OrganizationID, project.ID.String(), canonical.ID.String())
	require.NoError(t, err)
	require.Equal(t, toolset.Slug, canonicalIdentity.toolsetSlug)
	require.Equal(t, []string{toolset.Slug}, canonicalIdentity.outcomeParams(project.ID.String(), 0, 1).ToolsetSlugs)
	require.Empty(t, canonicalIdentity.toolLogsTargets().hostedToolsetSlugs, "member calls carry the slug too")
	memberIdentity, err := service.serverIdentity(ctx, principal.OrganizationID, project.ID.String(), member.ID.String())
	require.NoError(t, err)
	require.Empty(t, memberIdentity.toolsetSlug)
}
