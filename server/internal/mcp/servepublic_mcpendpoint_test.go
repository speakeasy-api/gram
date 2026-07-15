// servepublic_mcpendpoint_test.go verifies that /mcp/{mcpSlug} resolves
// through mcp_endpoints → mcp_servers first and falls back to the
// legacy toolsets.mcp_slug lookup on any not-found.
package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
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

// TestServePublic_McpEndpoint_PublicTunneledBacked_FailsClosed: a tunneled
// MCP server with public visibility only serves anonymously when the tunnel
// source's owner has set allow_public (double opt-in). This test seeds the
// non-consented state directly through the repo layer (the shape a manual SQL
// edit or future write path would produce) and asserts the serve path fails
// closed — as a 404, so unauthenticated callers cannot distinguish a gated
// endpoint from a missing one — rather than proxying into the tunnel.
func TestServePublic_McpEndpoint_PublicTunneledBacked_FailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	tunneledID, err := uuid.NewV7()
	require.NoError(t, err)
	tunneledServer, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:        tunneledID,
		ProjectID: *authCtx.ProjectID,
		Name:      "public-tunnel-attempt",
		KeyHash:   uuid.NewString(),
		KeyPrefix: "gram_tunnel_test",
	})
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	mcpServer, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  id,
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText("test tunneled mcp server"),
		Slug:                conv.ToPGText("test-tunneled-" + uuid.NewString()[:8]),
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

	_, err = servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), "", nil)
	require.Error(t, err, "public tunneled-backed endpoint must fail closed without owner consent")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
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

// mintIssuerBearerForEndpoint drives ServeToken with a synthesised
// UserSessionGrant and returns the minted JWT.
func mintIssuerBearerForEndpoint(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	slug string,
	mcpServer mcpserversrepo.McpServer,
	organizationID string,
) string {
	t.Helper()

	require.True(t, mcpServer.UserSessionIssuerID.Valid, "remote-backed seeds always carry an issuer")
	mcpEndpoint, err := mcpendpointsrepo.New(ti.conn).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           slug,
		CustomDomainID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	endpoint := mcp.NewResolvedMcpEndpointFromMcpServer(&mcpEndpoint, &mcpServer, organizationID)

	clientID := "test-client-" + uuid.NewString()
	redirectURI := "http://localhost:3000/callback"
	_, err = usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID: mcpServer.UserSessionIssuerID.UUID,
		ClientID:            clientID,
		ClientName:          "servepublic test client",
		RedirectUris:        []string{redirectURI},
	})
	require.NoError(t, err)

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code := "auth-code-" + uuid.NewString()
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
		Code:                code,
		UserSessionIssuerID: mcpServer.UserSessionIssuerID.UUID,
		UserSessionClientID: uuid.Nil,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
		Subject:             urn.NewAnonymousSubject(uuid.NewString()),
		CreatedAt:           time.Now(),
	}))

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/"+slug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeToken(w, req, endpoint))
	require.Equal(t, http.StatusOK, w.Code, "token endpoint should mint an access token: %s", w.Body.String())

	var resp struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.AccessToken)
	return resp.AccessToken
}

// TestServePublic_McpEndpoint_RemoteBacked_Proxies confirms /mcp/{slug}
// dispatches to the remote MCP proxy when the resolved mcp_server is
// backed by a remote_mcp_server. Remote-backed servers are issuer-gated,
// so the request authenticates with a minted user-session bearer; the
// proxy is exercised end-to-end: the test's httptest server captures the
// relayed initialize body.
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
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, endpointSlug, "public", issuerID)
	token := mintIssuerBearerForEndpoint(t, ctx, ti, endpointSlug, mcpServer, authCtx.ActiveOrganizationID)

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), token, nil)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("upstream not invoked within 5s; status=%d body=%s", w.Code, w.Body.String())
	}
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "upstream", "upstream initialize response must be relayed back")
}

// TestServePublic_McpEndpoint_PrivateRemoteBacked_NoAuth_Returns401
// confirms private-visibility remote-backed mcp_endpoints reject an
// unauthenticated request at the issuer gate, before the proxy fires.
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
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, endpointSlug, "private", issuerID)

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

// newStatelessRemoteMCPUpstream returns an httptest server that answers MCP
// JSON-RPC POSTs (initialize, tools/list, tools/call) with plain JSON,
// echoing the request id. It deliberately skips the streamable-HTTP session
// state machine (Mcp-Session-Id handshake, notifications/initialized) so a
// test can exercise the remote proxy's interceptors without driving a full
// MCP client handshake — mirroring the dumb upstream used by
// TestServePublic_McpEndpoint_RemoteBacked_Proxies. The single tool is named
// toolName.
func newStatelessRemoteMCPUpstream(t *testing.T, toolName string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &req)

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "upstream", "version": "1.0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        toolName,
				"description": "Returns pong",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			}}}
		case "tools/call":
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "pong"}}, "isError": false}
		default:
			result = map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// seedUserMCPConnectGrant writes a server-level mcp:connect grant on the
// user principal derived from a user-session JWT subject. A server-level
// selector (resource id, no tool dimension) admits any tool on the server,
// so it survives both the tools/list filter and the tools/call authz check.
func seedUserMCPConnectGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, userID, serverID string) {
	t.Helper()

	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, serverID).MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		Scope:          string(authz.ScopeMCPConnect),
		Effect:         pgtype.Text{},
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

// decodeMCPResult unmarshals a JSON-RPC response body, asserts it carries no
// error, and returns the result object.
func decodeMCPResult(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var resp struct {
		Result map[string]any  `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &resp), "response body: %s", string(body))
	require.Nil(t, resp.Error, "response must not surface a JSON-RPC error: %s", string(body))
	return resp.Result
}

// TestServePublic_McpEndpoint_IssuerGatedPrivateRemote_RBACEnforced_ResolvesGrants
// pins the AGE-2672 regression: an issuer-gated, private, remote-backed MCP
// endpoint served to an RBAC-enforced caller must prepare authz grants
// before the proxy's private-visibility mcp:connect interceptors run.
//
// Before the fix, serveRemoteBackend only called authz.PrepareContext on the
// non-issuer-gated path. For issuer-gated callers the proxy still attached
// the tools/list mcp:connect filter and the tools/call authz interceptor;
// with an enterprise org + session principal + RBAC enabled, those ran
// FindMatched / Require against a context with no prepared grants, returned
// ErrMissingGrants (mapped to CodeUnexpected), and the proxy substituted a
// JSON-RPC error event — yielding zero tools and a broken tools/call even
// though HTTP stayed 200.
//
// This is the first test to drive the full HTTP issuer-gated path with a
// real user-session JWT bearer. That bearer triggers RBAC enforcement (API
// keys, used by the engine-level RBAC tests in rpc_tools_list_test.go, bypass
// RBAC by design), which is precisely the condition the bug needed.
func TestServePublic_McpEndpoint_IssuerGatedPrivateRemote_RBACEnforced_ResolvesGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Mark the caller's org enterprise so authz.ShouldEnforce returns true
	// (enterprise + session principal + the test engine's always-on RBAC
	// flag). Without this the missing-grants path is dead — RBAC never
	// enforces and the bug cannot reproduce.
	require.NoError(t, orgsrepo.New(ti.conn).SetAccountType(ctx, orgsrepo.SetAccountTypeParams{
		GramAccountType: "enterprise",
		ID:              authCtx.ActiveOrganizationID,
	}))

	const toolName = "ping"
	upstream := newStatelessRemoteMCPUpstream(t, toolName)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpointSlug := "endpoint-" + uuid.NewString()
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, endpointSlug, "private", issuerID)

	// Grant the calling user a server-level mcp:connect grant. PrepareContext
	// derives the user principal from the JWT subject (an active org member)
	// and loads this grant; the interceptors then admit the tool. The grant
	// resource is the mcp_servers row id (NOT the remote_mcp_servers id) — the
	// resource the proxy's mcp:connect interceptors check (see ProxyManager.Build,
	// whose mcpServerID parameter is the RBAC ResourceID).
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockidp.MockUserID, mcpServer.ID.String())

	// Mint a user-session JWT for the issuer-gated endpoint. The audience is
	// the issuer URN (remote-backed endpoints bind the audience to the
	// issuer, not the backend id). Subject is the dev user, an active member
	// of the org, so PrepareContext can resolve principals.
	token, _, err := usersessions.NewSigner("test-jwt-secret").Mint(
		urn.NewUserSubject(mockidp.MockUserID),
		urn.NewUserSessionIssuer(issuerID).String(),
		ti.serverURL.String()+"/x/mcp/"+endpointSlug,
		time.Hour,
	)
	require.NoError(t, err)

	// A plain context (no session auth) so the only credential is the bearer
	// JWT, exactly as a real Remote MCP client would present it.
	initResp, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, initResp.Code, "initialize: %s", initResp.Body.String())

	listResp, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeToolsListBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listResp.Code, "tools/list: %s", listResp.Body.String())

	// Regression assertion: before the fix the mcp:connect filter ran
	// FindMatched against a context with no prepared grants, returned
	// ErrMissingGrants -> CodeUnexpected, and the proxy substituted a
	// JSON-RPC error event yielding zero tools. After the fix grants are
	// prepared and the granted tool survives the filter.
	listResult := decodeMCPResult(t, listResp.Body.Bytes())
	tools, ok := listResult["tools"].([]any)
	require.True(t, ok, "tools/list result must carry a tools array: %s", listResp.Body.String())
	require.Len(t, tools, 1, "the granted tool must survive the mcp:connect filter")

	// tools/call is gated by the same grants via ToolsCallAuthzInterceptor —
	// it must also succeed now that grants are prepared.
	callBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params":  map[string]any{"name": toolName, "arguments": map[string]any{}},
	})
	require.NoError(t, err)
	callResp, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, callBody, token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, callResp.Code, "tools/call: %s", callResp.Body.String())
	decodeMCPResult(t, callResp.Body.Bytes())
}

func TestServePublic_McpEndpoint_IssuerGatedPrivateRemote_RBACEnforced_RequiresConnectForInitialize(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	require.NoError(t, orgsrepo.New(ti.conn).SetAccountType(ctx, orgsrepo.SetAccountTypeParams{
		GramAccountType: "enterprise",
		ID:              authCtx.ActiveOrganizationID,
	}))

	upstreamHit := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHit <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpointSlug := "endpoint-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, endpointSlug, "private", issuerID)

	token, _, err := usersessions.NewSigner("test-jwt-secret").Mint(
		urn.NewUserSubject(mockidp.MockUserID),
		urn.NewUserSessionIssuer(issuerID).String(),
		ti.serverURL.String()+"/x/mcp/"+endpointSlug,
		time.Hour,
	)
	require.NoError(t, err)

	_, err = servePublicHTTP(t, context.Background(), ti, endpointSlug, makeInitializeBody(), token, nil)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	select {
	case <-upstreamHit:
		t.Fatal("upstream must not be reached without mcp:connect")
	default:
	}
}
