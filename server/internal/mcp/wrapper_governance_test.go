// wrapper_governance_test.go pins the AIS-633 contract: when a request
// resolves through an mcp_endpoints row to a toolset-backed mcp_servers row,
// hosting configuration (visibility, issuer gating, RBAC resource id) comes
// from the wrapper, and the toolset's own mcp_is_public /
// user_session_issuer_id columns are not consulted. It also covers the legacy
// toolset-URN audience acceptance and the migration merge-gate counters.
package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "wg-public-ts-"+uuid.NewString()[:8])
	endpointSlug := "wg-endpoint-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "private", uuid.NullUUID{}, uuid.Nil)

	_, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), "", nil)
	require.Error(t, err, "wrapper visibility 'private' must govern even though the toolset is public")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

// The issuer gate keys on mcp_servers.user_session_issuer_id: a toolset-side
// issuer with no wrapper issuer must NOT gate a wrapper-resolved request.
// Before AIS-633 the in-toolset gate ran off the toolset column and 401'd.
func TestServePublic_WrapperGovernance_ToolsetIssuerIgnoredWhenWrapperUngated(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "wg-ts-issuer-"+uuid.NewString()[:8])
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	_, err := toolsetsRepo.UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           toolset.ProjectID,
	})
	require.NoError(t, err)

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

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "wg-gated-"+uuid.NewString()[:8])
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

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "wg-rbac-"+uuid.NewString()[:8])
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

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "wg-legacyaud-"+uuid.NewString()[:8])
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
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Legacy-only toolset: no mcp_endpoints row, resolves via mcp_slug.
	legacyToolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "wg-legacy-"+uuid.NewString()[:8])

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
	wrappedToolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "wg-wrapped-"+uuid.NewString()[:8])
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
