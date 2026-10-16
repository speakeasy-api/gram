package mcp_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

// Synthetic values, never real credentials. A failing assertion may print them.
const (
	headerTestSecret      = "synthetic-tunnel-header-secret"
	headerTestChatSession = "synthetic-chat-session"
	headerTestAPIKey      = "synthetic-api-key"
)

type seededTunnelHeaders struct {
	tenantID uuid.UUID
}

// seedTunnelHeaders stores a static header, an encrypted secret header and a
// request-derived header on a tunnel through the encryption boundary.
func seedTunnelHeaders(t *testing.T, ctx context.Context, ti *testInstance, projectID, tunnelID uuid.UUID, regionRequired bool) seededTunnelHeaders {
	t.Helper()
	headers := tunneledmcp.NewHeaders(ti.logger, ti.conn, ti.enc)
	tenant, err := headers.CreateServerHeader(ctx, tunneledmcprepo.CreateServerHeaderParams{
		Name: "X-Jamf-Tenant", Description: conv.ToPGText(""), IsRequired: true, IsSecret: false,
		Value: conv.ToPGText("tenant-1"), ValueFromRequestHeader: conv.PtrToPGTextEmpty(nil),
		TunneledMcpServerID: tunnelID, ProjectID: projectID,
	})
	require.NoError(t, err)
	_, err = headers.CreateServerHeader(ctx, tunneledmcprepo.CreateServerHeaderParams{
		Name: "X-Api-Key", Description: conv.ToPGText(""), IsRequired: true, IsSecret: true,
		Value: conv.ToPGText(headerTestSecret), ValueFromRequestHeader: conv.PtrToPGTextEmpty(nil),
		TunneledMcpServerID: tunnelID, ProjectID: projectID,
	})
	require.NoError(t, err)
	_, err = headers.CreateServerHeader(ctx, tunneledmcprepo.CreateServerHeaderParams{
		Name: "X-Jamf-Region", Description: conv.ToPGText(""), IsRequired: regionRequired, IsSecret: false,
		Value: conv.PtrToPGTextEmpty(nil), ValueFromRequestHeader: conv.ToPGText("X-Client-Region"),
		TunneledMcpServerID: tunnelID, ProjectID: projectID,
	})
	require.NoError(t, err)
	return seededTunnelHeaders{tenantID: tenant.ID}
}

func requireConfiguredHeadersForwarded(t *testing.T, forwarded http.Header, tenant string) {
	t.Helper()
	require.Equal(t, tenant, forwarded.Get("X-Jamf-Tenant"))
	require.Equal(t, headerTestSecret, forwarded.Get("X-Api-Key"), "the decrypted secret reaches the tunnel")
	require.Equal(t, "eu-west", forwarded.Get("X-Jamf-Region"))
}

func requireNoSpeakeasyCredentialsForwarded(t *testing.T, forwarded http.Header) {
	t.Helper()
	for _, name := range []string{"Gram-Key", "Gram-Session", "Gram-Chat-Session", "Gram-Project", "X-Upstream-Token", "X-Gram-Tunnel-Require-Active"} {
		require.Empty(t, forwarded.Values(name), name)
	}
	for _, values := range forwarded {
		for _, v := range values {
			require.NotEqual(t, headerTestChatSession, v)
			require.NotEqual(t, headerTestAPIKey, v)
		}
	}
}

func publicTunnelRequest(slug string, body []byte, sessionID string) *http.Request {
	req := newTunneledPublicRequest(slug, http.MethodPost, body, sessionID)
	req.Header.Set("X-Client-Region", "eu-west")
	req.Header.Set("Gram-Project", "spoofed-project")
	req.Header.Set("X-Gram-Tunnel-Require-Active", "1")
	return req
}

// Public tunnels apply the same configured headers on initialize (the
// tunnel-manager build) and on later session requests (the pinned-session
// build), and Speakeasy's routing fields still win.
func TestTunnelConfiguredHeaders_PublicInitializeAndSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-session", legacy: false, dead: false, busy: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)
	seeded := seedTunnelHeaders(t, ctx, ti, *authCtx.ProjectID, fixture.tunnelID, true)

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServePublic(w, publicTunnelRequest(fixture.endpointSlug, makeInitializeBody(), "")))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	sid := w.Header().Get("Mcp-Session-Id")
	initForward := gateway.lastForward()
	requireConfiguredHeadersForwarded(t, initForward, "tenant-1")
	requireNoSpeakeasyCredentialsForwarded(t, initForward)
	require.Equal(t, fixture.tunnelID.String(), initForward.Get(wire.HeaderTunnelID))

	// An edit is visible on the very next request.
	_, err := tunneledmcp.NewHeaders(ti.logger, ti.conn, ti.enc).UpdateServerHeader(ctx, tunneledmcprepo.UpdateServerHeaderParams{
		Name: "X-Jamf-Tenant", Description: conv.ToPGText(""), IsRequired: true, IsSecret: false, SetValue: true,
		Value: conv.ToPGText("tenant-2"), ValueFromRequestHeader: conv.PtrToPGTextEmpty(nil),
		ID: seeded.tenantID, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	w = httptest.NewRecorder()
	require.NoError(t, ti.service.ServePublic(w, publicTunnelRequest(fixture.endpointSlug, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), sid)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	sessionForward := gateway.lastForward()
	requireConfiguredHeadersForwarded(t, sessionForward, "tenant-2")
	requireNoSpeakeasyCredentialsForwarded(t, sessionForward)
	require.Equal(t, "backend-session", sessionForward.Get("Mcp-Session-Id"), "the pinned backend session replaces the client's")
	require.Equal(t, "agent-1", sessionForward.Get(wire.HeaderTunnelAgentSession))
}

func TestTunnelConfiguredHeaders_PublicRequiredPassThroughMissing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-session", legacy: false, dead: false, busy: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)
	seedTunnelHeaders(t, ctx, ti, *authCtx.ProjectID, fixture.tunnelID, true)

	_, err := serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeInitializeBody(), "")
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
	require.Zero(t, gateway.forwardCount())
}

// Headers belong to the tunnel: every MCP server on it sends them, and a
// server on another tunnel sends none of them.
func TestTunnelConfiguredHeaders_SharedTunnelAndIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-session", legacy: false, dead: false, busy: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)
	seedTunnelHeaders(t, ctx, ti, *authCtx.ProjectID, fixture.tunnelID, false)

	sibling, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText("sibling"),
		Slug:                conv.ToPGText("sibling-" + uuid.NewString()[:8]),
		UserSessionIssuerID: conv.ToNullUUID(createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)),
		TunneledMcpServerID: conv.ToNullUUID(fixture.tunnelID),
		Visibility:          "public",
	})
	require.NoError(t, err)
	siblingSlug := "endpoint-" + uuid.NewString()
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: *authCtx.ProjectID, McpServerID: conv.ToNullUUID(sibling.ID), Slug: siblingSlug,
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServePublic(w, publicTunnelRequest(siblingSlug, makeInitializeBody(), "")))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	requireConfiguredHeadersForwarded(t, gateway.lastForward(), "tenant-1")

	otherGateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-2", backendSessionID: "backend-session-2", legacy: false, dead: false, busy: false, challenge: ""}
	other := newPublicTunnelFixture(t, ctx, ti, otherGateway, true)
	w = httptest.NewRecorder()
	require.NoError(t, ti.service.ServePublic(w, publicTunnelRequest(other.endpointSlug, makeInitializeBody(), "")))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	forwarded := otherGateway.lastForward()
	require.Empty(t, forwarded.Values("X-Jamf-Tenant"))
	require.Empty(t, forwarded.Values("X-Api-Key"))
	require.Empty(t, forwarded.Values("X-Jamf-Region"))
}

// A private tunneled server strips inbound Speakeasy credentials and refuses
// a stored pass-through row that would copy one, even one written around the
// management API.
func TestTunnelConfiguredHeaders_PrivateDirect(t *testing.T) {
	t.Parallel()

	issuer, _ := callerIssuerForTest(t)
	ctx, ti := newTestMCPServiceWithCallerAssertions(t, issuer)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *auth.ProjectID
	sessionIssuer := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "headers-direct-" + uuid.NewString()
	serverID, tunnelID := createPrivateTunneledServer(t, ctx, ti, projectID, sessionIssuer, slug, "")
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: conv.ToNullUUID(serverID), Slug: slug,
	})
	require.NoError(t, err)
	seedMetaMemberConnectGrant(t, ctx, ti.conn, auth.ActiveOrganizationID, serverID)
	seedTunnelHeaders(t, ctx, ti, projectID, tunnelID, false)
	// Written directly, bypassing the management API's policy.
	_, err = tunneledmcprepo.New(ti.conn).CreateServerHeader(ctx, tunneledmcprepo.CreateServerHeaderParams{
		Name: "X-Upstream-Token", Description: conv.ToPGText(""), IsRequired: false, IsSecret: false,
		Value: conv.PtrToPGTextEmpty(nil), ValueFromRequestHeader: conv.ToPGText("Authorization"),
		TunneledMcpServerID: tunnelID, ProjectID: projectID,
	})
	require.NoError(t, err)

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "test-agent", backendSessionID: "backend-session", mu: sync.Mutex{}}
	upstream := httptest.NewServer(gateway)
	t.Cleanup(upstream.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))

	subject := urn.NewUserSubject(auth.UserID)
	bearer := mintMetaIssuerBearer(t, ti, slug, sessionIssuer, subject)
	request := httptest.NewRequest(http.MethodPost, "/mcp/"+slug, bytes.NewReader(makeInitializeBody()))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Gram-Chat-Session", headerTestChatSession)
	request.Header.Set("Gram-Key", headerTestAPIKey)
	request.Header.Set("Gram-Project", "spoofed-project")
	request.Header.Set("X-Gram-Tunnel-Require-Active", "1")
	request.Header.Set("X-Upstream-Token", "client-fallback")
	request.Header.Set("X-Client-Region", "eu-west")
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", slug)
	request = request.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
	response := httptest.NewRecorder()
	require.NoError(t, ti.service.ServePublic(response, request))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	forwarded := gateway.lastForward()
	requireConfiguredHeadersForwarded(t, forwarded, "tenant-1")
	requireNoSpeakeasyCredentialsForwarded(t, forwarded)
	require.NotContains(t, forwarded.Get("Authorization"), bearer)
	require.Equal(t, tunnelID.String(), forwarded.Get(wire.HeaderTunnelID))
}

// The consent transport enumerates tools through the same tunnel build and
// so sends the tunnel's headers too.
func TestTunnelConfiguredHeaders_ConsentTransport(t *testing.T) {
	t.Parallel()

	issuer, _ := callerIssuerForTest(t)
	ctx, ti := newTestMCPServiceWithCallerAssertions(t, issuer)
	_, sessionIssuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	projectID := sessionIssuer.ProjectID.UUID
	slug := "headers-consent-" + uuid.NewString()
	serverID, tunnelID := createPrivateTunneledServer(t, ctx, ti, projectID, sessionIssuer.ID, slug, "")
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{ProjectID: projectID, McpServerID: conv.ToNullUUID(serverID), Slug: slug})
	require.NoError(t, err)
	seedTunnelHeaders(t, ctx, ti, projectID, tunnelID, false)
	stateID, csrf := seedModernConsentChallenge(t, ctx, ti, sessionIssuer.ID, client, serverID, slug)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.AuthorizerUserID = state.Subject.ID
	state.AuthorizerImpersonated = new(false)
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	seedMetaMemberConnectGrant(t, ctx, ti.conn, sessionIssuer.OrganizationID.String, serverID)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "x/mcp")
	require.NoError(t, err)

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "test-agent", backendSessionID: "test-backend-session", mu: sync.Mutex{}}
	upstream := httptest.NewServer(gateway)
	t.Cleanup(upstream.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))

	attempt := uuid.NewString()
	extra := map[string]string{"X-Client-Region": "eu-west"}
	init := serveConsentMCPRequest(t, ctx, ti, endpoint, stateID, csrf, attempt, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, extra)
	require.Contains(t, init.Body.String(), "serverInfo")
	forwarded := gateway.lastForward()
	requireConfiguredHeadersForwarded(t, forwarded, "tenant-1")
	for _, name := range []string{"Gram-Consent-State", "Gram-Consent-Csrf", "Gram-Consent-Inventory-Attempt"} {
		require.Empty(t, forwarded.Values(name), name)
	}

	extra["Mcp-Session-Id"] = init.Header().Get("Mcp-Session-Id")
	extra[mcpversions.HTTPHeader] = "2025-06-18"
	list := serveConsentMCPRequest(t, ctx, ti, endpoint, stateID, csrf, attempt, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, extra)
	require.Contains(t, list.Body.String(), "tools")
	requireConfiguredHeadersForwarded(t, gateway.lastForward(), "tenant-1")
}

// A gateway member dispatches through the same tunnel build.
func TestTunnelConfiguredHeaders_MetaMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-headers-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-session", legacy: false, dead: false, challenge: ""}
	tunnelID, _ := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Tunneled member", "member-tunnel", 0, "")
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), gatewayServer.URL, time.Hour))
	seedTunnelHeaders(t, ctx, ti, projectID, tunnelID, false)

	subject := createTestUser(t, ctx, ti, "meta-headers-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)
	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, text)

	forwarded := gateway.forwardFor(`"tools/call"`)
	require.Equal(t, "tenant-1", forwarded.Get("X-Jamf-Tenant"))
	require.Equal(t, headerTestSecret, forwarded.Get("X-Api-Key"))
	// The gateway call carried no region, and the optional pass-through
	// sends nothing rather than failing.
	require.Empty(t, forwarded.Values("X-Jamf-Region"))
}

// Headers are loaded by tunnel id: a tunnel in another project never sends
// this project's headers, and each tunnel sends its own.
func TestTunnelConfiguredHeaders_CrossProjectIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "other-" + uuid.NewString()[:8]
	other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	otherAuth := *authCtx
	otherAuth.ProjectID = &other.ID
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuth)

	gatewayA := &fakeTunnelGateway{t: t, agentSessionID: "agent-a", backendSessionID: "backend-a", legacy: false, dead: false, busy: false, challenge: ""}
	fixtureA := newPublicTunnelFixture(t, ctx, ti, gatewayA, true)
	seedTunnelHeaders(t, ctx, ti, *authCtx.ProjectID, fixtureA.tunnelID, false)

	gatewayB := &fakeTunnelGateway{t: t, agentSessionID: "agent-b", backendSessionID: "backend-b", legacy: false, dead: false, busy: false, challenge: ""}
	fixtureB := newPublicTunnelFixture(t, otherCtx, ti, gatewayB, true)
	_, err = tunneledmcp.NewHeaders(ti.logger, ti.conn, ti.enc).CreateServerHeader(ctx, tunneledmcprepo.CreateServerHeaderParams{
		Name: "X-Jamf-Tenant", Description: conv.ToPGText(""), IsRequired: true, IsSecret: false,
		Value: conv.ToPGText("tenant-b"), ValueFromRequestHeader: conv.PtrToPGTextEmpty(nil),
		TunneledMcpServerID: fixtureB.tunnelID, ProjectID: other.ID,
	})
	require.NoError(t, err)

	for _, fixture := range []publicTunnelFixture{fixtureA, fixtureB} {
		w := httptest.NewRecorder()
		require.NoError(t, ti.service.ServePublic(w, publicTunnelRequest(fixture.endpointSlug, makeInitializeBody(), "")))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	requireConfiguredHeadersForwarded(t, gatewayA.lastForward(), "tenant-1")
	forwardedB := gatewayB.lastForward()
	require.Equal(t, "tenant-b", forwardedB.Get("X-Jamf-Tenant"))
	require.Empty(t, forwardedB.Values("X-Api-Key"))
	require.Empty(t, forwardedB.Values("X-Jamf-Region"))
}

// A request that first lands on a gateway with no live agent session is
// rerouted by the real tunnel retryer, and the rerouted request still carries
// the tunnel's decrypted static, secret and request-derived headers.
func TestTunnelConfiguredHeaders_RerouteKeepsConfiguredHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	live := &fakeTunnelGateway{t: t, agentSessionID: "agent-live", backendSessionID: "", legacy: false, dead: false, busy: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, live, true)
	seedTunnelHeaders(t, ctx, ti, *authCtx.ProjectID, fixture.tunnelID, false)

	dead := &fakeTunnelGateway{t: t, agentSessionID: "agent-dead", backendSessionID: "", legacy: false, dead: true, busy: false, challenge: ""}
	deadServer := httptest.NewServer(dead)
	t.Cleanup(deadServer.Close)

	// Anonymous requests pick a route at random, and the retryer unpublishes a
	// dead route, so republish it until a request lands there first. The odds
	// of never doing so in this many attempts are negligible.
	const maxAttempts = 32
	for range maxAttempts {
		if dead.forwardCount() > 0 {
			break
		}
		require.NoError(t, ti.tunnelRoutes.Publish(ctx, fixture.tunnelID.String(), deadServer.URL, time.Hour))
		w := httptest.NewRecorder()
		require.NoError(t, ti.service.ServePublic(w, publicTunnelRequest(fixture.endpointSlug, makeInitializeBody(), "")))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	require.Equal(t, 1, dead.forwardCount(), "a request should have been routed to the dead gateway first")

	requireConfiguredHeadersForwarded(t, dead.lastForward(), "tenant-1")
	rerouted := live.lastForward()
	requireConfiguredHeadersForwarded(t, rerouted, "tenant-1")
	requireNoSpeakeasyCredentialsForwarded(t, rerouted)
	require.Equal(t, fixture.tunnelID.String(), rerouted.Get(wire.HeaderTunnelID))
}
