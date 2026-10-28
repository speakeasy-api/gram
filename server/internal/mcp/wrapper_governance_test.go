// wrapper_governance_test.go pins the AIS-633 contract: when a request
// resolves through an mcp_endpoints row to a toolset-backed mcp_servers row,
// hosting configuration (visibility, issuer gating, RBAC resource id) comes
// from the wrapper, and the toolset's own mcp_is_public /
// user_session_issuer_id columns are not consulted. It also covers the legacy
// toolset-URN audience acceptance and the migration merge-gate counters.
package mcp_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	goahttp "goa.design/goa/v3/http"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
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

// A bearer minted against the legacy toolset-URN audience keeps validating on
// the toolset-backed wrapper, and every acceptance increments
// mcp.legacy_audience_accepted with the issuer dimension.
func TestServePublic_WrapperGovernance_LegacyToolsetAudienceAccepted(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "wg-legacyaud-"+uuid.NewString()[:8])
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpointSlug := "wg-endpoint-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, issuerID)

	// The pre-migration mint bound the audience to the toolset URN.
	subject := urn.NewAnonymousSubject(uuid.NewString())
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  subject,
		Audience: urn.NewToolset(toolset.ID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + endpointSlug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, subject, jti)

	w, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), token, nil)
	require.NoError(t, err, "legacy toolset-URN audience must stay valid on the toolset-backed wrapper")
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	points := collectCounterPoints(t, reader, mcpmetrics.InstrumentLegacyAudienceAccepted)
	require.Equal(t, int64(1), points[attribute.NewSet(attr.UserSessionIssuerID(issuerID.String()))],
		"acceptance must increment mcp.legacy_audience_accepted with the issuer dimension")
}

// The legacy audience is only accepted on toolset-backed wrappers: a
// remote-backed endpoint rejects a toolset-URN-audience bearer outright.
func TestServePublic_WrapperGovernance_LegacyAudienceRejectedOffWrapper(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

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

	points := collectCounterPoints(t, reader, mcpmetrics.InstrumentLegacyAudienceAccepted)
	require.Empty(t, points, "no acceptance may be recorded for a rejected bearer")
}

// Legacy toolsets.mcp_slug resolutions increment mcp.toolset_slug_fallback
// with the entry point as the dimension, and wrapper-resolved requests do
// not. Exercises the runtime (serve_public) and well-known
// (well_known_protected_resource) sites; the remaining sites share the same
// instrumented lookups.
func TestServePublic_WrapperGovernance_FallbackCounterByEntryPoint(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Legacy-only toolset: no mcp_endpoints row, resolves via mcp_slug.
	legacyToolset := createPublicMCPToolset(t, ctx, ti, authCtx, "wg-legacy-"+uuid.NewString()[:8])

	w, err := servePublicHTTP(t, ctx, ti, legacyToolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	// The well-known handler falls back on the same legacy slug. The toolset
	// carries no OAuth configuration so the handler ultimately 404s, but the
	// fallback resolution itself must still be counted.
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/"+legacyToolset.McpSlug.String, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", legacyToolset.McpSlug.String)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	_ = ti.service.HandleGetProtectedResource(httptest.NewRecorder(), req)

	// Wrapper-resolved request: must not count.
	wrappedToolset := createPublicMCPToolset(t, ctx, ti, authCtx, "wg-wrapped-"+uuid.NewString()[:8])
	endpointSlug := "wg-endpoint-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, wrappedToolset.ID, endpointSlug, "public", uuid.NullUUID{}, uuid.Nil)
	w2, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w2.Code, "body=%s", w2.Body.String())

	points := collectCounterPoints(t, reader, mcpmetrics.InstrumentToolsetSlugFallback)
	require.Equal(t, int64(1), points[attribute.NewSet(attr.McpEntryPoint(mcpmetrics.LegacyFallbackServePublic))])
	require.Equal(t, int64(1), points[attribute.NewSet(attr.McpEntryPoint(mcpmetrics.LegacyFallbackWellKnownProtectedResource))])
	var total int64
	for _, v := range points {
		total += v
	}
	require.Equal(t, int64(2), total, "wrapper-resolved requests must not increment the fallback counter")
}

// fallbackLogLines returns the decoded legacy-fallback attribution log lines.
func fallbackLogLines(t *testing.T, buf *syncBuffer) []map[string]any {
	t.Helper()

	var lines []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		if record["msg"] == "mcp request served via legacy toolset slug fallback" {
			lines = append(lines, record)
		}
	}
	return lines
}

// Every fallback hit logs who is still on it: slug, toolset, project, request
// host, canonical wrapper presence, and the toolset's custom domain state.
func TestServePublic_WrapperGovernance_FallbackAttributionLog(t *testing.T) {
	t.Parallel()

	buf := &syncBuffer{}
	ctx, ti := newTestMCPServiceWithLogger(t, slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	wrapped := createPublicMCPToolset(t, ctx, ti, authCtx, "wg-log-wrapped-"+uuid.NewString()[:8])
	exposeCanonicalWrapper(t, ctx, ti, wrapped.ProjectID, wrapped.ID, "wg-log-canonical-"+uuid.NewString(), "public")
	aliased, domain := createPublicMCPToolsetWithCustomDomain(t, ctx, ti, authCtx, "wg-log-alias-"+uuid.NewString()[:8], "wg-log-"+uuid.NewString()[:8]+".example.com")

	for _, slug := range []string{wrapped.McpSlug.String, aliased.McpSlug.String} {
		_, _ = servePublicHTTP(t, ctx, ti, slug, makeInitializeBody(), "", nil)
	}
	domainCtx := customdomains.WithContext(ctx, &customdomains.Context{OrganizationID: domain.OrganizationID, Domain: domain.Domain, DomainID: domain.ID})
	_, _ = servePublicHTTP(t, domainCtx, ti, aliased.McpSlug.String, makeInitializeBody(), "", nil)
	domains := customdomains_repo.New(ti.conn)
	_, err := domains.UpdateCustomDomain(ctx, customdomains_repo.UpdateCustomDomainParams{ID: domain.ID, Verified: true, Activated: false, IngressName: domain.IngressName, CertSecretName: domain.CertSecretName, ProvisionerKind: domain.ProvisionerKind})
	require.NoError(t, err)
	_, _ = servePublicHTTP(t, ctx, ti, aliased.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, domains.DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID))
	_, _ = servePublicHTTP(t, ctx, ti, aliased.McpSlug.String, makeInitializeBody(), "", nil)

	lines := fallbackLogLines(t, buf)
	require.Len(t, lines, 5)
	expect := []struct {
		toolset      toolsets_repo.Toolset
		wrapper      bool
		host         string
		customDomain string
	}{
		{wrapped, true, "platform", "none"},
		{aliased, true, "platform", "live"},
		{aliased, true, "custom_domain", "live"},
		{aliased, true, "platform", "inactive"},
		{aliased, true, "platform", "deleted"},
	}
	for i, want := range expect {
		got := lines[i]
		require.Equal(t, string(mcpmetrics.LegacyFallbackServePublic), got[string(attr.McpEntryPointKey)])
		require.Equal(t, want.toolset.McpSlug.String, got[string(attr.ToolsetMCPSlugKey)])
		require.Equal(t, want.toolset.ID.String(), got[string(attr.ToolsetIDKey)])
		require.Equal(t, want.toolset.ProjectID.String(), got[string(attr.ProjectIDKey)])
		require.Equal(t, want.host, got[string(attr.McpFallbackRequestHostKey)])
		require.Equal(t, want.wrapper, got[string(attr.McpFallbackCanonicalWrapperKey)])
		require.Equal(t, want.customDomain, got[string(attr.McpFallbackToolsetCustomDomainKey)])
	}
}

func TestWellKnown_FallbackAttributionLogsSlugOnce(t *testing.T) {
	t.Parallel()
	buf := &syncBuffer{}
	ctx, ti := newTestMCPServiceWithLogger(t, slog.New(slog.NewJSONHandler(buf, nil)))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "well-known-log-"+uuid.NewString()[:8])
	for _, handler := range []func(http.ResponseWriter, *http.Request) error{
		ti.service.HandleGetProtectedResource,
		ti.service.HandleGetAuthorizationServer,
	} {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/test/mcp/"+toolset.McpSlug.String, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
		req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
		// This ungated toolset has no OAuth metadata, but its fallback
		// resolution is still logged before that rejection.
		_ = handler(httptest.NewRecorder(), req)
	}
	require.Len(t, fallbackLogLines(t, buf), 2)
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if strings.Contains(line, "mcp request served via legacy toolset slug fallback") {
			require.Equal(t, 1, strings.Count(line, `"`+string(attr.ToolsetMCPSlugKey)+`":`), line)
		}
	}
}

func TestLegacyFallback_DisabledCanonicalWrapper(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "disabled-legacy-"+uuid.NewString()[:8])
	exposeCanonicalWrapper(t, ctx, ti, toolset.ProjectID, toolset.ID, "canonical-"+uuid.NewString(), "disabled")
	// Another enabled wrapper must not resurrect the canonical legacy route.
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, "alternate-"+uuid.NewString(), "public", uuid.NullUUID{}, uuid.Nil)
	_, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeNotFound, shareable.Code)
	for _, handler := range []func(http.ResponseWriter, *http.Request) error{
		ti.service.HandleGetProtectedResource,
		ti.service.HandleGetAuthorizationServer,
	} {
		_, err := runMCPWellKnown(t, ctx, handler, toolset.McpSlug.String)
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeNotFound, shareable.Code)
	}
}

// A shared authorization server resolving a resource through the legacy
// toolset slug is counted under its own entry point, not the oauth one.
func TestSharedAuthorizationServer_FallbackCountedAsSharedResource(t *testing.T) {
	t.Parallel()

	idpURL, err := url.Parse("https://idp.example.com/authorize")
	require.NoError(t, err)
	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, testenv.NewLogger(t), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
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

	points := collectCounterPoints(t, reader, mcpmetrics.InstrumentToolsetSlugFallback)
	require.Equal(t, int64(1), points[attribute.NewSet(attr.McpEntryPoint(mcpmetrics.LegacyFallbackSharedResource))])
	require.Zero(t, points[attribute.NewSet(attr.McpEntryPoint(mcpmetrics.LegacyFallbackOAuth))])
}

// The organization gateway's shadowed-endpoint probe counts under its own
// entry point; the endpoint route it then serves still counts as oauth.
func TestOrganizationGatewayRoutes_ShadowProbeFallbackCountedSeparately(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	authCtx := requireProjectAuthContext(t, ctx)
	createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, "gateway")
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)

	w := postRegistration(t, router, "/mcp/gateway/register")
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	points := collectCounterPoints(t, reader, mcpmetrics.InstrumentToolsetSlugFallback)
	require.Equal(t, int64(1), points[attribute.NewSet(attr.McpEntryPoint(mcpmetrics.LegacyFallbackOrgGatewayProbe))])
	require.Equal(t, int64(1), points[attribute.NewSet(attr.McpEntryPoint(mcpmetrics.LegacyFallbackOAuth))])
}
