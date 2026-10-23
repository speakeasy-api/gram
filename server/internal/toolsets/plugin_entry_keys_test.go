package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func updateToolsetPayload(slug types.Slug) *gen.UpdateToolsetPayload {
	return &gen.UpdateToolsetPayload{
		SessionToken: nil, Slug: slug, Name: nil, Description: nil, DefaultEnvironmentSlug: nil, ToolUrns: nil,
		ResourceUrns: nil, PromptTemplateNames: nil, McpSlug: nil, McpIsPublic: nil, McpEnabled: nil,
		CustomDomainID: nil, ProjectSlugInput: nil,
	}
}

func TestToolsetsService_PluginEntriesFollowRenameAndDelete(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugins := pluginsrepo.New(ti.conn)
	defaultPlugin, err := plugins.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken: nil, Name: "Hosted Toolset", Description: nil, ToolUrns: []string{}, ResourceUrns: nil,
		DefaultEnvironmentSlug: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.True(t, *created.McpEnabled)
	toolsetID := uuid.MustParse(created.ID)

	// A legacy toolset-keyed entry in another plugin, as left before the dependent move.
	other, err := plugins.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, Name: "Other", Slug: "other-" + uuid.NewString()[:8], Description: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	_, err = testrepo.New(ti.conn).InsertToolsetKeyedPluginServerFixture(ctx, testrepo.InsertToolsetKeyedPluginServerFixtureParams{
		PluginID: other.ID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true},
		DisplayName: "Hosted Toolset", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)

	bystander, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken: nil, Name: "Bystander", Description: nil, ToolUrns: []string{}, ResourceUrns: nil,
		DefaultEnvironmentSlug: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	kept, err := plugins.AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID: other.ID, McpServerID: uuid.NullUUID{UUID: uuid.MustParse(bystander.ID), Valid: true},
		DisplayName: "Bystander", Policy: "required", SortOrder: 1,
	})
	require.NoError(t, err)

	rename := updateToolsetPayload(created.Slug)
	rename.Name = new("Renamed Toolset")
	_, err = ti.service.UpdateToolset(ctx, rename)
	require.NoError(t, err)

	for _, pluginID := range []uuid.UUID{defaultPlugin.ID, other.ID} {
		servers, err := plugins.ListPluginServers(ctx, pluginID)
		require.NoError(t, err)
		require.Contains(t, displayNames(servers), "Renamed Toolset", "a toolset rename reaches its entries")
		if pluginID == other.ID {
			require.Contains(t, displayNames(servers), "Bystander", "renaming one toolset leaves other server names unchanged")
		}
	}

	beforeRemovals, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerRemove)
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: created.Slug}))

	defaultServers, err := plugins.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.NotContains(t, displayNames(defaultServers), "Renamed Toolset", "deleting the toolset detaches its entries")
	otherServers, err := plugins.ListPluginServers(ctx, other.ID)
	require.NoError(t, err)
	require.Len(t, otherServers, 1, "another server's entry stays")
	require.Equal(t, kept.ID, otherServers[0].ID)
	afterRemovals, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerRemove)
	require.NoError(t, err)
	require.Equal(t, beforeRemovals+2, afterRemovals)
}

func TestToolsetsService_ReenableDoesNotTwinLegacyEntry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugins := pluginsrepo.New(ti.conn)
	defaultPlugin, err := plugins.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken: nil, Name: "Legacy Entry", Description: nil, ToolUrns: []string{}, ResourceUrns: nil,
		DefaultEnvironmentSlug: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	toolsetID := uuid.MustParse(created.ID)

	disable := updateToolsetPayload(created.Slug)
	disable.McpEnabled = new(false)
	_, err = ti.service.UpdateToolset(ctx, disable)
	require.NoError(t, err)
	require.NoError(t, plugins.SoftDeletePluginServers(ctx, defaultPlugin.ID))
	_, err = testrepo.New(ti.conn).InsertToolsetKeyedPluginServerFixture(ctx, testrepo.InsertToolsetKeyedPluginServerFixtureParams{
		PluginID: defaultPlugin.ID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true},
		DisplayName: "Legacy Entry", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)

	enable := updateToolsetPayload(created.Slug)
	enable.McpEnabled = new(true)
	_, err = ti.service.UpdateToolset(ctx, enable)
	require.NoError(t, err)

	servers, err := plugins.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1, "the toolset-keyed entry already covers the canonical server")
	require.Equal(t, toolsetID, servers[0].ToolsetID.UUID)
}

func displayNames(servers []pluginsrepo.PluginServer) []string {
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.DisplayName)
	}
	return names
}
