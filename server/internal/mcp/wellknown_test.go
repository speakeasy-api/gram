// wellknown_test.go covers the /.well-known/.../mcp/{slug} OAuth metadata
// handlers (HandleGetAuthorizationServer, HandleGetProtectedResource)
// end-to-end. Resolution tries mcp_endpoints → mcp_servers first and falls
// back to the legacy toolsets.mcp_slug lookup, so these tests exercise the
// full per-backend dispatch matrix on the resolved path (remote / toolset /
// issuer-gated) plus the legacy slug fallback — mirroring the /x/mcp
// coverage in xmcp/wellknown_test.go, but with metadata URLs rooted at
// /mcp/{slug}.
//
// The low-level response writers these handlers share
// (writeOAuth{Server,ProtectedResource}MetadataResponse) are unit-tested
// separately in wellknown_oauth_test.go, which is white-box (package mcp)
// because those helpers are unexported.
package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// runMCPWellKnown invokes a /mcp well-known handler with the chi `mcpSlug`
// URL param set to slug. Passing slug="" exercises the missing-slug branch
// (chi.URLParam returns "" for both missing and empty params, matching
// production routing).
func runMCPWellKnown(
	t *testing.T,
	ctx context.Context,
	handler func(http.ResponseWriter, *http.Request) error,
	slug string,
) (*httptest.ResponseRecorder, error) {
	t.Helper()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/.well-known/oauth/mcp/"+slug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := handler(w, req)
	return w, err
}

// ---------------------------------------------------------------------------
// HandleGetAuthorizationServer
// ---------------------------------------------------------------------------

func TestHandleGetAuthorizationServer_MissingSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp slug must be provided")
	require.Empty(t, w.Body.String())
}

func TestHandleGetAuthorizationServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, "definitely-missing-"+uuid.NewString()[:8])
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp server not found")
	require.Empty(t, w.Body.String())
}

func TestHandleGetAuthorizationServer_DisabledServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "disabled-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "disabled", uuid.Nil)

	// Disabled mcp_server resolves as not-found; with no legacy toolset for
	// the slug the fallback also misses → 404.
	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.Error(t, err)
	require.Empty(t, w.Body.String())
}

func TestHandleGetAuthorizationServer_RemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Remote-backed mcp_servers without issuer gating have no Gram-hosted
	// authorization server — the upstream publishes its own .well-known.
	slug := "remote-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", uuid.Nil)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no OAuth configuration found")
	require.Empty(t, w.Body.String())
}

func TestHandleGetAuthorizationServer_ToolsetBackendWithoutOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "ts-noauth-"+uuid.NewString()[:8])
	slug := "ep-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, uuid.Nil)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no OAuth configuration found")
	require.Empty(t, w.Body.String())
}

func TestHandleGetAuthorizationServer_ToolsetBackendWithProxy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	proxy := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "mcp-srv-proxy",
		IsPublic:     true,
		ProviderType: "",
	})
	slug := proxy.Toolset.McpSlug.String
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, proxy.Toolset.ID, slug, "public", uuid.NullUUID{}, uuid.Nil)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	// Legacy proxy metadata is keyed on the toolset's mcp_slug under
	// /oauth/{slug}, not the /mcp/{slug} surface.
	expectedIssuer := "http://0.0.0.0/oauth/" + slug
	require.Equal(t, expectedIssuer, metadata["issuer"])
	require.Equal(t, expectedIssuer+"/authorize", metadata["authorization_endpoint"])
	require.Equal(t, expectedIssuer+"/token", metadata["token_endpoint"])
	require.Equal(t, expectedIssuer+"/register", metadata["registration_endpoint"])

	scopes, ok := metadata["scopes_supported"].([]any)
	require.True(t, ok)
	require.Contains(t, scopes, "offline_access")
}

func TestHandleGetAuthorizationServer_ToolsetBackendWithExternalOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	external := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "mcp-srv-external",
		IsPublic: true,
		Metadata: nil,
	})
	slug := external.Toolset.McpSlug.String
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, external.Toolset.ID, slug, "public", uuid.NullUUID{}, uuid.Nil)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")

	// External OAuth toolsets surface the upstream provider's metadata verbatim.
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, "https://test-oauth-server.example.com", metadata["issuer"])
}

// TestHandleGetAuthorizationServer_IssuerGatedRemoteBackend is the primary
// regression for AGE-2624: an issuer-gated remote-backed mcp_server
// addressed at /mcp/{slug} now serves Gram-hosted RFC 8414 metadata
// (previously 404). The advertised issuer + endpoint URLs are rooted at
// /mcp/{slug}, matching the resource_metadata URL ServePublic advertises.
func TestHandleGetAuthorizationServer_IssuerGatedRemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "issuer-remote-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", issuerID)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedIssuer := "http://0.0.0.0/mcp/" + slug
	require.Equal(t, expectedIssuer, metadata["issuer"])
	require.Equal(t, expectedIssuer+"/authorize", metadata["authorization_endpoint"])
	require.Equal(t, expectedIssuer+"/token", metadata["token_endpoint"])
	require.Equal(t, expectedIssuer+"/register", metadata["registration_endpoint"])
	require.Equal(t, expectedIssuer+"/revoke", metadata["revocation_endpoint"])
}

// TestHandleGetAuthorizationServer_IssuerGatedToolsetBackend is the
// toolset companion of the remote-backed test. The dispatch branches on
// backend after the issuer check, so both backends need coverage.
func TestHandleGetAuthorizationServer_IssuerGatedToolsetBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "ts-issuer-"+uuid.NewString()[:8])
	slug := "issuer-toolset-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, issuerID)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedIssuer := "http://0.0.0.0/mcp/" + slug
	require.Equal(t, expectedIssuer, metadata["issuer"])
	require.Equal(t, expectedIssuer+"/authorize", metadata["authorization_endpoint"])
}

// TestHandleGetAuthorizationServer_IssuerGatedRemoteBackend_DanglingIssuerFK
// covers the race where the user_session_issuer FK target is deleted
// between mcp_server resolution and metadata emission.
func TestHandleGetAuthorizationServer_IssuerGatedRemoteBackend_DanglingIssuerFK(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "dangling-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", issuerID)

	// Sanity check: resolves cleanly before deletion.
	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	_, err = usersessionsrepo.New(ti.conn).DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{
		ID:        issuerID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	w, err = runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.Error(t, err, "dangling issuer FK must surface as a request-level error")
	require.Contains(t, err.Error(), "user_session_issuer not found")
	require.Empty(t, w.Body.String())
}

// TestHandleGetAuthorizationServer_LegacySlugFallbackProxy confirms a
// toolset with no mcp_endpoint row (pre the toolsets→mcp_servers
// migration) still resolves via the legacy toolsets.mcp_slug fallback.
func TestHandleGetAuthorizationServer_LegacySlugFallbackProxy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// No mcp_endpoint is created — the slug is only addressable via the
	// legacy toolsets.mcp_slug path.
	proxy := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "mcp-legacy-proxy",
		IsPublic:     true,
		ProviderType: "",
	})
	slug := proxy.Toolset.McpSlug.String

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, "http://0.0.0.0/oauth/"+slug, metadata["issuer"])
}

// ---------------------------------------------------------------------------
// HandleGetProtectedResource
// ---------------------------------------------------------------------------

func TestHandleGetProtectedResource_MissingSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp slug must be provided")
	require.Empty(t, w.Body.String())
}

func TestHandleGetProtectedResource_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, "definitely-missing-"+uuid.NewString()[:8])
	require.Error(t, err)
	require.Contains(t, err.Error(), "mcp server not found")
	require.Empty(t, w.Body.String())
}

func TestHandleGetProtectedResource_RemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "remote-pr-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", uuid.Nil)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, slug)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no OAuth configuration found")
	require.Empty(t, w.Body.String())
}

func TestHandleGetProtectedResource_ToolsetBackendWithoutOAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "ts-prnoauth-"+uuid.NewString()[:8])
	slug := "ep-pr-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, uuid.Nil)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, slug)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no OAuth configuration found")
	require.Empty(t, w.Body.String())
}

func TestHandleGetProtectedResource_ToolsetBackendWithProxy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	proxy := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "mcp-pr-proxy",
		IsPublic:     true,
		ProviderType: "",
	})
	slug := proxy.Toolset.McpSlug.String
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, proxy.Toolset.ID, slug, "public", uuid.NullUUID{}, uuid.Nil)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedResource := "http://0.0.0.0/mcp/" + slug
	require.Equal(t, expectedResource, metadata["resource"])
	authServers, ok := metadata["authorization_servers"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{expectedResource}, authServers)
}

// TestHandleGetProtectedResource_IssuerGatedRemoteBackend is the
// protected-resource companion of the AGE-2624 regression: an issuer-gated
// remote-backed mcp_server at /mcp/{slug} serves RFC 9728 metadata whose
// resource + authorization_servers point back at /mcp/{slug}.
func TestHandleGetProtectedResource_IssuerGatedRemoteBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "issuer-remote-pr-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", issuerID)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedResource := "http://0.0.0.0/mcp/" + slug
	require.Equal(t, expectedResource, metadata["resource"])
	authServers, ok := metadata["authorization_servers"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{expectedResource}, authServers)
}

func TestHandleGetProtectedResource_IssuerGatedToolsetBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsetsrepo.New(ti.conn)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "ts-issuer-pr-"+uuid.NewString()[:8])
	slug := "issuer-toolset-pr-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, issuerID)

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, "http://0.0.0.0/mcp/"+slug, metadata["resource"])
}

// TestHandleGetProtectedResource_IssuerGatedToolsetBackend_OnCustomDomain
// asserts an issuer-gated endpoint registered against a custom domain
// emits https://<domain>/mcp/<slug> as both resource and
// authorization_servers — clients reject discovery responses whose host
// doesn't match the resource they were directed to.
func TestHandleGetProtectedResource_IssuerGatedToolsetBackend_OnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	domainName := "mcp-issuer-cd-" + uuid.NewString()[:8] + ".example.com"
	toolset, domain := createPublicMCPToolsetWithCustomDomain(t, ctx, ti, authCtx, "ts-cd-"+uuid.NewString()[:8], domainName)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "issuer-cd-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{UUID: domain.ID, Valid: true}, issuerID)

	domainCtx := customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})

	w, err := runMCPWellKnown(t, domainCtx, ti.service.HandleGetProtectedResource, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))

	expectedResource := "https://" + domain.Domain + "/mcp/" + slug
	require.Equal(t, expectedResource, metadata["resource"])
	authServers, ok := metadata["authorization_servers"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{expectedResource}, authServers)
}

// TestHandleGetProtectedResource_LegacySlugFallbackProxy is the
// protected-resource companion of the legacy fallback test.
func TestHandleGetProtectedResource_LegacySlugFallbackProxy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	proxy := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "mcp-legacy-pr-proxy",
		IsPublic:     true,
		ProviderType: "",
	})
	slug := proxy.Toolset.McpSlug.String

	w, err := runMCPWellKnown(t, ctx, ti.service.HandleGetProtectedResource, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, "http://0.0.0.0/mcp/"+slug, metadata["resource"])
}
