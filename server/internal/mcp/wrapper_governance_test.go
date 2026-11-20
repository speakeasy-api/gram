// wrapper_governance_test.go pins the AIS-633 contract: when a request
// resolves through an mcp_endpoints row to a toolset-backed mcp_servers row,
// hosting configuration (visibility, issuer gating, RBAC resource id) comes
// from the wrapper, and the toolset's own mcp_is_public /
// user_session_issuer_id columns are not consulted, and a bearer must carry
// the server's audience.
package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	goahttp "goa.design/goa/v3/http"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// collectCounterPoints returns one int64 counter's data points by attribute set.
func collectCounterPoints(t *testing.T, reader *sdkmetric.ManualReader, instrument string) map[attribute.Set]int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	points := map[attribute.Set]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != instrument {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				points[point.Attributes] = point.Value
			}
		}
	}
	return points
}

// A public wrapper over a private toolset serves anonymously: wrapper
// visibility wins and toolsets.mcp_is_public is not consulted. Before
// AIS-633 the same setup 401'd on the toolset's private flag.
func TestServePublic_WrapperGovernance_PublicWrapperOverPrivateToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPrivateMCPToolset(t, ctx, ti, "wg-private-ts-"+uuid.NewString()[:8])
	endpointSlug := "wg-endpoint-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, uuid.Nil)

	// Plain context: no session, no bearer — only a public server answers.
	w, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err, "wrapper visibility 'public' must govern; toolset privacy must not be consulted")
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.NotEmpty(t, w.Header().Get("Mcp-Session-Id"))
}

// A private wrapper over a public toolset requires identity auth: wrapper
// visibility wins in the restrictive direction too.
func TestServePublic_WrapperGovernance_PrivateWrapperOverPublicToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "wg-public-ts-"+uuid.NewString()[:8])
	endpointSlug := "wg-endpoint-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "private", uuid.NullUUID{}, uuid.Nil)

	_, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), "", nil)
	require.Error(t, err, "wrapper visibility 'private' must govern even though the toolset is public")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

// The issuer gate keys on the resolved wrapper's issuer: the canonical
// server's issuer must NOT gate a request resolved through another wrapper.
func TestServePublic_WrapperGovernance_ToolsetIssuerIgnoredWhenWrapperUngated(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "wg-ts-issuer-"+uuid.NewString()[:8])
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	toolset = setToolsetIssuer(t, ctx, ti.conn, toolset.ProjectID, toolset, issuerID)

	endpointSlug := "wg-endpoint-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, uuid.Nil)

	w, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err, "an ungated wrapper must serve without a challenge despite the toolset-side issuer")
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
}

// An issuer-gated toolset-backed wrapper challenges unauthenticated callers
// with the /mcp-surface resource metadata URL — the gate keys on the wrapper.
func TestServePublic_WrapperGovernance_WrapperIssuerGatesToolsetBacked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "wg-gated-"+uuid.NewString()[:8])
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpointSlug := "wg-endpoint-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, issuerID)

	w, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), "", nil)
	require.Error(t, err, "issuer-gated wrapper must reject unauthenticated requests")

	expected := `Bearer resource_metadata="` + ti.serverURL.String() + `/.well-known/oauth-protected-resource/mcp/` + endpointSlug + `"`
	require.Equal(t, expected, w.Header().Get("WWW-Authenticate"))
}

// mintWrapperUserBearer mints a user-session JWT bound to the wrapper's
// issuer-URN audience and persists its user_sessions row, mirroring what
// /token emits for a wrapper-resolved endpoint.
func mintWrapperUserBearer(t *testing.T, ti *testInstance, issuerID uuid.UUID, endpointSlug string, subject urn.SessionSubject) string {
	t.Helper()

	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  subject,
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + endpointSlug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, subject, jti)
	return token
}

// mcp:connect on a private toolset-backed wrapper keys on the mcp_servers id:
// a grant on the toolset id does not satisfy the check, and a grant on the
// wrapper id does.
func TestServePublic_WrapperGovernance_RBACKeysOnWrapperID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "wg-rbac-"+uuid.NewString()[:8])
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpointSlug := "wg-endpoint-" + uuid.NewString()
	mcpServer := createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "private", uuid.NullUUID{}, issuerID)

	subject := urn.NewUserSubject(mockidp.MockUserID)
	token := mintWrapperUserBearer(t, ti, issuerID, endpointSlug, subject)

	// A toolset-id grant is the wrong id space and must not admit the caller.
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockidp.MockUserID, toolset.ID.String())

	_, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), token, nil)
	require.Error(t, err, "a toolset-id grant must not satisfy the wrapper-keyed mcp:connect check")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "resource_id="+mcpServer.ID.String(), "denial must reference the wrapper id")

	// The wrapper-id grant admits the caller.
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockidp.MockUserID, mcpServer.ID.String())
	w, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
}

// A bearer minted against the toolset-URN audience is rejected on the
// toolset-backed server: bearers must carry the server's audience.
func TestServePublic_WrapperGovernance_ToolsetAudienceRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "wg-toolsetaud-"+uuid.NewString()[:8])
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpointSlug := "wg-endpoint-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, issuerID)

	subject := urn.NewAnonymousSubject(uuid.NewString())
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  subject,
		Audience: urn.NewToolset(toolset.ID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + endpointSlug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, subject, jti)

	_, err = servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), token, nil)
	require.Error(t, err, "a toolset-URN audience must not authenticate the toolset-backed server")
	require.Contains(t, err.Error(), "access token")
}

// A remote-backed endpoint rejects a toolset-URN-audience bearer too.
func TestServePublic_WrapperGovernance_ToolsetAudienceRejectedOffWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpointSlug := "wg-endpoint-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", endpointSlug, "public", issuerID)

	subject := urn.NewAnonymousSubject(uuid.NewString())
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  subject,
		Audience: urn.NewToolset(uuid.New()).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + endpointSlug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, subject, jti)

	_, err = servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), token, nil)
	require.Error(t, err, "a toolset-URN audience must not authenticate a non-toolset-backed endpoint")
}

// A disabled canonical server is not served at its old address, even with another enabled wrapper of the toolset.
func TestDisabledCanonicalServer_NotServed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "disabled-canonical-"+uuid.NewString()[:8])
	canonicalSlug := "canonical-" + uuid.NewString()
	exposeCanonicalWrapper(t, ctx, ti, toolset.ProjectID, toolset.ID, canonicalSlug, "disabled")
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, "alternate-"+uuid.NewString(), "public", uuid.NullUUID{}, uuid.Nil)
	_, err := servePublicHTTP(t, context.Background(), ti, canonicalSlug, makeInitializeBody(), "", nil)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeNotFound, shareable.Code)
	for _, handler := range []func(http.ResponseWriter, *http.Request) error{
		ti.service.HandleGetProtectedResource,
		ti.service.HandleGetAuthorizationServer,
	} {
		_, err := runMCPWellKnown(t, ctx, handler, canonicalSlug)
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeNotFound, shareable.Code)
	}
}

// A shared authorization server resolves a resource at a toolset-backed server's endpoint.
func TestSharedAuthorizationServer_ResolvesToolsetEndpointResource(t *testing.T) {
	t.Parallel()

	idpURL, err := url.Parse("https://idp.example.com/authorize")
	require.NoError(t, err)
	ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, testenv.NewLogger(t), testenv.NewMeterProvider(t),
		&mockIdentityResolver{buildAuthURLResult: idpURL, hasAccessOK: true},
		mcp.TunnelPublicConfig{
			SessionTTL:         0,
			LiveSessionCap:     0,
			InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
			RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
			MaxRequestLifetime: 0,
		}, nil)
	authCtx := requireProjectAuthContext(t, ctx)

	slug := "wg-shared-" + uuid.NewString()[:8]
	_, issuer := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	clientID := "wg-shared-client-" + uuid.NewString()[:8]
	insertUserSessionClient(t, ctx, ti.conn, issuer.ID, clientID)
	setIssuerMode(t, ctx, ti, issuer.ID, "shared", "")

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {"http://example.com/cb"},
		"code_challenge":        {pkceChallenge(pkceVerifier(t))},
		"code_challenge_method": {"S256"},
		"resource":              {ti.serverURL.JoinPath("mcp", slug).String()},
	}
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedAuthorize(w, sharedRequest(ctx, http.MethodGet, "/oauth/usi/"+issuer.ID.String()+"/authorize?"+q.Encode(), issuer.ID.String(), nil)))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
}

// An endpoint named like the organization gateway route keeps its own registration route.
func TestOrganizationGatewayRoutes_ShadowedEndpointRegisters(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, "gateway")
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)

	w := postRegistration(t, router, "/mcp/gateway/register")
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}
