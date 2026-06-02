// servepublic_mcpendpoint_test.go verifies that /mcp/{mcpSlug} resolves
// through mcp_endpoints → mcp_servers first and falls back to the
// legacy toolsets.mcp_slug lookup on any not-found.
package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// ---------------------------------------------------------------------------
// Seed helpers — mirror the production wiring of an mcp_endpoint pointing at
// an mcp_server pointing at a backend (toolset or remote).
// ---------------------------------------------------------------------------

// createToolsetMcpEndpoint writes the mcp_servers row pointing at a
// toolset and the mcp_endpoints row exposing it under slug. visibility
// must be "public", "private", or "disabled". customDomainID may be
// invalid (Valid=false) for a platform-scoped endpoint. issuerID, when
// non-Nil, sets the mcp_servers.user_session_issuer_id FK to gate the
// endpoint.
func createToolsetMcpEndpoint(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	projectID uuid.UUID,
	toolsetID uuid.UUID,
	slug, visibility string,
	customDomainID uuid.NullUUID,
	issuerID uuid.UUID,
) mcpserversrepo.McpServer {
	t.Helper()
	id, err := uuid.NewV7()
	require.NoError(t, err)

	var issuer uuid.NullUUID
	if issuerID != uuid.Nil {
		issuer = uuid.NullUUID{UUID: issuerID, Valid: true}
	}

	mcpServer, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  id,
		ProjectID:           projectID,
		Name:                conv.ToPGText("test mcp server"),
		Slug:                conv.ToPGText("test-mcp-server-" + uuid.NewString()[:8]),
		EnvironmentID:       uuid.NullUUID{},
		UserSessionIssuerID: issuer,
		RemoteMcpServerID:   uuid.NullUUID{},
		ToolsetID:           uuid.NullUUID{UUID: toolsetID, Valid: true},
		Visibility:          visibility,
	})
	require.NoError(t, err)

	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: customDomainID,
		McpServerID:    mcpServer.ID,
		Slug:           slug,
	})
	require.NoError(t, err)
	return mcpServer
}

// createRemoteMcpEndpoint writes a remote_mcp_servers row pointing at
// upstreamURL, an mcp_servers row pointing at it, and an mcp_endpoints
// row exposing it under slug. issuerID, when non-Nil, sets the
// mcp_servers.user_session_issuer_id FK to gate the endpoint.
func createRemoteMcpEndpoint(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	projectID uuid.UUID,
	upstreamURL, slug, visibility string,
	issuerID uuid.UUID,
) (mcpserversrepo.McpServer, remotemcprepo.RemoteMcpServer) {
	t.Helper()

	remoteServer := remotemcptest.SeedServer(t, ctx, conn, remotemcprepo.CreateServerParams{
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           upstreamURL,
	})

	var issuer uuid.NullUUID
	if issuerID != uuid.Nil {
		issuer = uuid.NullUUID{UUID: issuerID, Valid: true}
	}

	id, err := uuid.NewV7()
	require.NoError(t, err)
	mcpServer, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  id,
		ProjectID:           projectID,
		Name:                conv.ToPGText("test mcp server"),
		Slug:                conv.ToPGText("test-mcp-server-" + uuid.NewString()[:8]),
		EnvironmentID:       uuid.NullUUID{},
		UserSessionIssuerID: issuer,
		RemoteMcpServerID:   uuid.NullUUID{UUID: remoteServer.ID, Valid: true},
		ToolsetID:           uuid.NullUUID{},
		Visibility:          visibility,
	})
	require.NoError(t, err)

	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: uuid.NullUUID{},
		McpServerID:    mcpServer.ID,
		Slug:           slug,
	})
	require.NoError(t, err)
	return mcpServer, remoteServer
}

// createUserSessionIssuer inserts a user_session_issuers row in the
// given project and returns its id. Mirrors the seed pattern used by
// the xmcp issuer-gated tests so the resulting issuer is structurally
// identical to what the management API would produce.
func createUserSessionIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               "issuer-" + uuid.NewString(),
		AuthnChallengeMode: "chain",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestServePublic_McpEndpoint_ToolsetBacked_ResolvesViaEndpoints
// confirms /mcp/{slug} resolves via the new mcp_endpoints lookup when
// the endpoint is backed by a toolset, dispatching through
// ServeToolsetResolved just like the legacy mcp_slug path.
func TestServePublic_McpEndpoint_ToolsetBacked_ResolvesViaEndpoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Toolset's mcp_slug is intentionally different from the endpoint
	// slug — confirms resolution flows through mcp_endpoints, not the
	// legacy toolset.mcp_slug query.
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "toolset-slug-"+uuid.NewString()[:8])

	endpointSlug := "endpoint-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, uuid.Nil)

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.NotEmpty(t, w.Header().Get("Mcp-Session-Id"))
}

// TestServePublic_NoMcpEndpoint_FallsBackToLegacyToolset confirms that
// when no mcp_endpoint matches the slug, /mcp/{slug} falls back to the
// legacy toolsets.mcp_slug lookup so existing customers without
// mcp_endpoints rows keep working unchanged.
func TestServePublic_NoMcpEndpoint_FallsBackToLegacyToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// No mcp_endpoints row is created — the slug is only addressable
	// via the legacy toolsets.mcp_slug path.
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "legacy-only-"+uuid.NewString()[:8])

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
}

// TestServePublic_McpEndpoint_RemoteBacked_Proxies confirms /mcp/{slug}
// dispatches to the remote MCP proxy when the resolved mcp_server is
// backed by a remote_mcp_server. The proxy is exercised end-to-end:
// the test's httptest server captures the relayed initialize body.
func TestServePublic_McpEndpoint_RemoteBacked_Proxies(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"upstream","version":"1.0"}}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	endpointSlug := "endpoint-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, endpointSlug, "public", uuid.Nil)

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), "", nil)
	<-done
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "upstream", "upstream initialize response must be relayed back")
}

// TestServePublic_McpEndpoint_PrivateRemoteBacked_NoAuth_Returns401
// confirms private-visibility remote-backed mcp_endpoints route through
// the identity-auth check in serveRemoteBackend. An unauthenticated
// request must be rejected before the proxy fires.
func TestServePublic_McpEndpoint_PrivateRemoteBacked_NoAuth_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstreamHit := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHit <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	endpointSlug := "endpoint-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, endpointSlug, "private", uuid.Nil)

	// Plain context (no auth context with active org) so the identity
	// chain in serveRemoteBackend has no credential to validate.
	_, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), "", nil)
	require.Error(t, err, "private remote-backed endpoint must reject unauthenticated requests")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	select {
	case <-upstreamHit:
		t.Fatal("upstream must not be reached when identity auth fails")
	default:
	}
}

// TestServePublic_McpEndpoint_IssuerGated_NoAuth_EmitsChallenge
// verifies the issuer-gated branch of /mcp/{slug} (mcp_server with
// user_session_issuer_id set): an unauthenticated request receives 401
// + a WWW-Authenticate header whose resource_metadata URL points at
// /.well-known/oauth-protected-resource/mcp/<slug>. The /mcp prefix
// (not /x/mcp) is the load-bearing part — mcpRouteBase must propagate
// through ApplyIssuerGate as "mcp" for requests arriving via ServePublic.
func TestServePublic_McpEndpoint_IssuerGated_NoAuth_EmitsChallenge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpointSlug := "endpoint-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", endpointSlug, "public", issuerID)

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), "", nil)
	require.Error(t, err, "issuer-gated endpoint must reject unauthenticated requests")

	wwwAuth := w.Header().Get("WWW-Authenticate")
	require.NotEmpty(t, wwwAuth, "challenge must include WWW-Authenticate")
	expected := `Bearer resource_metadata="` + ti.serverURL.String() + `/.well-known/oauth-protected-resource/mcp/` + endpointSlug + `"`
	require.Equal(t, expected, wwwAuth, "mcpRouteBase must be 'mcp' (not 'x/mcp') for /mcp callers")
}

// TestServePublic_McpEndpoint_DisabledMcpServer_FallsBackToLegacyToolset
// verifies the ServePublic doc contract that a disabled mcp_server
// surfaces as CodeNotFound from ResolveMCPEndpointAndServer and falls
// through to the legacy toolsets.mcp_slug lookup. A toolset with
// mcp_slug equal to the endpoint slug acts as the legacy-path target
// the fallback must find.
func TestServePublic_McpEndpoint_DisabledMcpServer_FallsBackToLegacyToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// A separate (disabled) toolset is the mcp_endpoint's backend; the
	// fallback target is a different public toolset sharing the slug
	// via createPublicMCPToolset (which sets both Slug and McpSlug to
	// the same value). The mcp_endpoint resolution finds the disabled
	// server (returns CodeNotFound); the fallback's GetToolsetByMcpSlug
	// finds the public legacy toolset.
	sharedSlug := "shared-" + uuid.NewString()[:8]
	disabledToolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "disabled-"+uuid.NewString()[:8])
	createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, sharedSlug)

	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, disabledToolset.ID, sharedSlug, "disabled", uuid.NullUUID{}, uuid.Nil)

	w, err := servePublicHTTP(t, ctx, ti, sharedSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err, "disabled mcp_server must fall through to the legacy toolset lookup")
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
}

// TestServePublic_PlatformDomain_DoesNotResolveCustomDomainEndpoint
// regression-guards the platform-only resolution semantic: a
// custom-domain-scoped mcp_endpoint must not resolve for a request
// arriving on the platform domain, even when the slug matches and no
// legacy toolset claims the slug. Asymmetric to the toolsets fallback
// path, which DOES allow platform requests to resolve custom-domain
// toolsets (see loadToolset docstring).
func TestServePublic_PlatformDomain_DoesNotResolveCustomDomainEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	customDomain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "custom.example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	_, err = customdomainsrepo.New(ti.conn).UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{
		ID:             customDomain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "custom-only-"+uuid.NewString()[:8])
	endpointSlug := "endpoint-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{UUID: customDomain.ID, Valid: true}, uuid.Nil)

	// Request arrives on the platform domain (no customdomains.Context).
	// The custom-domain mcp_endpoint must not resolve, and the legacy
	// toolset lookup must not find the slug either (toolset.mcp_slug
	// differs from endpointSlug). Expect CodeNotFound from ServePublic.
	_, err = servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), "", nil)
	require.Error(t, err, "platform-domain request must not resolve a custom-domain mcp_endpoint")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)

	// Same request, this time with the custom-domain context bound,
	// resolves and succeeds — confirms the test isn't passing by accident.
	ctxWithDomain := customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         customDomain.Domain,
		DomainID:       customDomain.ID,
	})
	w2, err2 := servePublicHTTP(t, ctxWithDomain, ti, endpointSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err2)
	require.Equal(t, http.StatusOK, w2.Code, "custom-domain-scoped request must resolve the endpoint; body=%s", w2.Body.String())
}
