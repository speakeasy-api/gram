package mcp_test

import (
	"bytes"
	"context"
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

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func callerIssuerForTest(t *testing.T) (*mcpauthz.Issuer, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	issuer, err := mcpauthz.New(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "https://gram.example", false)
	require.NoError(t, err)
	return issuer, &key.PublicKey
}

func TestPrivateTunnelAssertionAudienceTracksSavedResource(t *testing.T) {
	t.Parallel()
	issuer, key := callerIssuerForTest(t)
	ctx, ti := newTestMCPServiceWithCallerAssertions(t, issuer)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *auth.ProjectID
	sessionIssuer := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "assertion-direct-" + uuid.NewString()
	serverID, tunnelID := createPrivateTunneledServer(t, ctx, ti, projectID, sessionIssuer, slug, "")
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: conv.ToNullUUID(serverID), Slug: slug,
	})
	require.NoError(t, err)
	seedMetaMemberConnectGrant(t, ctx, ti.conn, auth.ActiveOrganizationID, serverID)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "test-agent", backendSessionID: "backend-session", mu: sync.Mutex{}}
	upstream := httptest.NewServer(gateway)
	t.Cleanup(upstream.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))
	subject := urn.NewUserSubject(auth.UserID)
	bearer := mintMetaIssuerBearer(t, ti, slug, sessionIssuer, subject)
	remoteClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, auth.ActiveOrganizationID, "assertion-routing", "", []uuid.UUID{sessionIssuer})
	stampRemoteSessionIssuer(t, ctx, ti.conn, projectID, serverID, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, auth.ActiveOrganizationID, remoteClient)))
	oldResource := "https://mcp.internal.example.com/a%2Fb?tenant=example/"
	exactResource := "https://mcp.internal.example.com/a%2Fb/?tenant=example/"
	seenIDs := map[string]bool{}
	for _, tc := range []struct{ resource, grantResource, authorization string }{
		{resource: oldResource, grantResource: oldResource, authorization: "Bearer upstream-oauth"},
		{resource: exactResource, grantResource: oldResource, authorization: ""},
		{resource: exactResource, grantResource: exactResource, authorization: "Bearer upstream-oauth"},
		{resource: "https://mcp.internal.example.com/another/", grantResource: exactResource, authorization: ""},
		{resource: "", grantResource: "", authorization: "Bearer upstream-oauth"},
	} {
		_, err := tunneledmcprepo.New(ti.conn).UpdateServer(ctx, tunneledmcprepo.UpdateServerParams{
			ID: tunnelID, ProjectID: projectID, ResourceIdentifier: conv.ToPGText(tc.resource),
		})
		require.NoError(t, err)
		insertQualifiedRemoteSessionToken(t, ctx, ti, sessionIssuer, remoteClient, subject, "upstream-oauth", tc.grantResource)
		request := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"?resource=https%3A%2F%2Fclient.example%2Fmcp", bytes.NewReader(makeInitializeBody()))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "Bearer "+bearer)
		request.Header.Set(mcpauthz.Header, "client-forged-assertion")
		request.Header.Set("Speakeasy-Authz", "client-forged-alias")
		request.Header.Set("X-Forwarded-Host", "client.example")
		route := chi.NewRouteContext()
		route.URLParams.Add("mcpSlug", slug)
		request = request.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, route))
		response := httptest.NewRecorder()
		require.NoError(t, ti.service.ServePublic(response, request))
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		headers, _ := tunnelForwards(gateway)
		forwarded := headers[len(headers)-1]
		require.Equal(t, tc.authorization, forwarded.Get("Authorization"), "resource=%q grant=%q", tc.resource, tc.grantResource)
		expected := conv.Default(tc.resource, urn.NewTunneledMcpServer(tunnelID).String())
		token, err := jwt.Parse(forwarded.Get(mcpauthz.Header), func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("https://gram.example"), jwt.WithAudience(expected), jwt.WithExpirationRequired())
		require.NoError(t, err)
		claims, ok := token.Claims.(jwt.MapClaims)
		require.True(t, ok)
		require.Equal(t, expected, claims["aud"])
		require.Equal(t, tunnelID.String(), claims["tunneled_mcp_server_id"])
		require.Equal(t, auth.ActiveOrganizationID, claims["organization_id"])
		require.Equal(t, projectID.String(), claims["project_id"])
		require.Empty(t, forwarded.Get("Speakeasy-Authz"))
		id, err := claims.GetSubject()
		require.NoError(t, err)
		require.Equal(t, "user:"+auth.UserID, id)
		jti, ok := claims["jti"].(string)
		require.True(t, ok)
		require.NotEmpty(t, jti)
		require.False(t, seenIDs[jti])
		seenIDs[jti] = true
	}
}

func TestPrivateTunnelConsentWorksWithStrictDiscoveryAssertion(t *testing.T) {
	t.Parallel()
	testPrivateTunnelConsentAssertion(t, "")
}

func TestPrivateTunnelConsentUsesConfiguredResourceAudience(t *testing.T) {
	t.Parallel()
	testPrivateTunnelConsentAssertion(t, "https://mcp.internal.example.com/a%2Fb/")
}

func testPrivateTunnelConsentAssertion(t *testing.T, resource string) {
	t.Helper()
	issuer, key := callerIssuerForTest(t)
	ctx, ti := newTestMCPServiceWithCallerAssertions(t, issuer)
	_, sessionIssuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	projectID := sessionIssuer.ProjectID.UUID
	slug := "assertion-consent-" + uuid.NewString()
	serverID, tunnelID := createPrivateTunneledServer(t, ctx, ti, projectID, sessionIssuer.ID, slug, resource)
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{ProjectID: projectID, McpServerID: conv.ToNullUUID(serverID), Slug: slug})
	require.NoError(t, err)
	audience := conv.Default(resource, urn.NewTunneledMcpServer(tunnelID).String())
	stateID, csrf := seedModernConsentChallenge(t, ctx, ti, sessionIssuer.ID, client, serverID, slug)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	profile, err := usersrepo.New(ti.conn).GetUser(ctx, state.Subject.ID)
	require.NoError(t, err)
	require.NotEmpty(t, profile.Email)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	inheritedAuth := *auth
	inheritedAuth.Email = new("untrusted-context@example.test")
	require.NotEqual(t, profile.Email, *inheritedAuth.Email)
	ctx = contextvalues.SetAuthContext(ctx, &inheritedAuth)
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
		token, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithAudience(audience), jwt.WithIssuer("https://gram.example"), jwt.WithExpirationRequired())
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
		if claims["email"] != profile.Email {
			http.Error(w, "trusted profile email required", http.StatusForbidden)
			return
		}
		if _, present := claims["email_verified"]; present {
			http.Error(w, "email verification is not established", http.StatusForbidden)
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
	insertQualifiedRemoteSessionToken(t, ctx, ti, sessionIssuer.ID, remoteClient, *state.Subject, "upstream-oauth", resource)
	state.CSRFToken = "csrf-token"
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	fx := validationFixture{ti: ti, endpoint: endpoint, stateID: stateID, subject: *state.Subject, clientID: remoteClient, name: slug}
	_, err = postValidate(t, fx, remoteClient)
	require.NoError(t, err)
	require.Equal(t, "valid", storedSession(t, ctx, fx).ValidationStatus.String)
	require.EqualValues(t, 1, cleanups.Load(), "SDK session cleanup must retain discovery provenance")

}

func TestPrivateTunnelKeepaliveProbesWithoutCallerAssertion(t *testing.T) {
	t.Parallel()
	issuer, _ := callerIssuerForTest(t)
	ctx, fx, tunnelID := seedTunneledRecheckFixture(t, "assertion-keepalive", "urn:example:private-mcp", issuer)
	_, err := usersrepo.New(fx.ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID: fx.subject.ID, Email: "caller@example.test", DisplayName: "Test caller",
	})
	require.NoError(t, err)
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "test-agent", backendSessionID: "backend-secret-session", mu: sync.Mutex{}}
	upstream := httptest.NewServer(gateway)
	t.Cleanup(upstream.Close)
	require.NoError(t, fx.ti.tunnelRoutes.Publish(ctx, tunnelID.String(), upstream.URL, time.Hour))

	checked, err := fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, checked)
	headers, bodies := tunnelForwards(gateway)
	requireTunnelProbe(t, headers, bodies, "token-assertion-keepalive")
	for _, header := range headers {
		require.Empty(t, header.Get(mcpauthz.Header), "a background probe has no authenticated caller")
	}
	after := storedSession(t, ctx, fx)
	require.Equal(t, "valid", after.ValidationStatus.String)
	require.True(t, after.LastValidatedAt.Valid)
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
	profile, err := usersrepo.New(ti.conn).GetUser(ctx, auth.UserID)
	require.NoError(t, err)
	require.NotEmpty(t, profile.Email)
	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "assertion-meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, slug, shared)
	resource := "https://mcp.internal.example.com/member/"
	memberID, tunnelID := createPrivateTunneledServer(t, ctx, ti, projectID, shared, "assertion-member", resource)
	_, err = metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{ProjectID: projectID, MetaMcpServerID: meta.ID, McpServerID: memberID})
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
		token, err := jwt.Parse(header.Get(mcpauthz.Header), func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithAudience(resource), jwt.WithIssuer("https://gram.example"))
		require.NoError(t, err)
		claims, ok := token.Claims.(jwt.MapClaims)
		require.True(t, ok)
		require.Equal(t, memberID.String(), claims["mcp_server_id"])
		require.Equal(t, subject.String(), claims["sub"])
		require.Equal(t, "mcp_request", claims["purpose"])
		require.Equal(t, profile.Email, claims["email"])
		require.NotContains(t, claims, "email_verified")
	}
}

func TestCallerProfileLookupFailureIsOperational(t *testing.T) {
	t.Parallel()
	issuer, _ := callerIssuerForTest(t)
	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithPoolConfigAndTemporal(t, testenv.NewLogger(t), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{}, nil, nil, false, mcp.MetaRuntimeConfig{}, testenv.NewTracerProvider(t), nil, issuer)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	shared := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "assertion-profile-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, shared)
	// The bearer is valid, but its human profile cannot be loaded.
	bearer := mintMetaIssuerBearer(t, ti, slug, shared, urn.NewUserSubject("missing-profile"))
	response, err := servePublicHTTP(t, ctx, ti, slug, makeInitializeBody(), bearer, nil)
	require.Error(t, err)
	require.NotContains(t, response.Header().Get("WWW-Authenticate"), "invalid_token")
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &metrics))
	points := map[attribute.Set]int64{}
	for _, scope := range metrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != mcpmetrics.InstrumentMCPRequestRejected {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				points[point.Attributes] = point.Value
			}
		}
	}
	require.Len(t, points, 1)
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.OAuthFailureReason("caller_profile_unavailable"),
		attr.McpURL("/mcp/"+slug),
		attr.McpSurface(string(mcpmetrics.SurfaceMeta)),
		attr.NetworkSurface(mcpmetrics.NetworkSurfacePublic),
	)])
}
