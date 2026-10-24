package agent_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

// stubConnectionStatuses answers MemberConnectionStatus from a fixed table and
// records the user each call was made for.
type stubConnectionStatuses struct {
	mu      sync.Mutex
	byMCP   map[uuid.UUID]platformmcp.GetMyMCPConnectionStatusOutput
	missing map[uuid.UUID]bool
	users   []string
}

func (s *stubConnectionStatuses) MemberConnectionStatus(_ context.Context, principal platformmcp.Principal, _ []authz.Grant, _, mcpID uuid.UUID) (platformmcp.GetMyMCPConnectionStatusOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = append(s.users, principal.UserID)
	if s.missing[mcpID] {
		return platformmcp.GetMyMCPConnectionStatusOutput{}, platformmcp.ErrMemberMCPStatusTargetNotFound
	}
	return s.byMCP[mcpID], nil
}

func connectionByID(t *testing.T, res *gen.GetMyConnectionsResult, id uuid.UUID) *gen.AgentServerConnection {
	t.Helper()
	for _, c := range res.Servers {
		if c.ID == id.String() {
			return c
		}
	}
	require.FailNowf(t, "server not reported", "id %s", id)
	return nil
}

func TestGetMyConnections_ReportsEachServerOnceWithConnectLinks(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	engineering := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	gtm := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "gtm")
	assignPlugin(t, ctx, ti.conn, engineering, ti.orgID, "*")
	assignPlugin(t, ctx, ti.conn, gtm, ti.orgID, "*")
	linear, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, engineering, "Linear", 1)
	datadog, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, engineering, "Datadog", 2)
	hidden, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, gtm, "Hidden", 1)
	gateway := seedPluginGateway(t, ctx, ti.conn, ti.orgID, ti.projectID, gtm, "Gateway", 2)
	// Linear sits in both plugins; it is one server with one sign-in.
	addExistingServerToPlugin(t, ctx, ti, gtm, linear, "Linear", 3)

	stub := &stubConnectionStatuses{
		byMCP: map[uuid.UUID]platformmcp.GetMyMCPConnectionStatusOutput{
			linear:  {State: platformmcp.MCPConnectionStateNotConnected, ConnectionURL: testServerURL + "/mcp/linear"},
			datadog: {State: platformmcp.MCPConnectionStateActive, ConnectionURL: testServerURL + "/mcp/datadog"},
		},
		missing: map[uuid.UUID]bool{hidden: true},
	}
	ti.service.WithConnectionStatuses(stub)

	res, err := ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.Len(t, res.Servers, 3, "Linear once, Datadog, and the gateway; the server the member can't see is omitted")
	l := connectionByID(t, res, linear)
	require.Equal(t, "not_connected", l.State)
	require.Equal(t, testServerURL+"/mcp/linear/connect/first-party", *l.ConnectURL)
	d := connectionByID(t, res, datadog)
	require.Equal(t, "connected", d.State)
	require.Nil(t, d.ConnectURL, "a signed-in server needs no sign-in link")
	g := connectionByID(t, res, gateway)
	require.Equal(t, "unknown", g.State)

	for _, user := range stub.users {
		require.Equal(t, mockidp.MockUserID, user, "status is read for the vouched member")
	}
	ids := make([]string, 0, len(res.Servers))
	for _, c := range res.Servers {
		ids = append(ids, c.ID)
	}
	require.Equal(t, []string{linear.String(), datadog.String(), gateway.String()}, ids, "servers follow the delivered plugin order")
}

func TestGetMyConnections_SkipsUndeliveredPlugins(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	delivered := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, delivered, ti.orgID, "*")
	linear, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, delivered, "Linear", 1)
	// A second project with no published marketplace delivers nothing.
	other := seedProject(t, ctx, ti.conn, ti.orgID, "unpublished")
	hidden := seedPlugin(t, ctx, ti.conn, ti.orgID, other, "hidden")
	assignPlugin(t, ctx, ti.conn, hidden, ti.orgID, "*")
	unseen, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, other, hidden, "Unseen", 1)
	ti.service.WithConnectionStatuses(&stubConnectionStatuses{})

	res, err := ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	ids := make([]string, 0, len(res.Servers))
	for _, c := range res.Servers {
		ids = append(ids, c.ID)
	}
	require.Contains(t, ids, linear.String())
	require.NotContains(t, ids, unseen.String(), "a plugin the device doesn't receive reports no servers")
}

func TestGetMyConnections_MapsStates(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		platformmcp.MCPConnectionStateReauthorizationRequired: "reauthorization_required",
		platformmcp.MCPConnectionStateNotApplicable:           "not_required",
		platformmcp.MCPConnectionStateSetupRequired:           "setup_required",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAgentService(t)
			publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
			plugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
			assignPlugin(t, ctx, ti.conn, plugin, ti.orgID, "*")
			server, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, plugin, "Linear", 1)
			ti.service.WithConnectionStatuses(&stubConnectionStatuses{byMCP: map[uuid.UUID]platformmcp.GetMyMCPConnectionStatusOutput{
				server: {State: in, ConnectionURL: testServerURL + "/mcp/linear"},
			}})

			res, err := ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: new(mockidp.MockUserEmail)})
			require.NoError(t, err)
			c := connectionByID(t, res, server)
			require.Equal(t, want, c.State)
			require.Equal(t, want == "reauthorization_required", c.ConnectURL != nil, "only states the user can fix carry a sign-in link")
		})
	}
}

func TestGetMyConnections_OrgKeyRequiresVouchedEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	_, err := ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: nil})
	requireCode(t, err, oops.CodeBadRequest)
}

func TestGetMyConnections_PerUserKeyReadsKeyOwner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = withPerUserKeyAuth(t, ctx, mockidp.MockUserEmail)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	plugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, plugin, ti.orgID, "*")
	seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, plugin, "Linear", 1)
	stub := &stubConnectionStatuses{}
	ti.service.WithConnectionStatuses(stub)

	_, err := ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: new("someone-else@example.com")})
	require.NoError(t, err)
	require.Equal(t, []string{mockidp.MockUserID}, stub.users, "a per-user key ignores the vouched email")
}

func TestGetMyConnections_NonMemberHasNoConnections(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	plugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, plugin, ti.orgID, "*")
	seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, plugin, "Linear", 1)
	stub := &stubConnectionStatuses{}
	ti.service.WithConnectionStatuses(stub)

	res, err := ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: new("stranger@example.com")})
	require.NoError(t, err)
	require.Empty(t, res.Servers)
	require.Empty(t, stub.users)
}

func addExistingServerToPlugin(t *testing.T, ctx context.Context, ti *testInstance, pluginID, serverID uuid.UUID, displayName string, sortOrder int32) {
	t.Helper()
	_, err := pluginsrepo.New(ti.conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID:    pluginID,
		McpServerID: uuid.NullUUID{UUID: serverID, Valid: true},
		DisplayName: displayName,
		Policy:      "required",
		SortOrder:   sortOrder,
	})
	require.NoError(t, err)
}

func TestGetMyConnections_NoPermissionIsNoAccess(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	plugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, plugin, ti.orgID, "*")
	server, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, plugin, "Linear", 1)
	ti.service.WithConnectionStatuses(&stubConnectionStatuses{byMCP: map[uuid.UUID]platformmcp.GetMyMCPConnectionStatusOutput{
		server: {
			State: platformmcp.MCPConnectionStateNotApplicable, Reason: platformmcp.MCPConnectionReasonPermissionRequired,
			RequiredScope: "mcp:connect", RequestAccessURL: testSiteURL + "/request-access",
		},
	}})

	res, err := ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	c := connectionByID(t, res, server)
	require.Equal(t, "no_access", c.State, "a member who may not connect can't fix it by signing in")
	require.Equal(t, testSiteURL+"/request-access", *c.RequestAccessURL)
	require.Nil(t, c.ConnectURL)
}

func TestGetMyConnections_RateLimitedPerMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ti.service.WithConnectionsLimiter(ratelimit.New(ratelimit.NewRedisStore(ti.redis), "test-get-my-connections-"+uuid.NewString(), ratelimit.PerMinute(1)))

	_, err := ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	_, err = ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: new(mockidp.MockUserEmail)})
	requireCode(t, err, oops.CodeRateLimitExceeded)
	_, err = ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: new("other@example.com")})
	require.NoError(t, err, "the budget is per member, not per key")
}
