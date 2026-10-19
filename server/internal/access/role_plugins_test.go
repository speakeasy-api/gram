package access

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	accesshttp "github.com/speakeasy-api/gram/server/gen/http/access/server"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestRolePluginsForResource(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := ac.ActiveOrganizationID
	id := uuid.MustParse(seedRemoteMCPServer(t, ctx, ti.conn, org))
	servers := mcpserversrepo.New(ti.conn)
	server, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: id, OrganizationID: org})
	require.NoError(t, err)
	projectID := server.ProjectID
	pr := pluginsrepo.New(ti.conn)
	admin := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, org)})
	create := func() uuid.UUID {
		t.Helper()
		p, err := pr.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: org, ProjectID: projectID, Name: "Matching plugin", Slug: "plugin-" + uuid.NewString()})
		require.NoError(t, err)
		return p.ID
	}
	assign := func(pluginID uuid.UUID, principal string) {
		t.Helper()
		_, err := pr.AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{OrganizationID: org, PluginID: pluginID, PrincipalUrn: principal})
		require.NoError(t, err)
	}
	attach := func(pluginID uuid.UUID, legacy bool) uuid.UUID {
		t.Helper()
		params := pluginsrepo.AddPluginServerParams{PluginID: pluginID, DisplayName: "Target", Policy: "required"}
		if legacy {
			params.ToolsetID = server.ToolsetID
		} else {
			params.McpServerID = uuid.NullUUID{UUID: id, Valid: true}
		}
		row, err := pr.AddPluginServer(ctx, params)
		require.NoError(t, err)
		return row.ID
	}
	remove := func(pluginID, membershipID uuid.UUID) {
		t.Helper()
		_, err := pr.RemovePluginServer(ctx, pluginsrepo.RemovePluginServerParams{ID: membershipID, PluginID: pluginID})
		require.NoError(t, err)
	}
	visibility := func(target uuid.UUID, value string) {
		t.Helper()
		_, err := servers.UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{ID: target, ProjectID: projectID, ToolsetID: server.ToolsetID, Visibility: value})
		require.NoError(t, err)
	}
	read := func(ctx context.Context, organization string, project, resource uuid.UUID) []pluginsrepo.ListRolePluginsForResourceRow {
		t.Helper()
		rows, err := plugins.RolePluginsForResource(ctx, ti.conn, ti.service.authz, organization, project, resource, []string{"role:admin", "user:all"})
		require.NoError(t, err)
		return rows
	}
	pluginID := create()
	assign(pluginID, "role:admin")
	assign(pluginID, "user:all")
	membershipID := attach(pluginID, false)
	require.Len(t, read(admin, org, projectID, id), 1)
	// Disabled direct targets still explain stored membership.
	visibility(id, "disabled")
	require.Len(t, read(admin, org, projectID, id), 1)
	visibility(id, "private")
	// Both content and exact assignment are necessary; all matches are returned.
	second, contentOnly, assignmentOnly := create(), create(), create()
	attach(second, false)
	attach(contentOnly, false)
	assign(second, "role:admin")
	assign(assignmentOnly, "role:admin")
	matches := read(admin, org, projectID, id)
	matchedIDs := make([]uuid.UUID, 0, len(matches))
	for _, match := range matches {
		matchedIDs = append(matchedIDs, match.PluginID)
	}
	require.ElementsMatch(t, []uuid.UUID{pluginID, second}, matchedIDs)
	_, err = pr.RemoveDeletedRolePluginAssignment(ctx, pluginsrepo.RemoveDeletedRolePluginAssignmentParams{OrganizationID: org, ProjectID: projectID, PluginID: second, PrincipalUrn: "role:admin"})
	require.NoError(t, err)
	matches = read(admin, org, projectID, id)
	require.Len(t, matches, 1)
	require.Equal(t, pluginID, matches[0].PluginID)
	require.Empty(t, read(admin, "org_other", projectID, id))
	require.Empty(t, read(admin, org, uuid.New(), id))
	reader := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, org)})
	require.Nil(t, read(reader, org, projectID, id))
	// A legacy entry follows only its sole active wrapper.
	remove(pluginID, membershipID)
	membershipID = attach(pluginID, true)
	require.Len(t, read(admin, org, projectID, id), 1)
	other, err := testrepo.New(ti.conn).CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{ID: uuid.New(), ProjectID: projectID, ToolsetID: server.ToolsetID, Visibility: "private"})
	require.NoError(t, err)
	require.Empty(t, read(admin, org, projectID, id))
	visibility(other, "disabled")
	require.Len(t, read(admin, org, projectID, id), 1)
	// Gateways match only themselves, not a member server.
	gatewayID, err := testrepo.New(ti.conn).CreateMCPGatewayFixture(ctx, testrepo.CreateMCPGatewayFixtureParams{ID: uuid.New(), OrganizationID: org, ProjectID: projectID, Name: "Audience gateway"})
	require.NoError(t, err)
	remove(pluginID, membershipID)
	gatewayMembership, err := pr.AddGatewayPluginServer(ctx, pluginsrepo.AddGatewayPluginServerParams{PluginID: pluginID, ProjectID: projectID, MetaMcpServerID: uuid.NullUUID{UUID: gatewayID, Valid: true}, DisplayName: "Target", Policy: "required"})
	require.NoError(t, err)
	require.Len(t, read(admin, org, projectID, gatewayID), 1)
	_, err = metamcprepo.New(ti.conn).UpdateMetaMCPServer(ctx, metamcprepo.UpdateMetaMCPServerParams{ID: gatewayID, OrganizationID: org, ProjectID: projectID, Name: "Audience gateway", Visibility: pgtype.Text{String: "disabled", Valid: true}})
	require.NoError(t, err)
	require.Len(t, read(admin, org, projectID, gatewayID), 1)
	require.Empty(t, read(admin, org, projectID, id))
	remove(pluginID, gatewayMembership.ID)
	membershipID = attach(pluginID, true)
	remove(pluginID, membershipID)
	require.Empty(t, read(admin, org, projectID, id))
	attach(pluginID, true)
	err = pr.DeletePlugin(ctx, pluginsrepo.DeletePluginParams{ID: pluginID, OrganizationID: org, ProjectID: projectID})
	require.NoError(t, err)
	rows := read(admin, org, projectID, id)
	require.NotNil(t, rows)
	require.Empty(t, rows)
}

func TestRolePluginsJSONDisclosure(t *testing.T) {
	t.Parallel()
	omitted, err := json.Marshal(accesshttp.NewListResourceAudienceResponseBody(&gen.ResourceAudienceResult{}))
	require.NoError(t, err)
	require.NotContains(t, string(omitted), "role_plugins")
	empty, err := json.Marshal(accesshttp.NewListResourceAudienceResponseBody(&gen.ResourceAudienceResult{RolePlugins: []*gen.ResourceAudienceRolePlugin{}}))
	require.NoError(t, err)
	require.Contains(t, string(empty), `"role_plugins":[]`)
}

func TestListResourceAudienceRolePluginsDisclosure(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	id := seedRemoteMCPServer(t, ctx, ti.conn, ac.ActiveOrganizationID)
	for _, admin := range []bool{true, false} {
		grants := []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID), authz.NewGrant(authz.ScopeMCPRead, id)}
		if admin {
			grants = append(grants, authz.NewGrant(authz.ScopeOrgAdmin, ac.ActiveOrganizationID))
		}
		result, err := ti.service.ListResourceAudience(authz.GrantsToContext(ctx, grants), &gen.ListResourceAudiencePayload{ResourceKind: "mcp", ResourceID: id})
		require.NoError(t, err)
		if admin {
			require.NotNil(t, result.RolePlugins)
			require.Empty(t, result.RolePlugins)
		} else {
			require.Nil(t, result.RolePlugins)
		}
	}
	// A fresh audience read must still authorize/resolve the current target;
	// stored plugin data cannot resurrect a deleted server.
	target, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: uuid.MustParse(id), OrganizationID: ac.ActiveOrganizationID})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: target.ID, ProjectID: target.ProjectID})
	require.NoError(t, err)
	adminCtx := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, ac.ActiveOrganizationID), authz.NewGrant(authz.ScopeMCPRead, id)})
	result, err := ti.service.ListResourceAudience(adminCtx, &gen.ListResourceAudiencePayload{ResourceKind: "mcp", ResourceID: id})
	require.Error(t, err)
	require.Nil(t, result)

}
