package plugins_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestRemoveDeletedRolePluginAssignmentRequiresExactScope(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	role := createTestRolePrincipal(t, ctx, ti, "deleted-role")
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Scoped cleanup"})
	require.NoError(t, err)
	pluginID := uuid.MustParse(plugin.ID)
	q := pluginsrepo.New(ti.conn)
	for _, principal := range []string{role, "*"} {
		_, err := q.AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
			PluginID: pluginID, OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal,
		})
		require.NoError(t, err)
	}
	listParams := pluginsrepo.ListPluginAssignmentsParams{
		PluginID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID,
	}
	before, err := q.ListPluginAssignments(ctx, listParams)
	require.NoError(t, err)
	exact := pluginsrepo.RemoveDeletedRolePluginAssignmentParams{
		PluginID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, PrincipalUrn: role,
	}
	wrongProject, wrongOrg, wrongPlugin, wrongRole := exact, exact, exact, exact
	wrongProject.ProjectID = uuid.New()
	wrongOrg.OrganizationID = "org_other_cleanup"
	wrongPlugin.PluginID = uuid.New()
	wrongRole.PrincipalUrn = "role:organization:" + uuid.NewString()
	for _, mismatched := range []pluginsrepo.RemoveDeletedRolePluginAssignmentParams{wrongProject, wrongOrg, wrongPlugin, wrongRole} {
		removed, err := q.RemoveDeletedRolePluginAssignment(ctx, mismatched)
		require.NoError(t, err)
		require.Zero(t, removed)
		after, err := q.ListPluginAssignments(ctx, listParams)
		require.NoError(t, err)
		require.ElementsMatch(t, before, after)
	}
	removed, err := q.RemoveDeletedRolePluginAssignment(ctx, exact)
	require.NoError(t, err)
	require.EqualValues(t, 1, removed)
	remaining, err := q.ListPluginAssignments(ctx, listParams)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, "*", remaining[0].PrincipalUrn)
	removed, err = q.RemoveDeletedRolePluginAssignment(ctx, exact)
	require.NoError(t, err)
	require.Zero(t, removed)
}
