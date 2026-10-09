// Per-tool RBAC for private tunneled MCP servers: the tools/list mcp:connect
// filter and the tools/call authz interceptor resolve grants and stored tool
// metadata by the fronting mcp_servers id, so a grant narrowed by tool or by
// disposition admits exactly those tools, and two servers fronting one tunnel
// are authorized independently.
package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
)

// tunnelRBACTools is what the fake tunneled backend advertises. Annotations
// are deliberately omitted upstream: dispositions come only from the
// metadata Speakeasy stores for each fronting server.
const tunnelRBACTools = `[
	{"name":"list_devices","inputSchema":{"type":"object"}},
	{"name":"wipe_device","inputSchema":{"type":"object"}},
	{"name":"ping","inputSchema":{"type":"object"}},
	{"name":"device_status","inputSchema":{"type":"object"}}
]`

// privateTunnelRBACFixture is one private MCP server fronting a tunnel, with
// its own endpoint and issuer, and a bearer for the mock user.
type privateTunnelRBACFixture struct {
	serverID uuid.UUID
	slug     string
	bearer   string
}

// addPrivateTunnelRBACServer fronts tunnelID with a new private MCP server
// whose stored metadata is the given JSON array.
func addPrivateTunnelRBACServer(t *testing.T, ctx context.Context, ti *testInstance, tunnelID uuid.UUID, metadataJSON string) privateTunnelRBACFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "tunnel-rbac-" + uuid.NewString()[:8]
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		EnvironmentID:       uuid.NullUUID{},
		UserSessionIssuerID: conv.ToNullUUID(issuerID),
		RemoteMcpServerID:   uuid.NullUUID{},
		TunneledMcpServerID: conv.ToNullUUID(tunnelID),
		ToolsetID:           uuid.NullUUID{},
		Visibility:          "private",
	})
	require.NoError(t, err)

	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: uuid.NullUUID{},
		McpServerID:    conv.ToNullUUID(server.ID),
		Slug:           slug,
	})
	require.NoError(t, err)

	if metadataJSON != "" {
		_, err = mcpserversrepo.New(ti.conn).AddMCPServerToolMetadata(ctx, mcpserversrepo.AddMCPServerToolMetadataParams{
			ProjectID:   projectID,
			McpServerID: server.ID,
			Tools:       []byte(metadataJSON),
		})
		require.NoError(t, err)
	}

	return privateTunnelRBACFixture{
		serverID: server.ID,
		slug:     slug,
		bearer:   mintMetaIssuerBearer(t, ti, slug, issuerID, urn.NewUserSubject(mockidp.MockUserID)),
	}
}

// newPrivateTunnelRBACGateway creates a tunnel source routed to a fake gateway
// that advertises tunnelRBACTools.
func newPrivateTunnelRBACGateway(t *testing.T, ctx context.Context, ti *testInstance) (uuid.UUID, *fakeTunnelGateway) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunnel, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:                 uuid.New(),
		ProjectID:          *authCtx.ProjectID,
		Name:               "rbac-tunnel-" + uuid.NewString()[:8],
		KeyHash:            uuid.NewString(),
		KeyPrefix:          "gram_tunnel_test",
		ResourceIdentifier: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-rbac", toolsJSON: tunnelRBACTools}
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnel.ID.String(), gatewayServer.URL, time.Hour))

	return tunnel.ID, gateway
}

// seedMockUserMCPGrant grants mcp:connect with the given narrowing to the mock
// user that private-endpoint bearers are minted for.
func seedMockUserMCPGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, serverID uuid.UUID, narrowing map[string]string) {
	t.Helper()

	selector := authz.NewSelector(authz.ScopeMCPConnect, serverID.String())
	maps.Copy(selector, narrowing)
	selectors, err := selector.MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID),
		Scope:          string(authz.ScopeMCPConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

// servePrivateTunnel sends one JSON-RPC request to a private tunneled endpoint
// as a remote MCP client would: the bearer is the only credential.
func servePrivateTunnel(t *testing.T, ti *testInstance, fixture privateTunnelRBACFixture, sessionID string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/mcp/"+fixture.slug, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Authorization", "Bearer "+fixture.bearer)
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", fixture.slug)
	request = request.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, route))

	response := httptest.NewRecorder()
	require.NoError(t, ti.service.ServePublic(response, request))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	return response
}

// initializePrivateTunnel opens a session and returns its id, empty when the
// endpoint is sessionless.
func initializePrivateTunnel(t *testing.T, ti *testInstance, fixture privateTunnelRBACFixture) string {
	t.Helper()

	response := servePrivateTunnel(t, ti, fixture, "", makeInitializeBody())
	return response.Header().Get("Mcp-Session-Id")
}

func listPrivateTunnelTools(t *testing.T, ti *testInstance, fixture privateTunnelRBACFixture, sessionID string) []string {
	t.Helper()

	result := decodeMCPResult(t, servePrivateTunnel(t, ti, fixture, sessionID, makeToolsListBody()).Body.Bytes())
	tools, ok := result["tools"].([]any)
	require.True(t, ok, "tools/list result must carry a tools array")

	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		entry, ok := tool.(map[string]any)
		require.True(t, ok)
		name, ok := entry["name"].(string)
		require.True(t, ok)
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func namedToolsCallBody(t *testing.T, name string) []byte {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": map[string]any{}},
	})
	require.NoError(t, err)
	return body
}

// forwardedToolCalls counts the tools/call requests that reached the tunnel
// gateway; initialize and listing traffic is legitimate either way.
func forwardedToolCalls(g *fakeTunnelGateway, name string) int {
	_, bodies := tunnelForwards(g)
	count := 0
	for _, body := range bodies {
		if strings.Contains(body, `"tools/call"`) && strings.Contains(body, `"name":"`+name+`"`) {
			count++
		}
	}
	return count
}

// requireToolCallAllowed asserts the call reached the backend and returned its
// result.
func requireToolCallAllowed(t *testing.T, ti *testInstance, fixture privateTunnelRBACFixture, sessionID string, gateway *fakeTunnelGateway, name string) {
	t.Helper()

	before := forwardedToolCalls(gateway, name)
	result := decodeMCPResult(t, servePrivateTunnel(t, ti, fixture, sessionID, namedToolsCallBody(t, name)).Body.Bytes())
	require.NotEqual(t, true, result["isError"], "tools/call %s must succeed", name)
	require.Equal(t, before+1, forwardedToolCalls(gateway, name), "an allowed %s call must reach the tunnel", name)
}

// requireToolCallDenied asserts the call was refused without being forwarded.
func requireToolCallDenied(t *testing.T, ti *testInstance, fixture privateTunnelRBACFixture, sessionID string, gateway *fakeTunnelGateway, name string) {
	t.Helper()

	before := forwardedToolCalls(gateway, name)
	body := servePrivateTunnel(t, ti, fixture, sessionID, namedToolsCallBody(t, name)).Body.String()
	require.Contains(t, body, `"error"`, "tools/call %s must be refused: %s", name, body)
	require.NotContains(t, body, "pong through the tunnel", "a refused call must not carry the backend result")
	require.Equal(t, before, forwardedToolCalls(gateway, name), "a refused %s call must never reach the tunnel", name)
}

// ping has no stored metadata; device_status is stored with every hint false,
// so both are unclassified.
const tunnelRBACMetadata = `[
	{"tool_name":"list_devices","read_only_hint":true},
	{"tool_name":"wipe_device","read_only_hint":false,"destructive_hint":true},
	{"tool_name":"device_status","read_only_hint":false,"destructive_hint":false,"idempotent_hint":false,"open_world_hint":false}
]`

func TestServePublic_PrivateTunneled_ToolGrantNarrowsListAndCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunnelID, gateway := newPrivateTunnelRBACGateway(t, ctx, ti)
	fixture := addPrivateTunnelRBACServer(t, ctx, ti, tunnelID, tunnelRBACMetadata)
	seedMockUserMCPGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, fixture.serverID, map[string]string{authz.SelectorKeyTool: "wipe_device"})

	sessionID := initializePrivateTunnel(t, ti, fixture)
	require.Equal(t, []string{"wipe_device"}, listPrivateTunnelTools(t, ti, fixture, sessionID))

	requireToolCallAllowed(t, ti, fixture, sessionID, gateway, "wipe_device")
	requireToolCallDenied(t, ti, fixture, sessionID, gateway, "list_devices")
	requireToolCallDenied(t, ti, fixture, sessionID, gateway, "ping")
}

// A disposition grant admits only tools whose stored metadata carries that
// disposition. A tool with no stored metadata, or stored with no hint set, has
// no disposition, so no disposition grant reaches it.
func TestServePublic_PrivateTunneled_DispositionGrantFollowsStoredMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunnelID, gateway := newPrivateTunnelRBACGateway(t, ctx, ti)
	fixture := addPrivateTunnelRBACServer(t, ctx, ti, tunnelID, tunnelRBACMetadata)
	seedMockUserMCPGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, fixture.serverID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})

	sessionID := initializePrivateTunnel(t, ti, fixture)
	require.Equal(t, []string{"list_devices"}, listPrivateTunnelTools(t, ti, fixture, sessionID))

	requireToolCallAllowed(t, ti, fixture, sessionID, gateway, "list_devices")
	requireToolCallDenied(t, ti, fixture, sessionID, gateway, "wipe_device")
	requireToolCallDenied(t, ti, fixture, sessionID, gateway, "ping")
	requireToolCallDenied(t, ti, fixture, sessionID, gateway, "device_status")
}

// Two MCP servers fronting the same tunnel hold different metadata and
// grants. Each is authorized by its own mcp_servers id: one server's grant
// never admits a tool on the other, and the same tool name can carry a
// different disposition on each.
func TestServePublic_PrivateTunneled_ServersSharingATunnelAuthorizeIndependently(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunnelID, gateway := newPrivateTunnelRBACGateway(t, ctx, ti)
	readOnly := addPrivateTunnelRBACServer(t, ctx, ti, tunnelID, tunnelRBACMetadata)
	// The second server classifies the same backend tools the other way round.
	inverted := addPrivateTunnelRBACServer(t, ctx, ti, tunnelID, `[
		{"tool_name":"list_devices","read_only_hint":false,"destructive_hint":true},
		{"tool_name":"wipe_device","read_only_hint":true}
	]`)
	seedMockUserMCPGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, readOnly.serverID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})
	seedMockUserMCPGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, inverted.serverID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})

	readOnlySession := initializePrivateTunnel(t, ti, readOnly)
	require.Equal(t, []string{"list_devices"}, listPrivateTunnelTools(t, ti, readOnly, readOnlySession))
	requireToolCallAllowed(t, ti, readOnly, readOnlySession, gateway, "list_devices")
	requireToolCallDenied(t, ti, readOnly, readOnlySession, gateway, "ping")

	// The same read-only grant reaches the opposite tool on the second
	// server, because its own stored metadata decides.
	invertedSession := initializePrivateTunnel(t, ti, inverted)
	require.Equal(t, []string{"wipe_device"}, listPrivateTunnelTools(t, ti, inverted, invertedSession))
	requireToolCallAllowed(t, ti, inverted, invertedSession, gateway, "wipe_device")
	requireToolCallDenied(t, ti, inverted, invertedSession, gateway, "list_devices")
	requireToolCallDenied(t, ti, inverted, invertedSession, gateway, "ping")
}

// A private tunneled member of a meta gateway is authorized by its own
// mcp_servers id and stored metadata: execute_tool runs a tool its grant
// reaches and refuses one it does not, without forwarding the refused call.
func TestServePublic_MetaEndpoint_ExecuteTool_PrivateTunneledMemberEnforcesToolGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-tunnel-rbac-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", toolsJSON: tunnelRBACTools}
	tunnelID, _, memberID := seedTunneledMetaMemberWithVisibility(t, ctx, ti, projectID, meta.ID, "Tunneled member", "member-tunnel", 0, "", "private")
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), gatewayServer.URL, time.Hour))

	_, err := mcpserversrepo.New(ti.conn).AddMCPServerToolMetadata(ctx, mcpserversrepo.AddMCPServerToolMetadataParams{
		ProjectID:   projectID,
		McpServerID: memberID,
		Tools:       []byte(tunnelRBACMetadata),
	})
	require.NoError(t, err)

	// Only the member's read-only tools, for an organization member with no
	// other grant.
	seedMockUserMCPGrant(t, ctx, ti.conn, orgID, memberID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, urn.NewUserSubject(mockidp.MockUserID))

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--list_devices"))
	require.False(t, isError, "a granted member tool must run: %s", text)
	require.Contains(t, text, "pong through the tunnel")
	require.Equal(t, 1, forwardedToolCalls(gateway, "list_devices"))

	for _, tool := range []string{"wipe_device", "ping", "device_status"} {
		text, isError = metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--"+tool))
		require.True(t, isError, "%s must be refused: %s", tool, text)
		require.Zero(t, forwardedToolCalls(gateway, tool), "a refused %s call must never reach the tunnel", tool)
	}
}
