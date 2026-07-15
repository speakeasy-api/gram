package mcp_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

// fakeTunnelGateway emulates the tunnel gateway's forward listener fronting a
// customer MCP backend: it enforces exact-target semantics, reports the agent
// session it served, and captures forwarded headers for assertions.
type fakeTunnelGateway struct {
	t              *testing.T
	agentSessionID string
	// backendSessionID is the Mcp-Session-Id the fake customer backend mints
	// on initialize and requires on session-bearing requests. Empty simulates
	// a sessionless backend.
	backendSessionID string
	// legacy simulates a gateway that predates exact-target support: it never
	// reports the agent session it served.
	legacy bool
	// dead simulates the agent session being gone: every forward answers
	// no-live-session.
	dead bool
	// challenge, when set, is emitted as WWW-Authenticate on every backend
	// response to prove Gram strips it for anonymous callers.
	challenge string

	mu       sync.Mutex
	forwards []http.Header
}

func (g *fakeTunnelGateway) lastForward() http.Header {
	g.mu.Lock()
	defer g.mu.Unlock()
	require.NotEmpty(g.t, g.forwards, "expected at least one forwarded request")
	return g.forwards[len(g.forwards)-1]
}

func (g *fakeTunnelGateway) forwardCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.forwards)
}

func (g *fakeTunnelGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.forwards = append(g.forwards, r.Header.Clone())
	g.mu.Unlock()

	exact := strings.TrimSpace(r.Header.Get(wire.HeaderTunnelAgentSession))
	if g.dead || (exact != "" && exact != g.agentSessionID) {
		w.Header().Set(wire.HeaderTunnelError, wire.TunnelErrorNoLiveSession)
		http.Error(w, "tunnel has no live agent session", http.StatusBadGateway)
		return
	}
	if !g.legacy {
		w.Header().Set(wire.HeaderTunnelAgentSession, g.agentSessionID)
	}
	if g.challenge != "" {
		w.Header().Set("WWW-Authenticate", g.challenge)
	}

	// Customer backend behavior.
	switch r.Method {
	case http.MethodDelete:
		if g.backendSessionID == "" || r.Header.Get("Mcp-Session-Id") != g.backendSessionID {
			http.Error(w, "unknown session", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	case http.MethodPost:
		body := make([]byte, 0, 1024)
		buf := bytes.NewBuffer(body)
		_, _ = buf.ReadFrom(r.Body)
		if strings.Contains(buf.String(), `"initialize"`) {
			if g.backendSessionID != "" {
				w.Header().Set("Mcp-Session-Id", g.backendSessionID)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"fake-tunnel-backend","version":"1.0.0"}}}`)
			return
		}
		// Session-bearing method: the backend only knows its own session id.
		if g.backendSessionID != "" && r.Header.Get("Mcp-Session-Id") != g.backendSessionID {
			http.Error(w, "unknown session", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// publicTunnelFixture seeds a tunneled source with allow_public consent, a
// public mcp_server fronting it, an mcp_endpoint, and a live fake gateway
// route.
type publicTunnelFixture struct {
	endpointSlug string
	tunnelID     uuid.UUID
	gateway      *fakeTunnelGateway
}

func newPublicTunnelFixture(t *testing.T, ctx context.Context, ti *testInstance, gateway *fakeTunnelGateway, allowPublic bool) publicTunnelFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	tunneledID, err := uuid.NewV7()
	require.NoError(t, err)
	tunneledServer, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:        tunneledID,
		ProjectID: *authCtx.ProjectID,
		Name:      "public-tunnel-" + uuid.NewString()[:8],
		KeyHash:   uuid.NewString(),
		KeyPrefix: "gram_tunnel_test",
	})
	require.NoError(t, err)

	if allowPublic {
		_, err = tunneledmcprepo.New(ti.conn).UpdateServer(ctx, tunneledmcprepo.UpdateServerParams{
			Name:        tunneledServer.Name,
			AllowPublic: pgtype.Bool{Bool: true, Valid: true},
			ID:          tunneledServer.ID,
			ProjectID:   *authCtx.ProjectID,
		})
		require.NoError(t, err)
	}

	id, err := uuid.NewV7()
	require.NoError(t, err)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	mcpServer, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  id,
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText("public tunneled mcp server"),
		Slug:                conv.ToPGText("public-tunneled-" + uuid.NewString()[:8]),
		EnvironmentID:       uuid.NullUUID{},
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{},
		TunneledMcpServerID: uuid.NullUUID{UUID: tunneledServer.ID, Valid: true},
		ToolsetID:           uuid.NullUUID{},
		Visibility:          "public",
	})
	require.NoError(t, err)

	endpointSlug := "endpoint-" + uuid.NewString()
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{},
		McpServerID:    mcpServer.ID,
		Slug:           endpointSlug,
	})
	require.NoError(t, err)

	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunneledServer.ID.String(), gatewayServer.URL, time.Hour))

	return publicTunnelFixture{
		endpointSlug: endpointSlug,
		tunnelID:     tunneledServer.ID,
		gateway:      gateway,
	}
}

// serveTunneledPublicRequest drives an anonymous request through ServePublic
// with an arbitrary method and optional Mcp-Session-Id.
func serveTunneledPublicRequest(t *testing.T, ti *testInstance, slug, method string, body []byte, sessionID string) (*httptest.ResponseRecorder, error) {
	t.Helper()

	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, "/mcp/"+slug, reader)
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	if err := ti.service.ServePublic(w, req); err != nil {
		return w, fmt.Errorf("serve public: %w", err)
	}
	return w, nil
}

// initializeTunneledPublicSession runs a successful anonymous initialize and
// returns the Gram-owned session id from the response.
func initializeTunneledPublicSession(t *testing.T, ti *testInstance, fixture publicTunnelFixture) string {
	t.Helper()

	w, err := serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeInitializeBody(), "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "initialize response: %s", w.Body.String())
	sid := w.Header().Get("Mcp-Session-Id")
	require.True(t, strings.HasPrefix(sid, "gsid_"), "expected Gram-owned session id, got %q", sid)
	return sid
}

// TestServePublic_Tunneled_AnonymousInitializeMintsGramSession: the core
// anonymous flow — initialize succeeds with no credentials, the response
// carries a Gram-owned session id (never the backend's), and the forward
// carried no Authorization header into the tunnel.
func TestServePublic_Tunneled_AnonymousInitializeMintsGramSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)

	sid := initializeTunneledPublicSession(t, ti, fixture)
	require.NotEqual(t, "backend-secret-session", sid)

	forwarded := gateway.lastForward()
	require.Empty(t, forwarded.Get("Authorization"), "Gram credentials must never reach the tunnel")
	require.Empty(t, forwarded.Get(wire.HeaderTunnelAgentSession), "initialize must not pin an exact target")
}

// TestServePublic_Tunneled_SessionRequestPinsExactTarget: a session-bearing
// request resolves the mapping, forwards the backend's own session id, and
// pins the exact agent session that served the initialize.
func TestServePublic_Tunneled_SessionRequestPinsExactTarget(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)

	sid := initializeTunneledPublicSession(t, ti, fixture)

	w, err := serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeToolsListBody(), sid)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "tools/list response: %s", w.Body.String())

	forwarded := gateway.lastForward()
	require.Equal(t, "backend-secret-session", forwarded.Get("Mcp-Session-Id"), "backend must see its own session id")
	require.Equal(t, "agent-1", forwarded.Get(wire.HeaderTunnelAgentSession), "session traffic must pin the exact agent session")
	require.NotEmpty(t, forwarded.Get(wire.HeaderTunnelConsumerSession))
}

// TestServePublic_Tunneled_UnknownOrMalformedSessionIs404: session ids that
// are not Gram-minted, or valid-shaped but unknown, must 404 so MCP clients
// re-initialize — and must never be forwarded into the tunnel.
func TestServePublic_Tunneled_UnknownOrMalformedSessionIs404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)

	for _, sid := range []string{
		"backend-secret-session",
		"gsid_00000000000000000000000000000000",
	} {
		_, err := serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeToolsListBody(), sid)
		require.Error(t, err)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeNotFound, oopsErr.Code, "session %q must 404", sid)
	}
	require.Zero(t, gateway.forwardCount(), "unknown sessions must not reach the tunnel")
}

// TestServePublic_Tunneled_DeadAgentSessionTranslatesTo404: when the pinned
// agent session is gone the gateway answers no-live-session; Gram must
// translate that to 404 and drop the mapping so the client re-initializes
// rather than seeing 502s forever.
func TestServePublic_Tunneled_DeadAgentSessionTranslatesTo404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)

	sid := initializeTunneledPublicSession(t, ti, fixture)

	gateway.mu.Lock()
	gateway.dead = true
	gateway.mu.Unlock()

	_, err := serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeToolsListBody(), sid)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)

	// The mapping is gone: the same sid now 404s before reaching the tunnel.
	before := gateway.forwardCount()
	_, err = serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeToolsListBody(), sid)
	require.Error(t, err)
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
	require.Equal(t, before, gateway.forwardCount(), "dropped session must not be re-forwarded")
}

// TestServePublic_Tunneled_DeleteTerminatesSession: DELETE forwards the
// backend session id and drops the mapping on success.
func TestServePublic_Tunneled_DeleteTerminatesSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)

	sid := initializeTunneledPublicSession(t, ti, fixture)

	w, err := serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodDelete, nil, sid)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "backend-secret-session", gateway.lastForward().Get("Mcp-Session-Id"))

	_, err = serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeToolsListBody(), sid)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

// TestServePublic_Tunneled_SessionlessBackendGetsNoSyntheticSession: a
// backend that returns no Mcp-Session-Id is sessionless per the MCP spec;
// Gram must not synthesize a session header the backend did not produce.
func TestServePublic_Tunneled_SessionlessBackendGetsNoSyntheticSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "", legacy: false, dead: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)

	w, err := serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeInitializeBody(), "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Header().Get("Mcp-Session-Id"))

	// Follow-up sessionless traffic still serves.
	w, err = serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeToolsListBody(), "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

// TestServePublic_Tunneled_LegacyGatewayFailsClosed: a gateway that does not
// report the agent session it served cannot support exact-target pinning; a
// session-bearing initialize must fail closed rather than mint an untrackable
// session.
func TestServePublic_Tunneled_LegacyGatewayFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: true, dead: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)

	_, err := serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeInitializeBody(), "")
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeGatewayError, oopsErr.Code)
}

// TestServePublic_Tunneled_StripsBackendChallenge: a WWW-Authenticate
// challenge from the customer's backend must never reach an anonymous caller
// — this endpoint has no authorization server to direct them to.
func TestServePublic_Tunneled_StripsBackendChallenge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, challenge: `Bearer resource_metadata="http://internal.example/.well-known"`}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)

	w, err := serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeInitializeBody(), "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
	require.Empty(t, w.Header().Get(wire.HeaderTunnelAgentSession), "internal tunnel headers must not leak")
}

// TestServePublic_Tunneled_LiveSessionCapRejectsInitialize: once the
// per-tunnel anonymous session cap is reached, further initializes are
// rejected before touching the customer's backend.
func TestServePublic_Tunneled_LiveSessionCapRejectsInitialize(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithTunnelPublicConfig(t, &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{
		SessionTTL:         0,
		LiveSessionCap:     1,
		InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		MaxRequestLifetime: 0,
	})
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)

	initializeTunneledPublicSession(t, ti, fixture)
	forwardsAfterFirst := gateway.forwardCount()

	w, err := serveTunneledPublicRequest(t, ti, fixture.endpointSlug, http.MethodPost, makeInitializeBody(), "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "JSON-RPC rejection envelope rides an HTTP 200")
	require.Contains(t, w.Body.String(), "anonymous session capacity")
	require.Equal(t, forwardsAfterFirst, gateway.forwardCount(), "capacity rejection must not reach the backend")
}

// TestServePublic_Tunneled_OAuthSurfaceIs404: the OAuth discovery and grant
// surface must not exist for anonymous public tunneled endpoints even though
// the issuer column is populated.
func TestServePublic_Tunneled_OAuthSurfaceIs404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, challenge: ""}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)

	logger := ti.logger
	mcpEndpoint, mcpServer, err := ti.service.ResolveMCPEndpointAndServer(ctx, logger, fixture.endpointSlug)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/"+fixture.endpointSlug, nil)
	w := httptest.NewRecorder()
	err = ti.service.ServeWellKnownProtectedResourceForServer(w, req, logger, mcpEndpoint, mcpServer, "mcp")
	requireNotFoundOops(t, err)

	req = httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+fixture.endpointSlug, nil)
	w = httptest.NewRecorder()
	err = ti.service.ServeWellKnownAuthorizationServerForServer(w, req, logger, mcpEndpoint, mcpServer, "mcp")
	requireNotFoundOops(t, err)

	// The issuer-gated grant handlers resolve through
	// LoadResolvedMcpEndpointBySlug, which must refuse the slug outright.
	_, err = ti.service.LoadResolvedMcpEndpointBySlug(ctx, logger, fixture.endpointSlug, "mcp")
	requireNotFoundOops(t, err)
}

func requireNotFoundOops(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

// TestServePublic_Tunneled_PrivateVisibilityUnaffected: a tunneled server
// with private visibility still requires authentication even when the source
// has allow_public set — consent alone must not open anything.
func TestServePublic_Tunneled_PrivateVisibilityUnaffected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tunneledID, err := uuid.NewV7()
	require.NoError(t, err)
	tunneledServer, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:        tunneledID,
		ProjectID: *authCtx.ProjectID,
		Name:      "private-tunnel-" + uuid.NewString()[:8],
		KeyHash:   uuid.NewString(),
		KeyPrefix: "gram_tunnel_test",
	})
	require.NoError(t, err)
	_, err = tunneledmcprepo.New(ti.conn).UpdateServer(ctx, tunneledmcprepo.UpdateServerParams{
		Name:        tunneledServer.Name,
		AllowPublic: pgtype.Bool{Bool: true, Valid: true},
		ID:          tunneledServer.ID,
		ProjectID:   *authCtx.ProjectID,
	})
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	mcpServer, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  id,
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText("private tunneled mcp server"),
		Slug:                conv.ToPGText("private-tunneled-" + uuid.NewString()[:8]),
		EnvironmentID:       uuid.NullUUID{},
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{},
		TunneledMcpServerID: uuid.NullUUID{UUID: tunneledServer.ID, Valid: true},
		ToolsetID:           uuid.NullUUID{},
		Visibility:          "private",
	})
	require.NoError(t, err)

	endpointSlug := "endpoint-" + uuid.NewString()
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{},
		McpServerID:    mcpServer.ID,
		Slug:           endpointSlug,
	})
	require.NoError(t, err)

	// Anonymous request against a private tunneled server: still challenged,
	// never anonymous.
	w, err := serveTunneledPublicRequest(t, ti, endpointSlug, http.MethodPost, makeInitializeBody(), "")
	if err == nil {
		require.Equal(t, http.StatusUnauthorized, w.Code)
	} else {
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	}
}
