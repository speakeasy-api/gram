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
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestService_ServePublic(t *testing.T) {
	t.Parallel()

	t.Run("handles initialize request successfully", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server",
			Slug:                   "test-mcp",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                conv.ToPGText("test-mcp"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Make the toolset public so it doesn't require authentication
		toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
			Name:                   toolset.Name,
			Description:            toolset.Description,
			DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
			McpSlug:                toolset.McpSlug,
			McpIsPublic:            true,
			McpEnabled:             toolset.McpEnabled,
			Slug:                   toolset.Slug,
			ProjectID:              toolset.ProjectID,
		})
		require.NoError(t, err)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
				"params": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"clientInfo": map[string]any{
						"name":    "test-client",
						"version": "1.0.0",
					},
				},
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		mcpSlug := toolset.McpSlug.String
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", mcpSlug)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()

		err = ti.service.ServePublic(w, req)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, w.Code)
		require.NotEmpty(t, w.Header().Get("Mcp-Session-Id"))

		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err, "response body: %s", w.Body.String())
		require.Equal(t, "2.0", response["jsonrpc"])
		require.InDelta(t, 1, response["id"], 0)
		require.NotNil(t, response["result"])

		result, ok := response["result"].(map[string]any)
		require.True(t, ok, "result should be a map")
		require.Equal(t, "2024-11-05", result["protocolVersion"])
		require.NotNil(t, result["capabilities"])
		require.NotNil(t, result["serverInfo"])
	})

	t.Run("returns unauthorized for private mcp without authentication", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Private MCP Server",
			Slug:                   "private-mcp",
			Description:            conv.ToPGText("A private MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                pgtype.Text{String: "", Valid: false},
			McpEnabled:             false,
		})
		require.NoError(t, err)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.Slug, bytes.NewReader(bodyBytes))

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.Slug)
		unauthCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(unauthCtx)

		w := httptest.NewRecorder()

		err = ti.service.ServePublic(w, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("returns server instructions in initialize response", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPService(t)
		toolsetsRepo := toolsets_repo.New(ti.conn)
		metadataRepo := metadata_repo.New(ti.conn)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Name:                   "Test MCP Server with Instructions",
			Slug:                   "test-mcp-instructions",
			Description:            conv.ToPGText("A test MCP server"),
			DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
			McpSlug:                conv.ToPGText("test-mcp-instructions"),
			McpEnabled:             true,
		})
		require.NoError(t, err)

		// Make the toolset public so it doesn't require authentication
		toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
			Name:                   toolset.Name,
			Description:            toolset.Description,
			DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
			McpSlug:                toolset.McpSlug,
			McpIsPublic:            true,
			McpEnabled:             toolset.McpEnabled,
			Slug:                   toolset.Slug,
			ProjectID:              toolset.ProjectID,
		})
		require.NoError(t, err)

		// Set MCP metadata with instructions
		instructions := "You have tools for searching the Test Hub. Use them wisely."
		_, err = metadataRepo.UpsertMetadata(ctx, metadata_repo.UpsertMetadataParams{
			ToolsetID:                toolset.ID,
			ProjectID:                *authCtx.ProjectID,
			ExternalDocumentationUrl: pgtype.Text{String: "", Valid: false},
			LogoID:                   uuid.NullUUID{Valid: false},
			Instructions:             conv.ToPGText(instructions),
		})
		require.NoError(t, err)

		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
				"params": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"clientInfo": map[string]any{
						"name":    "test-client",
						"version": "1.0.0",
					},
				},
			},
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		mcpSlug := toolset.McpSlug.String
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(bodyBytes))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", mcpSlug)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()

		err = ti.service.ServePublic(w, req)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err, "response body: %s", w.Body.String())

		result, ok := response["result"].(map[string]any)
		require.True(t, ok, "result should be a map")
		require.Equal(t, "2024-11-05", result["protocolVersion"])
		require.NotNil(t, result["capabilities"])
		require.NotNil(t, result["serverInfo"])
		require.Equal(t, instructions, result["instructions"])
	})
}

// TestService_ServePublic_PrivateMCP_WithOAuth tests authentication behavior
// for private MCP servers with OAuth enabled (oAuthProxyProvider.ProviderType == "gram")
func TestService_ServePublic_PrivateMCP_WithOAuth(t *testing.T) {
	t.Parallel()

	// Helper to create initialize request body
	initializeBody := func() []byte {
		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
				"params": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"clientInfo": map[string]any{
						"name":    "test-client",
						"version": "1.0.0",
					},
				},
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		return bodyBytes
	}

	t.Run("valid OAuth token authenticates successfully", func(t *testing.T) {
		t.Parallel()

		// Create mock that returns valid token with session - closure captures sessionToken variable
		var sessionToken string
		mockOAuth := &mockOAuthService{
			validateFunc: func(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*oauth.Token, error) {
				expiresAt := time.Now().Add(24 * time.Hour)
				return &oauth.Token{
					ToolsetID:   toolsetId,
					AccessToken: accessToken,
					ExternalSecrets: []oauth.ExternalSecret{{
						Token:     sessionToken,
						ExpiresAt: &expiresAt,
					}},
				}, nil
			},
		}

		ctx, ti := newTestMCPServiceWithOAuth(t, mockOAuth)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectID)

		// Get a valid session token from the session manager
		sessionToken = ti.getSessionToken(ctx, t)
		t.Logf("Session token: %q", sessionToken)
		require.NotEmpty(t, sessionToken, "session token should be created")

		// Create toolset with OAuth - use ti directly since we have access
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
			ProviderType:                      "gram",
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
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(initializeBody()))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-oauth-token")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", mcpSlug)
		// Use a fresh context without auth to simulate external request
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err = ti.service.ServePublic(w, req)
		require.NoError(t, err)

		// WWW-Authenticate should NOT be present on success
		require.Empty(t, w.Header().Get("WWW-Authenticate"), "WWW-Authenticate header should not be present on successful auth")
	})

	t.Run("invalid OAuth token returns 401 with WWW-Authenticate", func(t *testing.T) {
		t.Parallel()

		// Create mock that returns ErrInvalidAccessToken
		mockOAuth := &mockOAuthService{
			validateFunc: func(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*oauth.Token, error) {
				return nil, oauth.ErrInvalidAccessToken
			},
		}

		ctx, ti := newTestMCPServiceWithOAuth(t, mockOAuth)

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
			ProviderType:                      "gram",
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
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(initializeBody()))
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
	})

	t.Run("valid API key authenticates without WWW-Authenticate header", func(t *testing.T) {
		t.Parallel()

		// Use real OAuth service (will fail OAuth validation, but API key succeeds)
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
			ProviderType:                      "gram",
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

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		mcpSlug := toolset.McpSlug.String
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(initializeBody()))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", mcpSlug)
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err = ti.service.ServePublic(w, req)
		require.NoError(t, err)

		// WWW-Authenticate should NOT be present when API key auth succeeds
		require.Empty(t, w.Header().Get("WWW-Authenticate"), "WWW-Authenticate header should not be present when API key auth succeeds")
	})
}

// TestService_ServePublic_PrivateMCP_WithoutOAuth tests authentication behavior
// for private MCP servers without OAuth (oAuthProxyProvider == nil)
func TestService_ServePublic_PrivateMCP_WithoutOAuth(t *testing.T) {
	t.Parallel()

	// Helper to create initialize request body
	initializeBody := func() []byte {
		reqBody := []map[string]any{
			{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
				"params": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"clientInfo": map[string]any{
						"name":    "test-client",
						"version": "1.0.0",
					},
				},
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		return bodyBytes
	}

	t.Run("valid API key authenticates successfully", func(t *testing.T) {
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

		// Create API key
		apiKey := ti.createTestAPIKey(ctx, t)

		mcpSlug := toolset.McpSlug.String
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(initializeBody()))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", mcpSlug)
		reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(reqCtx)

		w := httptest.NewRecorder()
		err = ti.service.ServePublic(w, req)
		require.NoError(t, err)

		// WWW-Authenticate should NOT be present
		require.Empty(t, w.Header().Get("WWW-Authenticate"), "WWW-Authenticate header should not be present when OAuth is not configured")
	})

	t.Run("invalid API key returns 401 without WWW-Authenticate", func(t *testing.T) {
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
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(initializeBody()))
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
	})

	t.Run("bearer token fails without WWW-Authenticate header", func(t *testing.T) {
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
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(initializeBody()))
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
	})
}
