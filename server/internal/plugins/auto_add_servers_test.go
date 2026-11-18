package plugins_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// grantRoleConnect gives role Use access to every MCP server, the broadest
// grant automatic server adding reacts to.
func grantRoleConnect(t *testing.T, ctx context.Context, ti *testInstance, role string) {
	t.Helper()
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, "*").MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
}

// revokeRoleConnect removes the grant grantRoleConnect made and replays the
// role change the way the role editor does.
func revokeRoleConnect(t *testing.T, ctx context.Context, ti *testInstance, role string) {
	t.Helper()
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: Exercise the transactional role change hook.
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	before, err := roledelivery.Snapshot(ctx, tx, ac.ActiveOrganizationID, role)
	require.NoError(t, err)
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	_, err = accessrepo.New(tx).DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal})
	require.NoError(t, err)
	_, err = roledelivery.RoleChanged(ctx, tx, ac.ActiveOrganizationID, role, before, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

func pluginServerIDs(t *testing.T, ctx context.Context, ti *testInstance, pluginID string) []string {
	t.Helper()
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: pluginID})
	require.NoError(t, err)
	ids := make([]string, 0, len(got.Servers))
	for _, server := range got.Servers {
		ids = append(ids, *server.McpServerID)
	}
	return ids
}

// With automatic server adding off, no role delivery entrypoint changes a
// plugin's servers. The audience entrypoint comes first: assigning a role
// that can use every server to a hand-curated plugin must not fill it.
func TestAutoAddServersOff_NoEntrypointAddsServers(t *testing.T) {
	t.Parallel()
	for _, entrypoint := range []string{"audience", "role_grant", "eligible", "setup"} {
		t.Run(entrypoint, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			ac, _ := contextvalues.GetAuthContext(ctx)
			role := createTestRolePrincipal(t, ctx, ti, "manual")
			params, _ := platformDistributionServer(t, ctx, ti, false)
			slug := "manual"
			plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Manual", Slug: &slug})
			require.NoError(t, err)
			require.False(t, plugin.AutoAddServersEnabled, "automatic server adding is off by default")
			assign := func() {
				_, err := ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
				require.NoError(t, err)
			}
			if entrypoint != "audience" {
				assign()
			}
			grantRoleConnect(t, ctx, ti, role)
			switch entrypoint {
			case "audience":
				assign()
			case "setup":
				require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{}))
			case "role_grant", "eligible":
				tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: Exercise transactional distribution hooks.
				require.NoError(t, err)
				defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
				if entrypoint == "role_grant" {
					_, err = roledelivery.RoleChanged(ctx, tx, ac.ActiveOrganizationID, role, nil, nil)
				} else {
					_, err = plugins.AttachToDefaultAndRolePluginsAudited(ctx, tx, audit.NewLogger(), ac, params, nil)
				}
				require.NoError(t, err)
				require.NoError(t, tx.Commit(ctx))
			}
			require.Empty(t, pluginServerIDs(t, ctx, ti, plugin.ID))
		})
	}
}

// With automatic server adding off, losing a role audience or the role's
// access leaves servers an admin added by hand in place.
func TestAutoAddServersOff_NoEntrypointRemovesServers(t *testing.T) {
	t.Parallel()
	for _, entrypoint := range []string{"audience", "role_grant"} {
		t.Run(entrypoint, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			role := createTestRolePrincipal(t, ctx, ti, "manual")
			server := createTestToolset(t, ctx, ti.conn, "manual")
			plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Manual"})
			require.NoError(t, err)
			grantRoleConnect(t, ctx, ti, role)
			_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
			require.NoError(t, err)
			_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: new(server.ID.String()), DisplayName: new(server.Name), Policy: "required"})
			require.NoError(t, err)
			require.Equal(t, []string{server.ID.String()}, pluginServerIDs(t, ctx, ti, plugin.ID))
			switch entrypoint {
			case "audience":
				_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{}})
				require.NoError(t, err)
			case "role_grant":
				revokeRoleConnect(t, ctx, ti, role)
			}
			require.Equal(t, []string{server.ID.String()}, pluginServerIDs(t, ctx, ti, plugin.ID))
		})
	}
}

// Turning automatic server adding on fills the plugin from its current role
// audiences. Turning it back off keeps the servers, and later access changes
// leave them alone.
func TestAutoAddServers_TurningOnAddsRoleServersAndOffKeepsThem(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	role := createTestRolePrincipal(t, ctx, ti, "toggle")
	server := createTestToolset(t, ctx, ti.conn, "toggle")
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Toggle"})
	require.NoError(t, err)
	grantRoleConnect(t, ctx, ti, role)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	require.Empty(t, pluginServerIDs(t, ctx, ti, plugin.ID))

	updated, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: plugin.Name, Slug: plugin.Slug, AutoAddServersEnabled: new(true)})
	require.NoError(t, err)
	require.True(t, updated.AutoAddServersEnabled)
	require.Equal(t, []string{server.ID.String()}, pluginServerIDs(t, ctx, ti, plugin.ID))
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)
	beforeSnap, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnap, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, false, beforeSnap["auto_add_servers_enabled"])
	require.Equal(t, true, afterSnap["auto_add_servers_enabled"])

	// An unrelated edit keeps the setting.
	updated, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: "Renamed", Slug: plugin.Slug})
	require.NoError(t, err)
	require.True(t, updated.AutoAddServersEnabled)

	updated, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: updated.Name, Slug: plugin.Slug, AutoAddServersEnabled: new(false)})
	require.NoError(t, err)
	require.False(t, updated.AutoAddServersEnabled)
	require.Equal(t, []string{server.ID.String()}, pluginServerIDs(t, ctx, ti, plugin.ID), "turning it off keeps the current servers")
	revokeRoleConnect(t, ctx, ti, role)
	require.Equal(t, []string{server.ID.String()}, pluginServerIDs(t, ctx, ti, plugin.ID), "access changes no longer remove servers")
}

// Turning automatic server adding on does not bring back a server an admin
// removed before, the same rule role setup follows.
func TestAutoAddServers_TurningOnSkipsEarlierRemovals(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	role := createTestRolePrincipal(t, ctx, ti, "removed")
	server := createTestToolset(t, ctx, ti.conn, "removed")
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Removed"})
	require.NoError(t, err)
	grantRoleConnect(t, ctx, ti, role)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	added, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: new(server.ID.String()), DisplayName: new(server.Name), Policy: "required"})
	require.NoError(t, err)
	require.NoError(t, ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{PluginID: plugin.ID, ID: added.ID}))

	_, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: plugin.Name, Slug: plugin.Slug, AutoAddServersEnabled: new(true)})
	require.NoError(t, err)
	require.Empty(t, pluginServerIDs(t, ctx, ti, plugin.ID))
}

// Role setup creates its plugin with automatic server adding off, so the plugin
// starts empty even when the role can use servers.
func TestProcessRoleDistributionSetup_CreatesPluginWithAutoAddServersOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	role := createTestRolePrincipal(t, ctx, ti, "Fresh Role")
	_ = createTestToolset(t, ctx, ti.conn, "fresh")
	grantRoleConnect(t, ctx, ti, role)
	require.NoError(t, processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{}))
	list, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	var pluginID uuid.UUID
	for _, p := range list.Plugins {
		if p.Name == "Fresh Role" {
			require.False(t, p.AutoAddServersEnabled)
			pluginID = uuid.MustParse(p.ID)
		}
	}
	require.NotEqual(t, uuid.Nil, pluginID)
	require.Empty(t, pluginServerIDs(t, ctx, ti, pluginID.String()))
}
