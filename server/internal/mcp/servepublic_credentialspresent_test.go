// Tests for credential presence/absence behavior on public MCP endpoints.
// Each test verifies that requests with missing, invalid, or correct credentials
// are accepted or rejected as expected based on security definitions, OAuth
// configuration, and server visibility (public vs private).
package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// ---------------------------------------------------------------------------
// Tests from servepublic_auth_test.go (all except helpers and BatchRequest)
// ---------------------------------------------------------------------------

func TestServePublicAuth_NoSecurityDefs_InitializeSucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "pub-no-sec-init")

	unauthCtx := context.Background()
	w, err := servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestServePublicAuth_WithSecurityDefs_NoCredentials_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "pub-sec-no-creds")
	ti.addToolWithSecurity(ctx, t, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)

	// Initialize without any credentials — should get 401
	unauthCtx := context.Background()
	_, err := servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestServePublicAuth_WithSecurityDefs_ValidMCPHeader_Succeeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "pub-sec-valid-hdr")
	envVar := ti.addToolWithSecurity(ctx, t, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)

	// Send the matching MCP header — env var is TEST_API_KEY, so header is MCP-Test-Api-Key
	_ = envVar
	unauthCtx := context.Background()
	w, err := servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", map[string]string{
		"MCP-Test-Api-Key": "some-secret-value",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestServePublicAuth_WithSecurityDefs_WrongMCPHeader_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "pub-sec-wrong-hdr")
	ti.addToolWithSecurity(ctx, t, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)

	// Send an MCP header that doesn't match any security env var
	unauthCtx := context.Background()
	_, err := servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", map[string]string{
		"MCP-Wrong-Key": "some-value",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestServePublicAuth_PrivateServer_NoToken_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private Auth Test MCP",
		Slug:                   "priv-auth-test",
		Description:            conv.ToPGText("A private MCP for auth testing"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("priv-auth-test"),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	unauthCtx := context.Background()
	_, err = servePublicHTTP(t, unauthCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired or invalid access token")
}

// ---------------------------------------------------------------------------
// Tests from servepublic_oauth_test.go (selected)
// ---------------------------------------------------------------------------

// TestServePublicOAuth_ProxyNoSecurityDefs_NoToken_Returns401 is the exact
// regression scenario from the Mar 31 - Apr 2 incident. A public toolset with
// OAuth proxy configured but no per-tool security annotations must return 401
// with WWW-Authenticate when no token is provided.
func TestServePublicOAuth_ProxyNoSecurityDefs_NoToken_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:     "proxy-nosec",
		IsPublic: true,
	})

	mcpSlug := result.Toolset.McpSlug.String
	_, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized", "should return unauthorized when no token provided for OAuth-configured server")
}

// TestServePublicOAuth_ExternalNoSecurityDefs_NoToken_Returns401 verifies the
// same behavior for external OAuth servers (ExternalOauthServerID).
func TestServePublicOAuth_ExternalNoSecurityDefs_NoToken_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "ext-nosec",
		IsPublic: true,
	})

	mcpSlug := result.Toolset.McpSlug.String
	_, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized", "should return unauthorized when no token provided for external OAuth server")
}

// TestServePublicOAuth_NoOAuth_NoSecurityDefs_Succeeds verifies that a public
// toolset without OAuth and without security annotations succeeds without any
// credentials (baseline behavior).
func TestServePublicOAuth_NoOAuth_NoSecurityDefs_Succeeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "no-oauth-nosec-"+uuid.New().String()[:8])

	mcpSlug := toolset.McpSlug.String
	_, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "", nil)
	require.NoError(t, err, "public MCP without OAuth should succeed without credentials")
}

// ---------------------------------------------------------------------------
// Tests flattened from servepublic_test.go subtests
// ---------------------------------------------------------------------------

func TestServePublic_DeniesUnauthenticatedAccessToPrivateMCP(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolsetsRepo := toolsets_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create a PRIVATE toolset
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private Unauthenticated MCP",
		Slug:                   "private-unauth-mcp",
		Description:            conv.ToPGText("A private MCP not accessible without auth"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("private-unauth-mcp"),
		McpEnabled:             true,
		// McpIsPublic defaults to false
	})
	require.NoError(t, err)

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(bodyBytes))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	// Use a context WITHOUT any auth
	unauthCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(unauthCtx)

	w := httptest.NewRecorder()

	// This should fail - private MCPs require authentication
	err = ti.service.ServePublic(w, req)
	require.Error(t, err, "private MCP should NOT be accessible without authentication")
	require.Contains(t, err.Error(), "expired or invalid access token")
}

func TestServePublic_PrivateWithOAuth_InvalidToken_Returns401WithWWWAuthenticate(t *testing.T) {
	t.Parallel()

	// Real OAuth service — an unknown token will naturally fail validation.
	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create toolset with OAuth
	oauthRepo := oauth_repo.New(ti.conn)
	oauthServer, err := oauthRepo.UpsertOAuthProxyServer(ctx, oauth_repo.UpsertOAuthProxyServerParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "test-oauth-server-" + uuid.New().String()[:8],
	})
	require.NoError(t, err)

	_, err = oauthRepo.UpsertOAuthProxyProvider(ctx, oauth_repo.UpsertOAuthProxyProviderParams{
		ProjectID:                         *authCtx.ProjectID,
		OauthProxyServerID:                oauthServer.ID,
		Slug:                              "gram-provider-" + uuid.New().String()[:8],
		ProviderType:                      string(oauth.OAuthProxyProviderTypeGram),
		ScopesSupported:                   []string{},
		ResponseTypesSupported:            []string{},
		ResponseModesSupported:            []string{},
		GrantTypesSupported:               []string{},
		TokenEndpointAuthMethodsSupported: []string{},
		SecurityKeyNames:                  []string{},
		Secrets:                           []byte("{}"),
	})
	require.NoError(t, err)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	slug := "private-oauth-mcp-" + uuid.New().String()[:8]
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private OAuth MCP",
		Slug:                   slug,
		Description:            conv.ToPGText("A private MCP server with OAuth"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	// Link toolset to OAuth proxy server
	toolset, err = toolsetsRepo.UpdateToolsetOAuthProxyServer(ctx, toolsets_repo.UpdateToolsetOAuthProxyServerParams{
		OauthProxyServerID: uuid.NullUUID{UUID: oauthServer.ID, Valid: true},
		Slug:               toolset.Slug,
		ProjectID:          *authCtx.ProjectID,
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(makeInitializeBody()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer invalid-oauth-token")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	err = ti.service.ServePublic(w, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired or invalid access token")

	// WWW-Authenticate header should be present when OAuth is enabled and auth fails
	wwwAuth := w.Header().Get("WWW-Authenticate")
	require.NotEmpty(t, wwwAuth, "WWW-Authenticate header should be present when OAuth is enabled and auth fails")
	require.Contains(t, wwwAuth, "Bearer resource_metadata=")
}

func TestServePublic_PrivateWithoutOAuth_InvalidAPIKey_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create private toolset WITHOUT OAuth proxy server
	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private No-OAuth MCP",
		Slug:                   "private-no-oauth-mcp-" + uuid.New().String()[:8],
		Description:            conv.ToPGText("A private MCP server without OAuth"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("private-no-oauth-mcp-" + uuid.New().String()[:8]),
		McpEnabled:             true,
		// OauthProxyServerID NOT set - no OAuth
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(makeInitializeBody()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer invalid-api-key")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	err = ti.service.ServePublic(w, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired or invalid access token")

	// WWW-Authenticate should NOT be present when OAuth is not configured
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "WWW-Authenticate header should not be present when OAuth is not configured")
}

func TestServePublic_PrivateWithoutOAuth_BearerTokenFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Create private toolset WITHOUT OAuth proxy server
	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private No-OAuth MCP",
		Slug:                   "private-no-oauth-mcp-" + uuid.New().String()[:8],
		Description:            conv.ToPGText("A private MCP server without OAuth"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("private-no-oauth-mcp-" + uuid.New().String()[:8]),
		McpEnabled:             true,
		// OauthProxyServerID NOT set - no OAuth
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(makeInitializeBody()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer some-random-bearer-token")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	err = ti.service.ServePublic(w, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired or invalid access token")

	// WWW-Authenticate should NOT be present when OAuth is not configured
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "WWW-Authenticate header should not be present when OAuth is not configured")
}

// ---------------------------------------------------------------------------
// Dual security scheme tests: toolsets with BOTH apiKey AND oauth2 security.
// These exercise the anySchemeSatisfied logic where either credential satisfies.
// ---------------------------------------------------------------------------

// TestServePublic_DualSecurity_NoCredentials_Returns401 verifies that a public
// toolset with both apiKey and oauth2 security returns 401 when no credentials
// are provided.
func TestServePublic_DualSecurity_NoCredentials_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "dual-sec-nocreds")
	ti.addToolWithDualSecurity(ctx, t, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)

	_, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

// TestServePublic_DualSecurity_APIKeyOnly_Succeeds verifies that providing only
// an apiKey header satisfies the security check when both apiKey and oauth2
// schemes are defined — the apiKey scheme alone is sufficient.
func TestServePublic_DualSecurity_APIKeyOnly_Succeeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "dual-sec-apikey")
	ti.addToolWithDualSecurity(ctx, t, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)

	w, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), "", map[string]string{
		"MCP-Test-Api-Key": "some-api-key-value",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

// TestServePublic_DualSecurity_OAuthTokenOnly_Succeeds verifies that providing
// only an OAuth token satisfies the security check when both apiKey and oauth2
// schemes are defined — the oauth2 scheme alone is sufficient.
func TestServePublic_DualSecurity_OAuthTokenOnly_Succeeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "dual-sec-oauth",
		IsPublic:     true,
		ProviderType: "custom",
	})

	ti.addToolWithDualSecurity(ctx, t, result.Toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)

	// Issue a real Gram token with an upstream credential.
	upstreamExpiry := time.Now().Add(24 * time.Hour)
	issuer := oauthtest.NewTokenIssuer(t, ti.cacheAdapter, ti.enc)
	issued := issuer.IssueToken(t, ctx, result.Toolset.ID, "upstream-token", "", &upstreamExpiry, []string{"test_oauth"})

	w, err := servePublicHTTP(t, context.Background(), ti, result.Toolset.McpSlug.String, makeInitializeBody(), issued.AccessToken, nil)
	if err != nil {
		require.NotContains(t, err.Error(), "unauthorized", "oauth2 token alone should satisfy the dual-security check")
	}
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
}
