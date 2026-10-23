package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	agenthttp "github.com/speakeasy-api/gram/server/gen/http/agent/server"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// seedPluginMCPServer adds a toolset-backed MCP server to the project and to
// the plugin, returning the server id and the toolset id its grants key on.
func seedPluginMCPServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, projectID, pluginID uuid.UUID, displayName string, sortOrder int32) (uuid.UUID, uuid.UUID) {
	t.Helper()
	fixtures := testrepo.New(conn)

	toolsetID := uuid.New()
	_, err := fixtures.CreateToolsetFixture(ctx, testrepo.CreateToolsetFixtureParams{
		ID:             toolsetID,
		OrganizationID: orgID,
		ProjectID:      projectID,
		Name:           displayName,
		Slug:           "toolset-" + toolsetID.String()[:8],
	})
	require.NoError(t, err)

	serverID := uuid.New()
	_, err = fixtures.CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{
		ID:         serverID,
		ProjectID:  projectID,
		ToolsetID:  uuid.NullUUID{UUID: toolsetID, Valid: true},
		Visibility: "private",
	})
	require.NoError(t, err)

	_, err = pluginsrepo.New(conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID:    pluginID,
		McpServerID: uuid.NullUUID{UUID: serverID, Valid: true},
		DisplayName: displayName,
		Policy:      "required",
		SortOrder:   sortOrder,
	})
	require.NoError(t, err)
	return serverID, toolsetID
}

func seedPluginGateway(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, projectID, pluginID uuid.UUID, displayName string, sortOrder int32) uuid.UUID {
	t.Helper()
	gatewayID := uuid.New()
	_, err := testrepo.New(conn).CreateMCPGatewayFixture(ctx, testrepo.CreateMCPGatewayFixtureParams{
		ID:             gatewayID,
		OrganizationID: orgID,
		ProjectID:      projectID,
		Name:           displayName,
	})
	require.NoError(t, err)

	_, err = pluginsrepo.New(conn).AddGatewayPluginServer(ctx, pluginsrepo.AddGatewayPluginServerParams{
		MetaMcpServerID: uuid.NullUUID{UUID: gatewayID, Valid: true},
		DisplayName:     displayName,
		Policy:          "required",
		SortOrder:       sortOrder,
		PluginID:        pluginID,
		ProjectID:       projectID,
	})
	require.NoError(t, err)
	return gatewayID
}

func grantUserMCPRead(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, userID, resourceID string) {
	t.Helper()
	selectors, err := authz.NewSelector(authz.ScopeMCPRead, resourceID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		Scope:          string(authz.ScopeMCPRead),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func pluginBySlug(t *testing.T, res *gen.GetPluginsResult, slug string) *gen.AgentPlugin {
	t.Helper()
	for _, p := range res.Plugins {
		if p.Slug == slug {
			return p
		}
	}
	require.FailNowf(t, "plugin not delivered", "slug %q", slug)
	return nil
}

func TestGetPlugins_ListsPluginServersWithDashboardLinksForMCPRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, ti.projectID)
	require.NoError(t, err)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	pluginID := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, pluginID, ti.orgID, "*")
	// Seeded out of display order to prove sort_order wins.
	seedPluginGateway(t, ctx, ti.conn, ti.orgID, ti.projectID, pluginID, "Gateway", 2)
	serverID, toolsetID := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, pluginID, "Linear", 1)
	grantUserMCPRead(t, ctx, ti.conn, ti.orgID, mockidp.MockUserID, toolsetID.String())

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	servers := pluginBySlug(t, res, "engineering").Servers
	require.Len(t, servers, 2)
	require.Equal(t, "Linear", servers[0].Name)
	require.Equal(t, serverID.String(), *servers[0].ID, "the server id is shared by every plugin and keys getMyConnections")
	require.Equal(t,
		testSiteURL+"/"+authCtx.OrganizationSlug+"/projects/"+project.Slug+"/mcp/x/"+serverID.String(),
		*servers[0].DashboardURL,
		"mcp:read on the server's backing toolset links its dashboard page")
	require.Equal(t, "Gateway", servers[1].Name)
	require.Nil(t, servers[1].DashboardURL,
		"a grant on one server does not extend to a gateway in the same project")

	require.Nil(t, pluginBySlug(t, res, wantObservability).Servers,
		"observability carries hooks, not servers, so it sends no server list")
}

func TestGetPlugins_WithholdsDashboardLinksWithoutMCPRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	pluginID := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, pluginID, ti.orgID, "*")
	seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, pluginID, "Linear", 0)

	// An address that is not an org member resolves to no RBAC principals, so
	// it holds no grants even though the wildcard delivers it the plugin.
	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("stranger@example.com")})
	require.NoError(t, err)

	servers := pluginBySlug(t, res, "engineering").Servers
	require.Len(t, servers, 1)
	require.Equal(t, "Linear", servers[0].Name)
	require.Nil(t, servers[0].DashboardURL)
}

func TestGetPlugins_PluginWithoutServersSendsEmptyList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	pluginID := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "basket")
	assignPlugin(t, ctx, ti.conn, pluginID, ti.orgID, "*")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	servers := pluginBySlug(t, res, "basket").Servers
	require.NotNil(t, servers, "an empty list, not an absent one, tells the agent the plugin has no servers")
	require.Empty(t, servers)
}

// TestGetPlugins_EncodesServerListPresence pins the wire contract the agent
// relies on: an empty list is sent as [] ("No servers"), while observability
// sends no servers key at all.
func TestGetPlugins_EncodesServerListPresence(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	pluginID := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "basket")
	assignPlugin(t, ctx, ti.conn, pluginID, ti.orgID, "*")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	encoded, err := json.Marshal(agenthttp.NewGetPluginsResponseBody(res))
	require.NoError(t, err)
	var wire struct {
		Plugins []map[string]json.RawMessage `json:"plugins"`
	}
	require.NoError(t, json.Unmarshal(encoded, &wire))

	bySlug := make(map[string]map[string]json.RawMessage, len(wire.Plugins))
	for _, p := range wire.Plugins {
		var slug string
		require.NoError(t, json.Unmarshal(p["slug"], &slug))
		bySlug[slug] = p
	}
	require.JSONEq(t, `[]`, string(bySlug["basket"]["servers"]))
	require.NotContains(t, bySlug[wantObservability], "servers")
}

func TestGetPlugins_SkipsDisabledPluginServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	pluginID := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, pluginID, ti.orgID, "*")
	seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, pluginID, "Linear", 0)
	disabledID, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, pluginID, "Retired", 1)
	err := testrepo.New(ti.conn).SetMCPServerVisibilityFixture(ctx, testrepo.SetMCPServerVisibilityFixtureParams{
		Visibility: "disabled",
		ID:         disabledID,
		ProjectID:  ti.projectID,
	})
	require.NoError(t, err)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	servers := pluginBySlug(t, res, "engineering").Servers
	require.Len(t, servers, 1, "a turned-off server is no longer delivered by the plugin")
	require.Equal(t, "Linear", servers[0].Name)
}

func TestGetPlugins_SkipsDeletedPluginServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	pluginID := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, pluginID, ti.orgID, "*")
	seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, pluginID, "Linear", 0)

	deletedServerID, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, pluginID, "Deleted server", 1)
	_, err := mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{
		ID:        deletedServerID,
		ProjectID: ti.projectID,
	})
	require.NoError(t, err)

	removedServerID, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, pluginID, "Removed membership", 2)
	membership, err := pluginsrepo.New(ti.conn).GetPluginServerByBackend(ctx, pluginsrepo.GetPluginServerByBackendParams{
		PluginID:       pluginID,
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		McpServerID:    uuid.NullUUID{UUID: removedServerID, Valid: true},
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(ti.conn).RemovePluginServer(ctx, pluginsrepo.RemovePluginServerParams{ID: membership.ID, PluginID: pluginID})
	require.NoError(t, err)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	servers := pluginBySlug(t, res, "engineering").Servers
	require.Len(t, servers, 1, "a deleted server and a removed membership are no longer delivered by the plugin")
	require.Equal(t, "Linear", servers[0].Name)
}

func TestGetPlugins_ETagTracksPluginServersAndDashboardAccess(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	pluginID := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, pluginID, ti.orgID, "*")
	payload := &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)}

	before, err := ti.service.GetPlugins(ctx, payload)
	require.NoError(t, err)

	_, toolsetID := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, pluginID, "Linear", 0)
	added, err := ti.service.GetPlugins(ctx, payload)
	require.NoError(t, err)
	require.NotEqual(t, before.Etag, added.Etag, "adding a server to a plugin changes the ETag")

	grantUserMCPRead(t, ctx, ti.conn, ti.orgID, mockidp.MockUserID, toolsetID.String())
	granted, err := ti.service.GetPlugins(ctx, payload)
	require.NoError(t, err)
	require.NotEqual(t, added.Etag, granted.Etag, "gaining a dashboard link changes the ETag")
}
