package mcp_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func callerIssuerForTest(t *testing.T) (*mcpauthz.Issuer, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	issuer, err := mcpauthz.New(string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "https://gram.example", false)
	require.NoError(t, err)
	return issuer, &key.PublicKey
}

func TestPrivateTunnelConsentWorksWithStrictDiscoveryAssertion(t *testing.T) {
	t.Parallel()
	issuer, key := callerIssuerForTest(t)
	ctx, ti := newTestMCPServiceWithCallerAssertions(t, issuer)
	_, sessionIssuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	projectID := sessionIssuer.ProjectID.UUID
	slug := "assertion-consent-" + uuid.NewString()
	serverID, tunnelID := createPrivateTunneledServer(t, ctx, ti, projectID, sessionIssuer.ID, slug, "")
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{ProjectID: projectID, McpServerID: conv.ToNullUUID(serverID), Slug: slug})
	require.NoError(t, err)
	stateID, csrf := seedModernConsentChallenge(t, ctx, ti, sessionIssuer.ID, client, serverID, slug)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	// These fields are written only by the successful IDP callback.
	state.AuthorizerUserID = state.Subject.ID
	state.AuthorizerImpersonated = new(false)
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	seedMetaMemberConnectGrant(t, ctx, ti.conn, sessionIssuer.OrganizationID.String, serverID)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "x/mcp")
	require.NoError(t, err)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "test-agent", backendSessionID: "test-backend-session", mu: sync.Mutex{}}
	var accepted atomic.Int32
	var cleanups atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get(mcpauthz.Header)
		token, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithAudience(urn.NewTunneledMcpServer(tunnelID).String()), jwt.WithIssuer("https://gram.example"), jwt.WithExpirationRequired())
		if err != nil {
			http.Error(w, "assertion required", http.StatusUnauthorized)
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "invalid claims", http.StatusUnauthorized)
			return
		}
		if claims["purpose"] != "mcp_discovery" || claims["sub"] != "user:"+state.Subject.ID || claims["organization_id"] != endpoint.OrganizationID || claims["project_id"] != projectID.String() {
			http.Error(w, "wrong binding", http.StatusForbidden)
			return
		}
		// The destination enforces discovery's signed allowlist; tools/call cannot
		// become authorized merely by possessing a valid discovery signature.
		if r.Method == http.MethodPost {
			var rpc struct {
				Method string `json:"method"`
			}
			rawBody, _ := io.ReadAll(r.Body)
			body := strings.NewReader(string(rawBody))
			_ = json.Unmarshal(rawBody, &rpc)
			allowed := false
			methods, ok := claims["allowed_methods"].([]any)
			if !ok {
				http.Error(w, "invalid discovery scope", http.StatusForbidden)
				return
			}
			for _, method := range methods {
				allowed = allowed || method == rpc.Method
			}
			if !allowed {
				http.Error(w, "discovery cannot execute tools", http.StatusForbidden)
				return
			}
			r.Body = io.NopCloser(body)
		} else if r.Method != http.MethodDelete {
			http.Error(w, "invalid transport method", http.StatusForbidden)
			return
		}
		accepted.Add(1)
		if r.Method == http.MethodDelete {
			cleanups.Add(1)
		}
		w.Header().Set(mcpauthz.Header, raw) // An upstream echo must not reach the client.
		gateway.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))
	attempt := uuid.NewString()
	init := serveConsentMCPRequest(t, ctx, ti, endpoint, stateID, csrf, attempt, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, nil)
	require.Contains(t, init.Body.String(), "serverInfo")
	require.Empty(t, init.Header().Get(mcpauthz.Header))
	headers := map[string]string{"Mcp-Session-Id": init.Header().Get("Mcp-Session-Id"), mcpversions.HTTPHeader: "2025-06-18"}
	list := serveConsentMCPRequest(t, ctx, ti, endpoint, stateID, csrf, attempt, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, headers)
	require.Contains(t, list.Body.String(), "tools")
	require.EqualValues(t, 2, accepted.Load())
	req := httptest.NewRequest(http.MethodPost, "/x/mcp/"+slug+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"forbidden"}}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Consent-State", stateID)
	req.Header.Set("Gram-Consent-Csrf", csrf)
	req.Header.Set("Gram-Consent-Inventory-Attempt", attempt)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	err = ti.service.ServeConsentMCP(response, req, endpoint)
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "method is not available on the consent transport")
	require.EqualValues(t, 2, accepted.Load(), "a consent credential must never forward tools/call")
	// Manual connection verification exercises the SDK discovery, handshake,
	// list, and detached DELETE paths with the same live consent proof.
	remoteClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, endpoint.OrganizationID, "assertion-upstream", "", []uuid.UUID{sessionIssuer.ID})
	stampRemoteSessionIssuer(t, ctx, ti.conn, projectID, serverID, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, endpoint.OrganizationID, remoteClient)))
	insertQualifiedRemoteSessionToken(t, ctx, ti, sessionIssuer.ID, remoteClient, *state.Subject, "upstream-oauth", "")
	state.CSRFToken = "csrf-token"
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	fx := validationFixture{ti: ti, endpoint: endpoint, stateID: stateID, subject: *state.Subject, clientID: remoteClient, name: slug}
	_, err = postValidate(t, fx, remoteClient)
	require.NoError(t, err)
	require.Equal(t, "valid", storedSession(t, ctx, fx).ValidationStatus.String)
	require.EqualValues(t, 1, cleanups.Load(), "SDK session cleanup must retain discovery provenance")

}

func TestPrivateTunnelKeepaliveSkipsWithoutChangingVerdict(t *testing.T) {
	t.Parallel()
	issuer, _ := callerIssuerForTest(t)
	ctx, fx, tunnelID := seedTunneledRecheckFixture(t, "assertion-keepalive", "urn:example:private-mcp", issuer)
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "caller assertion required", http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)
	require.NoError(t, fx.ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))
	before := storedSession(t, ctx, fx)
	checked, err := fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Zero(t, checked)
	require.Zero(t, attempts.Load())
	after := storedSession(t, ctx, fx)
	require.Equal(t, before.ValidationStatus, after.ValidationStatus)
	require.Equal(t, before.LastValidatedAt, after.LastValidatedAt)
	require.True(t, after.LastRefreshAttemptAt.Valid, "the claim lease still paces skipped probes")
}

func TestPublicTunnelPinnedSessionNeverReceivesCallerAssertion(t *testing.T) {
	t.Parallel()
	issuer, _ := callerIssuerForTest(t)
	ctx, ti := newTestMCPServiceWithCallerAssertions(t, issuer)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "test-agent", backendSessionID: "backend-session", mu: sync.Mutex{}}
	fixture := newPublicTunnelFixture(t, ctx, ti, gateway, true)
	sid := initializeTunneledPublicSession(t, ti, fixture)
	request := newTunneledPublicRequest(fixture.endpointSlug, http.MethodPost, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), sid)
	request.Header.Set("SPEAKEASY_AUTHZ", "forged")
	request.Header.Set("Speakeasy-Authz", "forged-alias")
	response := httptest.NewRecorder()
	require.NoError(t, ti.service.ServePublic(response, request))
	require.Equal(t, http.StatusOK, response.Code)
	headers, _ := tunnelForwards(gateway)
	require.GreaterOrEqual(t, len(headers), 2)
	for _, header := range headers {
		require.Empty(t, header.Get(mcpauthz.Header))
		require.Empty(t, header.Get("Speakeasy-Authz"))
	}
}

func TestMetaDispatchAssertionBindsPrivateTunnelMember(t *testing.T) {
	t.Parallel()
	issuer, key := callerIssuerForTest(t)
	ctx, ti := newTestMCPServiceWithCallerAssertions(t, issuer)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *auth.ProjectID, auth.ActiveOrganizationID
	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "assertion-meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, slug, shared)
	memberID, tunnelID := createPrivateTunneledServer(t, ctx, ti, projectID, shared, "assertion-member", "")
	_, err := metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{ProjectID: projectID, MetaMcpServerID: meta.ID, McpServerID: memberID})
	require.NoError(t, err)
	seedMetaMemberConnectGrant(t, ctx, ti.conn, orgID, memberID)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "test-agent", backendSessionID: "backend-session", mu: sync.Mutex{}}
	upstream := httptest.NewServer(gateway)
	t.Cleanup(upstream.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))
	subject := urn.NewUserSubject(auth.UserID)
	bearer := mintMetaIssuerBearer(t, ti, slug, shared, subject)
	rpc := executeMetaTool(t, ti, slug, bearer, "assertion-member--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, text)
	require.Contains(t, text, "pong through the tunnel")
	headers, _ := tunnelForwards(gateway)
	require.GreaterOrEqual(t, len(headers), 3)
	for _, header := range headers {
		token, err := jwt.Parse(header.Get(mcpauthz.Header), func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithAudience(urn.NewTunneledMcpServer(tunnelID).String()), jwt.WithIssuer("https://gram.example"))
		require.NoError(t, err)
		claims, ok := token.Claims.(jwt.MapClaims)
		require.True(t, ok)
		require.Equal(t, memberID.String(), claims["mcp_server_id"])
		require.Equal(t, subject.String(), claims["sub"])
		require.Equal(t, "mcp_request", claims["purpose"])
	}
}
