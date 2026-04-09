// servepublic_clientdance_test.go — OAuth protocol endpoint tests (well-known
// metadata) and full client-dance integration tests that exercise the
// 401 → WWW-Authenticate → resource metadata → server metadata discovery chain.
package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// ---------------------------------------------------------------------------
// Well-known OAuth server metadata tests
// ---------------------------------------------------------------------------

func TestService_HandleWellKnownOAuthServerMetadata(t *testing.T) {
	t.Parallel()

	t.Run("returns_error_when_mcp_slug_is_missing", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/", nil)

		// Empty mcp slug - use chi.RouteCtxKey to set context
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", "")
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err := ti.service.HandleWellKnownOAuthServerMetadata(w, req)

		// Should return an error for missing slug
		require.Error(t, err)
		require.Contains(t, err.Error(), "mcp slug must be provided")
	})

	t.Run("returns_404_when_toolset_not_found", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/nonexistent", nil)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", "nonexistent-mcp-slug")
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err := ti.service.HandleWellKnownOAuthServerMetadata(w, req)

		// Should return a 404 error
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("returns_404_when_no_oauth_configuration_found", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		// Create a toolset without OAuth configuration
		slug := "no-oauth-mcp-" + uuid.New().String()[:8]
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "No OAuth MCP",
			Slug:                   slug,
			Description:            conv.ToPGText("A test MCP without OAuth"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                conv.ToPGText(slug),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+toolset.McpSlug.String, nil)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err = ti.service.HandleWellKnownOAuthServerMetadata(w, req)

		// Should return 404 for no OAuth configuration
		require.Error(t, err)
		require.Contains(t, err.Error(), "OAuth")
	})

	t.Run("returns_metadata_when_oauth_proxy_is_configured", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)
		oauthRepo := oauth_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		// Create OAuth proxy server
		oauthServer, err := oauthRepo.UpsertOAuthProxyServer(ctx, oauth_repo.UpsertOAuthProxyServerParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      "wellknown-oauth-server-" + uuid.New().String()[:8],
		})
		require.NoError(t, err)

		// Create OAuth proxy provider with "gram" type
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

		// Create a toolset with OAuth
		slug := "oauth-wellknown-mcp-" + uuid.New().String()[:8]
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "OAuth WellKnown MCP",
			Slug:                   slug,
			Description:            conv.ToPGText("A test MCP with OAuth"),
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

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+toolset.McpSlug.String, nil)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err = ti.service.HandleWellKnownOAuthServerMetadata(w, req)

		// Should return successfully with metadata
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Header().Get("Content-Type"), "application/json")
		require.NotEmpty(t, w.Body.Bytes())
	})
}

// ---------------------------------------------------------------------------
// Well-known OAuth server metadata — refresh token grant tests
// ---------------------------------------------------------------------------

func TestService_HandleWellKnownOAuthServerMetadata_RefreshTokenGrant(t *testing.T) {
	t.Parallel()

	t.Run("grant_types_supported includes refresh_token", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)
		oauthRepo := oauth_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		oauthServer, err := oauthRepo.UpsertOAuthProxyServer(ctx, oauth_repo.UpsertOAuthProxyServerParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      "grant-types-oauth-server-" + uuid.New().String()[:8],
		})
		require.NoError(t, err)

		_, err = oauthRepo.UpsertOAuthProxyProvider(ctx, oauth_repo.UpsertOAuthProxyProviderParams{
			ProjectID:                         *authCtx.ProjectID,
			OauthProxyServerID:                oauthServer.ID,
			Slug:                              "grant-types-provider-" + uuid.New().String()[:8],
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

		slug := "grant-types-mcp-" + uuid.New().String()[:8]
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Grant Types MCP",
			Slug:                   slug,
			Description:            conv.ToPGText("test grant_types_supported"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                conv.ToPGText(slug),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		toolset, err = toolsetsRepo.UpdateToolsetOAuthProxyServer(ctx, toolsets_repo.UpdateToolsetOAuthProxyServerParams{
			OauthProxyServerID: uuid.NullUUID{UUID: oauthServer.ID, Valid: true},
			Slug:               toolset.Slug,
			ProjectID:          *authCtx.ProjectID,
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+toolset.McpSlug.String, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.HandleWellKnownOAuthServerMetadata(w, req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code)

		var metadata map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &metadata)
		require.NoError(t, err)

		grantTypes, ok := metadata["grant_types_supported"].([]any)
		require.True(t, ok, "grant_types_supported should be an array")
		require.Contains(t, grantTypes, "authorization_code")
		require.Contains(t, grantTypes, "refresh_token")
	})

	t.Run("scopes_supported populated from provider", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)
		oauthRepo := oauth_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		oauthServer, err := oauthRepo.UpsertOAuthProxyServer(ctx, oauth_repo.UpsertOAuthProxyServerParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      "scopes-oauth-server-" + uuid.New().String()[:8],
		})
		require.NoError(t, err)

		_, err = oauthRepo.UpsertOAuthProxyProvider(ctx, oauth_repo.UpsertOAuthProxyProviderParams{
			ProjectID:                         *authCtx.ProjectID,
			OauthProxyServerID:                oauthServer.ID,
			Slug:                              "scopes-provider-" + uuid.New().String()[:8],
			ProviderType:                      string(oauth.OAuthProxyProviderTypeCustom),
			ScopesSupported:                   []string{"openid", "profile"},
			ResponseTypesSupported:            []string{},
			ResponseModesSupported:            []string{},
			GrantTypesSupported:               []string{},
			TokenEndpointAuthMethodsSupported: []string{},
			SecurityKeyNames:                  []string{},
			Secrets:                           []byte("{}"),
		})
		require.NoError(t, err)

		slug := "scopes-mcp-" + uuid.New().String()[:8]
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Scopes MCP",
			Slug:                   slug,
			Description:            conv.ToPGText("test scopes_supported"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                conv.ToPGText(slug),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		toolset, err = toolsetsRepo.UpdateToolsetOAuthProxyServer(ctx, toolsets_repo.UpdateToolsetOAuthProxyServerParams{
			OauthProxyServerID: uuid.NullUUID{UUID: oauthServer.ID, Valid: true},
			Slug:               toolset.Slug,
			ProjectID:          *authCtx.ProjectID,
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+toolset.McpSlug.String, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.HandleWellKnownOAuthServerMetadata(w, req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code)

		var metadata map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &metadata)
		require.NoError(t, err)

		scopes, ok := metadata["scopes_supported"].([]any)
		require.True(t, ok, "scopes_supported should be present")
		require.Contains(t, scopes, "openid")
		require.Contains(t, scopes, "profile")
	})

	t.Run("scopes_supported omitted when empty", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)
		oauthRepo := oauth_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		oauthServer, err := oauthRepo.UpsertOAuthProxyServer(ctx, oauth_repo.UpsertOAuthProxyServerParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      "no-scopes-oauth-server-" + uuid.New().String()[:8],
		})
		require.NoError(t, err)

		_, err = oauthRepo.UpsertOAuthProxyProvider(ctx, oauth_repo.UpsertOAuthProxyProviderParams{
			ProjectID:                         *authCtx.ProjectID,
			OauthProxyServerID:                oauthServer.ID,
			Slug:                              "no-scopes-provider-" + uuid.New().String()[:8],
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

		slug := "no-scopes-mcp-" + uuid.New().String()[:8]
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "No Scopes MCP",
			Slug:                   slug,
			Description:            conv.ToPGText("test scopes_supported omitted"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                conv.ToPGText(slug),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		toolset, err = toolsetsRepo.UpdateToolsetOAuthProxyServer(ctx, toolsets_repo.UpdateToolsetOAuthProxyServerParams{
			OauthProxyServerID: uuid.NullUUID{UUID: oauthServer.ID, Valid: true},
			Slug:               toolset.Slug,
			ProjectID:          *authCtx.ProjectID,
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+toolset.McpSlug.String, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		err = ti.service.HandleWellKnownOAuthServerMetadata(w, req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code)

		var metadata map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &metadata)
		require.NoError(t, err)

		// Even with no provider scopes, offline_access is always advertised
		scopes, hasScopes := metadata["scopes_supported"]
		require.True(t, hasScopes, "scopes_supported should be present")
		scopeList, ok := scopes.([]any)
		require.True(t, ok)
		require.Equal(t, []any{"offline_access"}, scopeList)
	})
}

// ---------------------------------------------------------------------------
// Well-known OAuth protected resource metadata tests
// ---------------------------------------------------------------------------

func TestService_HandleWellKnownOAuthProtectedResourceMetadata(t *testing.T) {
	t.Parallel()

	t.Run("returns_error_when_mcp_slug_is_missing", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/", nil)

		// Empty mcp slug
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", "")
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err := ti.service.HandleWellKnownOAuthProtectedResourceMetadata(w, req)

		// Should return an error for missing slug
		require.Error(t, err)
		require.Contains(t, err.Error(), "mcp slug must be provided")
	})

	t.Run("returns_404_when_toolset_not_found", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestMCPService(t)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/nonexistent", nil)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", "nonexistent-mcp-slug")
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err := ti.service.HandleWellKnownOAuthProtectedResourceMetadata(w, req)

		// Should return a 404 error
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("returns_metadata_for_valid_toolset_with_oauth", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)
		oauthRepo := oauth_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		// Create OAuth proxy server
		oauthServer, err := oauthRepo.UpsertOAuthProxyServer(ctx, oauth_repo.UpsertOAuthProxyServerParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      "protected-resource-oauth-server-" + uuid.New().String()[:8],
		})
		require.NoError(t, err)

		// Create OAuth proxy provider
		_, err = oauthRepo.UpsertOAuthProxyProvider(ctx, oauth_repo.UpsertOAuthProxyProviderParams{
			ProjectID:                         *authCtx.ProjectID,
			OauthProxyServerID:                oauthServer.ID,
			Slug:                              "protected-resource-provider-" + uuid.New().String()[:8],
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

		// Create a toolset with OAuth
		slug := "protected-resource-mcp-" + uuid.New().String()[:8]
		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Protected Resource MCP",
			Slug:                   slug,
			Description:            conv.ToPGText("A test MCP for protected resource metadata"),
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

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/"+toolset.McpSlug.String, nil)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err = ti.service.HandleWellKnownOAuthProtectedResourceMetadata(w, req)

		// Should return successfully with metadata
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Header().Get("Content-Type"), "application/json")
		require.NotEmpty(t, w.Body.Bytes())
	})
}

// ---------------------------------------------------------------------------
// Full client-dance integration tests
// ---------------------------------------------------------------------------

// TestClientDance_ExternalOAuth_FullFlow verifies the end-to-end OAuth
// discovery chain for external OAuth: unauthenticated initialize returns 401
// with a WWW-Authenticate header pointing to the protected resource metadata,
// which in turn references the external authorization server's metadata.
func TestClientDance_ExternalOAuth_FullFlow(t *testing.T) {
	t.Parallel()

	// 1. Stand up a real in-memory OAuth authorization server.
	authServer := oauthtest.NewAuthorizationServer(t)

	// 2. Create an external OAuth toolset wired to the auth server.
	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:       "ext-dance",
		IsPublic:   true,
		AuthServer: authServer,
	})

	mcpSlug := result.Toolset.McpSlug.String

	// 3. Send an initialize request WITHOUT a bearer token — expect 401.
	w, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	// 4. The 401 response must include a WWW-Authenticate header with resource_metadata URL.
	wwwAuth := w.Header().Get("WWW-Authenticate")
	require.NotEmpty(t, wwwAuth, "401 must include WWW-Authenticate header")
	require.Contains(t, wwwAuth, "Bearer resource_metadata=")

	// 5. Call HandleWellKnownOAuthProtectedResourceMetadata and verify it returns
	//    valid JSON containing the MCP resource URL.
	prReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/"+mcpSlug, nil)
	prRctx := chi.NewRouteContext()
	prRctx.URLParams.Add("mcpSlug", mcpSlug)
	prReq = prReq.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, prRctx))

	prW := httptest.NewRecorder()
	err = ti.service.HandleWellKnownOAuthProtectedResourceMetadata(prW, prReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, prW.Code)

	var prMeta map[string]any
	err = json.Unmarshal(prW.Body.Bytes(), &prMeta)
	require.NoError(t, err)

	// The resource metadata must contain a "resource" field with the MCP URL.
	resource, ok := prMeta["resource"].(string)
	require.True(t, ok, "resource field must be a string")
	require.Contains(t, resource, mcpSlug, "resource URL should reference the MCP slug")

	// 6. The authorization_servers array must exist and reference the auth server's
	//    endpoints (stored metadata from the AuthorizationServer).
	authServers, ok := prMeta["authorization_servers"].([]any)
	require.True(t, ok, "authorization_servers should be an array")
	require.NotEmpty(t, authServers, "authorization_servers should not be empty")
}

// TestClientDance_ProxyOAuth_WWWAuthenticateChain verifies the
// WWW-Authenticate -> well-known chain for proxy OAuth: unauthenticated
// initialize returns 401 with WWW-Authenticate, the protected resource
// metadata endpoint returns valid JSON, and the server metadata endpoint
// returns the authorization, token, and registration endpoints.
func TestClientDance_ProxyOAuth_WWWAuthenticateChain(t *testing.T) {
	t.Parallel()

	// 1. Create a proxy toolset (gram type, public).
	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:     "proxy-dance",
		IsPublic: true,
	})

	mcpSlug := result.Toolset.McpSlug.String

	// 2. Send an initialize request WITHOUT a bearer token — expect 401.
	w, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	// 3. The 401 response must include a WWW-Authenticate header with resource_metadata URL.
	wwwAuth := w.Header().Get("WWW-Authenticate")
	require.NotEmpty(t, wwwAuth, "401 must include WWW-Authenticate header")
	require.Contains(t, wwwAuth, "Bearer resource_metadata=")

	// 4. Call HandleWellKnownOAuthProtectedResourceMetadata — verify valid metadata.
	prReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/"+mcpSlug, nil)
	prRctx := chi.NewRouteContext()
	prRctx.URLParams.Add("mcpSlug", mcpSlug)
	prReq = prReq.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, prRctx))

	prW := httptest.NewRecorder()
	err = ti.service.HandleWellKnownOAuthProtectedResourceMetadata(prW, prReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, prW.Code)

	var prMeta map[string]any
	err = json.Unmarshal(prW.Body.Bytes(), &prMeta)
	require.NoError(t, err)

	resource, ok := prMeta["resource"].(string)
	require.True(t, ok, "resource field must be a string")
	require.Contains(t, resource, mcpSlug, "resource URL should reference the MCP slug")

	// 5. Call HandleWellKnownOAuthServerMetadata — verify it returns valid metadata
	//    with authorization_endpoint, token_endpoint, and registration_endpoint.
	smReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+mcpSlug, nil)
	smRctx := chi.NewRouteContext()
	smRctx.URLParams.Add("mcpSlug", mcpSlug)
	smReq = smReq.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, smRctx))

	smW := httptest.NewRecorder()
	err = ti.service.HandleWellKnownOAuthServerMetadata(smW, smReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, smW.Code)

	var smMeta map[string]any
	err = json.Unmarshal(smW.Body.Bytes(), &smMeta)
	require.NoError(t, err)

	// Verify the key OAuth endpoints are present in server metadata.
	authEndpoint, ok := smMeta["authorization_endpoint"].(string)
	require.True(t, ok, "authorization_endpoint must be a string")
	require.NotEmpty(t, authEndpoint)

	tokenEndpoint, ok := smMeta["token_endpoint"].(string)
	require.True(t, ok, "token_endpoint must be a string")
	require.NotEmpty(t, tokenEndpoint)

	regEndpoint, ok := smMeta["registration_endpoint"].(string)
	require.True(t, ok, "registration_endpoint must be a string")
	require.NotEmpty(t, regEndpoint)
}
