package plugins_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestEligibleServerDeliversPriorRoleGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	role := createTestRolePrincipal(t, ctx, ti, "eligibility")
	server := createTestToolset(t, ctx, ti.conn, "Later enabled")
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPEnabledByID(ctx, toolsetsrepo.SetToolsetMCPEnabledByIDParams{ID: server.ID, ProjectID: *ac.ProjectID, McpEnabled: false}))
	//nolint:glint // notestingrawsql: mirrors hostedmcp.Sync disabling the canonical server.
	_, err := ti.conn.Exec(ctx, `UPDATE mcp_servers SET visibility = 'disabled' WHERE id = $1`, server.ID)
	require.NoError(t, err)
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, server.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Eligible delivery"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Empty(t, got.Servers)
	attach := func() {
		tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: Exercise the public attachment hook in its enclosing eligibility transaction.
		require.NoError(t, err)
		defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
		require.NoError(t, toolsetsrepo.New(tx).SetToolsetMCPEnabledByID(ctx, toolsetsrepo.SetToolsetMCPEnabledByIDParams{ID: server.ID, ProjectID: *ac.ProjectID, McpEnabled: true}))
		//nolint:glint // notestingrawsql: mirrors hostedmcp.Sync re-enabling the canonical server.
		_, err = tx.Exec(ctx, `UPDATE mcp_servers SET visibility = 'private' WHERE id = $1`, server.ID)
		require.NoError(t, err)
		_, err = plugins.AttachToDefaultAndRolePluginsAudited(ctx, tx, audit.NewLogger(), ac, plugins.AttachToDefaultPluginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: server.ID, Valid: true}, DisplayName: server.Name}, nil)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
	}
	attach()
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1, "eligibility delivers existing grants without reassigning the audience")
	require.Equal(t, server.ID.String(), *got.Servers[0].McpServerID, "MCP enable delivers the toolset's canonical server")
	require.NoError(t, ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{PluginID: plugin.ID, ID: got.Servers[0].ID}))
	attach()
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Empty(t, got.Servers, "eligibility replay respects administrator removal")
}

// enableToolsetAndAttach turns MCP on for a wrapped toolset the way toolset updates do, then runs the eligibility hook.
func enableToolsetAndAttach(t *testing.T, ctx context.Context, ti *testInstance, server toolsetsrepo.Toolset) {
	t.Helper()
	ac, _ := contextvalues.GetAuthContext(ctx)
	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: Exercise the public attachment hook in its enclosing eligibility transaction.
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	require.NoError(t, toolsetsrepo.New(tx).SetToolsetMCPEnabledByID(ctx, toolsetsrepo.SetToolsetMCPEnabledByIDParams{ID: server.ID, ProjectID: *ac.ProjectID, McpEnabled: true}))
	//nolint:glint // notestingrawsql: mirrors hostedmcp.Sync re-enabling the canonical server.
	_, err = tx.Exec(ctx, `UPDATE mcp_servers SET visibility = 'private' WHERE id = $1`, server.ID)
	require.NoError(t, err)
	_, err = plugins.AttachToDefaultAndRolePluginsAudited(ctx, tx, audit.NewLogger(), ac, plugins.AttachToDefaultPluginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: server.ID, Valid: true}, DisplayName: server.Name}, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

// A removal recorded under the old toolset key still keeps the canonical server out on MCP enable.
func TestEligibleServerRespectsToolsetKeyedRemoval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	role := createTestRolePrincipal(t, ctx, ti, "eligibility-tombstone")
	server := createTestToolset(t, ctx, ti.conn, "Removed before")
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPEnabledByID(ctx, toolsetsrepo.SetToolsetMCPEnabledByIDParams{ID: server.ID, ProjectID: *ac.ProjectID, McpEnabled: false}))
	//nolint:glint // notestingrawsql: mirrors hostedmcp.Sync disabling the canonical server.
	_, err := ti.conn.Exec(ctx, `UPDATE mcp_servers SET visibility = 'disabled' WHERE id = $1`, server.ID)
	require.NoError(t, err)
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, server.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Eligible tombstone"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	_, err = testrepo.New(ti.conn).InsertToolsetKeyedPluginServerFixture(ctx, testrepo.InsertToolsetKeyedPluginServerFixtureParams{
		PluginID: uuid.MustParse(plugin.ID), ToolsetID: uuid.NullUUID{UUID: server.ID, Valid: true}, DisplayName: server.Name, Policy: "required", SortOrder: 0,
		DeletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)

	enableToolsetAndAttach(t, ctx, ti, server)
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Empty(t, got.Servers, "a toolset-keyed removal is not re-added")
}
