package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestListPluginsPagesAProjectsPluginsWithMembershipCounts(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_inventory")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)

	defaultPlugin, err := pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	marketing := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Marketing Tools", "marketing")

	_, err = pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		PluginID:       marketing.ID,
		OrganizationID: principal.OrganizationID,
		PrincipalUrn:   urn.PrincipalWildcard,
	})
	require.NoError(t, err)

	// The whole project fits in one page, and every plugin in it is reported.
	page, err := service.ListPlugins(ctx, principal, ListPluginsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	require.Empty(t, page.NextCursor)
	require.Len(t, page.Plugins, 2)

	byID := map[string]Plugin{}
	for _, plugin := range page.Plugins {
		byID[plugin.ID] = plugin
	}
	require.True(t, byID[defaultPlugin.ID.String()].IsDefault)
	require.False(t, byID[marketing.ID.String()].IsDefault)
	require.True(t, byID[marketing.ID.String()].Audience.AllMembers)
	require.Zero(t, byID[marketing.ID.String()].Audience.Users)
	// The project has no package repository connected, so nothing in it can be
	// published — reported as its own state rather than as "unpublished".
	require.Equal(t, PluginPublicationNoRepository, byID[marketing.ID.String()].Publication)

	// A one-per-page walk covers the same plugins exactly once.
	seen := map[string]bool{}
	cursor := ""
	for range 3 {
		step, err := service.ListPlugins(ctx, principal, ListPluginsInput{ProjectID: project.ID.String(), Limit: 1, Cursor: cursor})
		require.NoError(t, err)
		for _, plugin := range step.Plugins {
			require.False(t, seen[plugin.ID], "plugin returned twice")
			seen[plugin.ID] = true
		}
		cursor = step.NextCursor
		if cursor == "" {
			break
		}
	}
	require.Len(t, seen, 2)
}

func TestGetPluginResolvesAnExactTargetAndReportsMembership(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_membership")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	marketing := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Marketing Tools", "marketing")

	// Named by slug, by exact name, and by id: one plugin, three ways to say it.
	for _, target := range []string{"marketing", "Marketing Tools", "MARKETING TOOLS", marketing.ID.String()} {
		got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: target})
		require.NoError(t, err, target)
		require.Equal(t, marketing.ID.String(), got.Plugin.ID)
		require.Empty(t, got.Servers)
		require.Empty(t, got.Skills)
		require.False(t, got.Truncated)
	}
}

func TestGetPluginRefusesAnUnmatchedTargetRatherThanFallingBackToDefault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_refusal")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	_, err = pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)

	_, err = service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "marketing"})
	require.ErrorIs(t, err, ErrPluginNotFound)

	// Two plugins sharing a name is ambiguous, never resolved by picking one.
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared", "shared-one")
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared", "shared-two")
	_, err = service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: "Shared"})
	require.ErrorIs(t, err, ErrPluginAmbiguous)
}

func TestPluginInventoryRefusesAnotherOrganizationsProject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_tenancy")
	require.NoError(t, err)

	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	_, otherProject := seedRegistrationLifecycle(t, ctx, conn)
	service := testPluginTargets(conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, otherProject.ID, "Foreign", "foreign")

	_, err = service.ListPlugins(ctx, principal, ListPluginsInput{ProjectID: otherProject.ID.String()})
	require.ErrorIs(t, err, ErrPluginProjectNotFound)

	_, err = service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: otherProject.ID.String(), Plugin: "foreign"})
	require.ErrorIs(t, err, ErrPluginProjectNotFound)
}

func seedPlugin(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, name, slug string) pluginsrepo.Plugin {
	t.Helper()

	plugin, err := pluginsrepo.New(conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           name,
		Slug:           slug,
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)
	return plugin
}
