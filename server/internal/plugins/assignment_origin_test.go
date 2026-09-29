package plugins_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestAssignmentOriginRejectsDeletedProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Origin guards"})
	require.NoError(t, err)
	q := pluginsrepo.New(ti.conn)
	add := pluginsrepo.AddPluginAssignmentOriginParams{PluginID: uuid.MustParse(plugin.ID), ProjectID: *ac.ProjectID, OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: "role:organization:" + uuid.NewString()}
	remove := pluginsrepo.RemovePluginAssignmentOriginParams{PluginID: add.PluginID, ProjectID: add.ProjectID, OrganizationID: add.OrganizationID, PrincipalUrn: add.PrincipalUrn}
	rows, err := q.AddPluginAssignmentOrigin(ctx, add)
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	rows, err = q.RemovePluginAssignmentOrigin(ctx, remove)
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	rows, err = q.AddPluginAssignmentOrigin(ctx, add)
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	_, err = ti.conn.Exec(ctx, `UPDATE projects SET deleted_at = clock_timestamp() WHERE id = $1`, add.ProjectID) //nolint:glint // notestingrawsql: isolated soft-deletion fixture retains the plugin and its assignments
	require.NoError(t, err)
	rows, err = q.RemovePluginAssignmentOrigin(ctx, remove)
	require.NoError(t, err)
	require.Zero(t, rows)
	add.PrincipalUrn = "role:organization:" + uuid.NewString()
	rows, err = q.AddPluginAssignmentOrigin(ctx, add)
	require.NoError(t, err)
	require.Zero(t, rows)
}

func TestReplaceAssignmentsHintsOnlyMissingOrigin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Origin hints"})
	require.NoError(t, err)
	origin := createTestRolePrincipal(t, ctx, ti, "origin")
	other := createTestRolePrincipal(t, ctx, ti, "other")
	settingID := uuid.New()
	_, err = ti.conn.Exec(ctx, `INSERT INTO role_provisioning_settings (id, organization_id, role_urn, project_id) VALUES ($1, $2, $3, $4)`, settingID, ac.ActiveOrganizationID, origin, *ac.ProjectID) //nolint:glint // notestingrawsql: isolated managed-plugin fixture
	require.NoError(t, err)
	_, err = ti.conn.Exec(ctx, `INSERT INTO role_plugin_associations (role_provisioning_setting_id, project_id, plugin_id, is_current) VALUES ($1, $2, $3, true)`, settingID, *ac.ProjectID, uuid.MustParse(plugin.ID)) //nolint:glint // notestingrawsql: isolated managed-plugin fixture
	require.NoError(t, err)
	countHints := func() int {
		var count int
		err := ti.conn.QueryRow(ctx, `SELECT count(*) FROM publish_outbox WHERE organization_id = $1 AND topic = 'gram.plugins.v1.RoleProvisioningRequested'`, ac.ActiveOrganizationID).Scan(&count) //nolint:glint // notestingrawsql: assert transactional maintenance hint emission
		require.NoError(t, err)
		return count
	}
	before := countHints()
	for _, desired := range [][]string{{origin}, {origin}, {origin, other}, {origin}} {
		_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: desired})
		require.NoError(t, err)
		require.Equal(t, before, countHints(), "retained origin must not enqueue maintenance")
	}
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{other}})
	require.NoError(t, err)
	require.Equal(t, before+1, countHints())
}
