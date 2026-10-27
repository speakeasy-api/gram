package plugins_test

import (
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
	require.NoError(t, testrepo.New(ti.conn).SetToolsetMCPEnabledByID(ctx, testrepo.SetToolsetMCPEnabledByIDParams{ID: server.ID, ProjectID: *ac.ProjectID, McpEnabled: false}))
	require.NoError(t, testrepo.New(ti.conn).SetMCPServerVisibilityFixture(ctx, testrepo.SetMCPServerVisibilityFixtureParams{Visibility: visibility.Disabled, ID: server.ID, ProjectID: *ac.ProjectID}))
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
		require.NoError(t, testrepo.New(tx).SetToolsetMCPEnabledByID(ctx, testrepo.SetToolsetMCPEnabledByIDParams{ID: server.ID, ProjectID: *ac.ProjectID, McpEnabled: true}))
		require.NoError(t, testrepo.New(tx).SetMCPServerVisibilityFixture(ctx, testrepo.SetMCPServerVisibilityFixtureParams{Visibility: visibility.Private, ID: server.ID, ProjectID: *ac.ProjectID}))
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

// A live wrapper whose backing toolset is deleted is never delivered, canonical or not.
func TestRoleDeliverySkipsWrapperOnDeletedToolset(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		canonical bool
		deleted   bool
	}{
		{name: "canonical on deleted toolset", canonical: true, deleted: true},
		{name: "member on deleted toolset", deleted: true},
		{name: "member on live toolset"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			ac, _ := contextvalues.GetAuthContext(ctx)
			role := createTestRolePrincipal(t, ctx, ti, "deleted-backing")
			toolset := createUnwrappedTestToolset(t, ctx, ti.conn, "Deleted backing")
			wrapperID := uuid.New()
			if tt.canonical {
				wrapperID = toolset.ID
			}
			wrapper, err := testrepo.New(ti.conn).CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{ID: wrapperID, ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private"})
			require.NoError(t, err)
			_, err = endpointrepo.New(ti.conn).CreateMCPEndpoint(ctx, endpointrepo.CreateMCPEndpointParams{ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: wrapper, Valid: true}, Slug: "endpoint-" + wrapper.String()[:8]})
			require.NoError(t, err)
			principal, err := urn.ParsePrincipal(role)
			require.NoError(t, err)
			selectors, err := authz.NewSelector(authz.ScopeMCPConnect, toolset.ID.String()).MarshalJSON()
			require.NoError(t, err)
			_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
			require.NoError(t, err)
			if tt.deleted {
				_, err = toolsetsrepo.New(ti.conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: toolset.Slug, ProjectID: *ac.ProjectID})
				require.NoError(t, err)
			}
			plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Deleted backing"})
			require.NoError(t, err)
			_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
			require.NoError(t, err)

			tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: Drive the role delivery entry points in their enclosing transaction.
			require.NoError(t, err)
			defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
			eligible, err := roledelivery.Eligible(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, wrapper, nil)
			require.NoError(t, err)
			populated, err := roledelivery.Populate(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID, uuid.MustParse(plugin.ID), role, nil)
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			require.False(t, eligible, "Eligible adds nothing")
			require.False(t, populated, "Populate adds nothing")

			got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
			require.NoError(t, err)
			if tt.deleted {
				require.Empty(t, got.Servers, "a wrapper on a deleted toolset is not delivered")
				return
			}
			require.Len(t, got.Servers, 1, "the role audience delivers a wrapper on a live toolset")
			require.Equal(t, wrapper.String(), *got.Servers[0].McpServerID)
		})
	}
}

// The MCP-enable hook does not deliver a wrapper whose toolset was deleted to an existing role audience.
func TestEligibleServerSkipsWrapperOnDeletedToolset(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	role := createTestRolePrincipal(t, ctx, ti, "eligibility-deleted")
	toolset := createUnwrappedTestToolset(t, ctx, ti.conn, "Deleted before")
	wrapper, err := testrepo.New(ti.conn).CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{ID: uuid.New(), ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private"})
	require.NoError(t, err)
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, toolset.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Eligible deleted"})
	require.NoError(t, err)
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{role}})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: toolset.Slug, ProjectID: *ac.ProjectID})
	require.NoError(t, err)

	// The first endpoint makes the wrapper otherwise eligible and runs the attachment hook.
	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: Exercise the public attachment hook in its enclosing eligibility transaction.
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	_, err = endpointrepo.New(tx).CreateMCPEndpoint(ctx, endpointrepo.CreateMCPEndpointParams{ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: wrapper, Valid: true}, Slug: "endpoint-" + wrapper.String()[:8]})
	require.NoError(t, err)
	_, err = plugins.AttachToDefaultAndRolePluginsAudited(ctx, tx, audit.NewLogger(), ac, plugins.AttachToDefaultPluginParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: wrapper, Valid: true}, DisplayName: toolset.Name}, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Empty(t, got.Servers, "a wrapper on a deleted toolset is not delivered on enable")
}
