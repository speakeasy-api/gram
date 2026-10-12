package plugins_test

import (
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEligibleServerDeliversPriorRoleGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	role := createTestRolePrincipal(t, ctx, ti, "eligibility")
	server := createTestToolset(t, ctx, ti.conn, "Later enabled")
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPEnabledByID(ctx, toolsetsrepo.SetToolsetMCPEnabledByIDParams{ID: server.ID, ProjectID: *ac.ProjectID, McpEnabled: false}))
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
		_, err = plugins.AttachToDefaultAndRolePluginsAudited(ctx, tx, audit.NewLogger(), ac, plugins.AttachToDefaultPluginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: server.ID, Valid: true}, DisplayName: server.Name}, nil)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
	}
	attach()
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, 1, "eligibility delivers existing grants without reassigning the audience")
	require.NoError(t, ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{PluginID: plugin.ID, ID: got.Servers[0].ID}))
	attach()
	got, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Empty(t, got.Servers, "eligibility replay respects administrator removal")
}
