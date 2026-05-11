// servepublic_credentialsvalid_test.go consolidates tests that verify
// credential validity for public MCP endpoints: valid tokens accepted,
// expired tokens rejected, and upstream refresh flows.
package mcp_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// setupCustomOAuthToolset creates a public toolset with a custom OAuth proxy
// provider whose token endpoint points to the given URL.
func setupCustomOAuthToolset(t *testing.T, ctx context.Context, ti *testInstance, tokenEndpoint string) (mcpSlug string, toolsetID uuid.UUID) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	oauthRepo := oauth_repo.New(ti.conn)
	oauthServer, err := oauthRepo.UpsertOAuthProxyServer(ctx, oauth_repo.UpsertOAuthProxyServerParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "custom-oauth-server-" + uuid.New().String()[:8],
	})
	require.NoError(t, err)

	_, err = oauthRepo.UpsertOAuthProxyProvider(ctx, oauth_repo.UpsertOAuthProxyProviderParams{
		ProjectID:                         *authCtx.ProjectID,
		OauthProxyServerID:                oauthServer.ID,
		Slug:                              "custom-provider-" + uuid.New().String()[:8],
		ProviderType:                      string(oauth.OAuthProxyProviderTypeCustom),
		ScopesSupported:                   []string{},
		ResponseTypesSupported:            []string{},
		ResponseModesSupported:            []string{},
		GrantTypesSupported:               []string{},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
		SecurityKeyNames:                  []string{"api_key"},
		Secrets:                           []byte(`{"client_id":"cid","client_secret":"csec"}`),
		TokenEndpoint:                     pgtype.Text{String: tokenEndpoint, Valid: true},
	})
	require.NoError(t, err)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	slug := "custom-oauth-mcp-" + uuid.New().String()[:8]
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Custom OAuth MCP",
		Slug:                   slug,
		Description:            conv.ToPGText("A public MCP with custom OAuth proxy"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

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

	_, err = toolsetsRepo.UpdateToolsetOAuthProxyServer(ctx, toolsets_repo.UpdateToolsetOAuthProxyServerParams{
		OauthProxyServerID: uuid.NullUUID{UUID: oauthServer.ID, Valid: true},
		Slug:               toolset.Slug,
		ProjectID:          *authCtx.ProjectID,
	})
	require.NoError(t, err)

	return toolset.McpSlug.String, toolset.ID
}

// ---------------------------------------------------------------------------
// Tests moved from servepublic_oauth_test.go
// ---------------------------------------------------------------------------

func TestServePublicOAuth_ProxyNoSecurityDefs_ValidToken_Succeeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateProxyToolset(t, ctx, ti.conn, authCtx, oauthtest.ProxyToolsetOpts{
		Slug:         "proxy-nosec-tok",
		IsPublic:     true,
		ProviderType: "custom",
	})

	// Issue a real Gram token backed by an upstream credential.
	upstreamExpiry := time.Now().Add(24 * time.Hour)
	issuer := oauthtest.NewTokenIssuer(t, ti.cacheAdapter, ti.enc)
	issued := issuer.IssueToken(t, ctx, result.Toolset.ID, "upstream-token", "", &upstreamExpiry, []string{})

	mcpSlug := result.Toolset.McpSlug.String
	w, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), issued.AccessToken, nil)
	// The request may fail later (e.g. no active deployment), but it must NOT
	// fail with "unauthorized" — the security check should pass.
	if err != nil {
		require.NotContains(t, err.Error(), "unauthorized", "should not return unauthorized when valid token provided")
	}
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "should not send WWW-Authenticate when valid token provided")
}

func TestServePublicOAuth_ExternalNoSecurityDefs_ValidToken_Succeeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	result := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, authCtx, oauthtest.ExternalOAuthToolsetOpts{
		Slug:     "ext-nosec-tok",
		IsPublic: true,
	})

	mcpSlug := result.Toolset.McpSlug.String
	// External OAuth flow passes the bearer token through without Gram-level
	// validation — it's collected as-is in tokenInputs.
	w, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), "some-external-token", nil)
	require.NoError(t, err)
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "should not send WWW-Authenticate when token provided")
}

// ---------------------------------------------------------------------------
// Tests flattened from TestService_ServePublic_PrivateMCP_WithOAuth
// ---------------------------------------------------------------------------

func TestServePublic_PrivateWithOAuth_ValidToken_Succeeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Get a valid session token — this is the "upstream" credential for
	// a Gram-type proxy: the external secret is the user's session token.
	sessionToken := ti.getSessionToken(ctx, t)
	require.NotEmpty(t, sessionToken, "session token should be created")

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

	// Issue a real Gram OAuth token with the session as the external secret.
	upstreamExpiry := time.Now().Add(24 * time.Hour)
	issuer := oauthtest.NewTokenIssuer(t, ti.cacheAdapter, ti.enc)
	issued := issuer.IssueToken(t, ctx, toolset.ID, sessionToken, "", &upstreamExpiry, []string{})

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(makeInitializeBody()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issued.AccessToken)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	reqCtx := context.WithValue(t.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	err = ti.service.ServePublic(w, req)
	require.NoError(t, err)

	// WWW-Authenticate should NOT be present on success
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "WWW-Authenticate header should not be present on successful auth")
}

func TestServePublic_PrivateWithOAuth_ValidAPIKey_Succeeds(t *testing.T) {
	t.Parallel()

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

	// Create API key
	apiKey := ti.createTestAPIKey(ctx, t)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(makeInitializeBody()))
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
}

// ---------------------------------------------------------------------------
// Tests flattened from TestService_ServePublic_CustomOAuthProxy
// ---------------------------------------------------------------------------

func TestServePublic_CustomProxy_UpstreamRefreshSucceeds(t *testing.T) {
	t.Parallel()

	// Stand up a real upstream OAuth server for the refresh.
	idp := devidptest.Launch(t, devidptest.LaunchOpts{})

	ctx, ti := newTestMCPService(t)
	devidptest.CreateRefreshToken(t, ctx, idp.Repo, devidptest.RefreshTokenOpts{
		Token:  "upstream-refresh",
		Mode:   devidptest.OAuth21Mode,
		UserID: idp.DefaultUser.ID,
	})

	mcpSlug, toolsetID := setupCustomOAuthToolset(t, ctx, ti, idp.OAuth21URL+"/token")

	// Issue a Gram token with expired upstream credentials.
	pastExpiry := time.Now().Add(-1 * time.Minute)
	issuer := oauthtest.NewTokenIssuer(t, ti.cacheAdapter, ti.enc)
	issued := issuer.IssueToken(t, ctx, toolsetID, "expired-upstream-access", "upstream-refresh", &pastExpiry, []string{"api_key"})

	// Expire the external secrets so ValidateAccessToken returns ErrExpiredExternalSecrets.
	issuer.ExpireExternalSecrets(t, ctx, toolsetID, issued.AccessToken, time.Now().Add(-1*time.Minute))

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(makeInitializeBody()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issued.AccessToken)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.ServePublic(w, req)
	require.NoError(t, err, "request should succeed after upstream refresh")
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "no WWW-Authenticate on success")
}

func TestServePublic_CustomProxy_UpstreamRefreshFailureAllowsInitialize(t *testing.T) {
	t.Parallel()

	// Upstream server that rejects all token requests (simulates refresh failure).
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	t.Cleanup(failServer.Close)

	ctx, ti := newTestMCPService(t)
	mcpSlug, toolsetID := setupCustomOAuthToolset(t, ctx, ti, failServer.URL+"/token")

	pastExpiry := time.Now().Add(-1 * time.Minute)
	issuer := oauthtest.NewTokenIssuer(t, ti.cacheAdapter, ti.enc)
	issued := issuer.IssueToken(t, ctx, toolsetID, "expired-upstream-access", "upstream-refresh", &pastExpiry, []string{"api_key"})
	issuer.ExpireExternalSecrets(t, ctx, toolsetID, issued.AccessToken, time.Now().Add(-1*time.Minute))

	// Token refresh failure is best-effort — initialize still succeeds because
	// this test toolset has no security definitions (no deployed tools with
	// http_security rows).
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(makeInitializeBody()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issued.AccessToken)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.ServePublic(w, req)
	require.NoError(t, err, "initialize should succeed even when token refresh fails")
	require.Equal(t, http.StatusOK, w.Code)
}

func TestServePublic_CustomProxy_ValidTokenNoRefresh(t *testing.T) {
	t.Parallel()

	// Upstream server that fails if contacted (should never be hit).
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"should_not_be_called"}`))
	}))
	t.Cleanup(failServer.Close)

	ctx, ti := newTestMCPService(t)
	mcpSlug, toolsetID := setupCustomOAuthToolset(t, ctx, ti, failServer.URL+"/token")

	// Issue a Gram token with valid (non-expired) upstream credentials.
	futureExpiry := time.Now().Add(24 * time.Hour)
	issuer := oauthtest.NewTokenIssuer(t, ti.cacheAdapter, ti.enc)
	issued := issuer.IssueToken(t, ctx, toolsetID, "valid-upstream-access", "", &futureExpiry, []string{"api_key"})

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(makeInitializeBody()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issued.AccessToken)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.ServePublic(w, req)
	require.NoError(t, err, "request should succeed with valid token")
}

// ---------------------------------------------------------------------------
// Test flattened from TestService_ServePublic_PrivateMCP_WithoutOAuth
// ---------------------------------------------------------------------------

func TestServePublic_PrivateWithoutOAuth_ValidAPIKey_Succeeds(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug, bytes.NewReader(makeInitializeBody()))
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
}

// ---------------------------------------------------------------------------
// New test: expired Gram token returns 401
// ---------------------------------------------------------------------------

// assertCustomProxyRefreshFailsClosed is the shared assertion body for
// the TestServePublic_CustomProxy_UpstreamRefreshFails_*_WithSecurity_Returns401
// cluster. The upstream /token endpoint returns the supplied status (4xx
// for credential-class failure, 5xx for transient), and the test verifies
// that with security defs deployed Gram fails closed at /mcp/{slug}
// rather than letting a stale upstream secret proceed.
//
// We can't use setupCustomOAuthToolset here because it hardcodes
// SecurityKeyNames=["api_key"], which would fail the security check via
// key mismatch — masking the actual property under test.
func assertCustomProxyRefreshFailsClosed(t *testing.T, slugPrefix string, upstreamStatus int) {
	t.Helper()

	const securityKey = "TEST_API_KEY"

	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(upstreamStatus)
		_, _ = w.Write([]byte(`{"error":"upstream_failure"}`))
	}))
	t.Cleanup(failServer.Close)

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	oauthRepo := oauth_repo.New(ti.conn)
	oauthServer, err := oauthRepo.UpsertOAuthProxyServer(ctx, oauth_repo.UpsertOAuthProxyServerParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      slugPrefix + "-server-" + uuid.New().String()[:8],
	})
	require.NoError(t, err)

	_, err = oauthRepo.UpsertOAuthProxyProvider(ctx, oauth_repo.UpsertOAuthProxyProviderParams{
		ProjectID:                         *authCtx.ProjectID,
		OauthProxyServerID:                oauthServer.ID,
		Slug:                              slugPrefix + "-provider-" + uuid.New().String()[:8],
		ProviderType:                      string(oauth.OAuthProxyProviderTypeCustom),
		ScopesSupported:                   []string{},
		ResponseTypesSupported:            []string{},
		ResponseModesSupported:            []string{},
		GrantTypesSupported:               []string{},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
		SecurityKeyNames:                  []string{securityKey},
		Secrets:                           []byte(`{"client_id":"cid","client_secret":"csec"}`),
		TokenEndpoint:                     pgtype.Text{String: failServer.URL + "/token", Valid: true},
	})
	require.NoError(t, err)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	slug := slugPrefix + "-mcp-" + uuid.New().String()[:8]
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Refresh-Fail MCP",
		Slug:                   slug,
		Description:            conv.ToPGText("Custom OAuth toolset with matching security key"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

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

	_, err = toolsetsRepo.UpdateToolsetOAuthProxyServer(ctx, toolsets_repo.UpdateToolsetOAuthProxyServerParams{
		OauthProxyServerID: uuid.NullUUID{UUID: oauthServer.ID, Valid: true},
		Slug:               toolset.Slug,
		ProjectID:          *authCtx.ProjectID,
	})
	require.NoError(t, err)

	ti.addToolWithSecurity(ctx, t, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)

	pastExpiry := time.Now().Add(-1 * time.Minute)
	issuer := oauthtest.NewTokenIssuer(t, ti.cacheAdapter, ti.enc)
	issued := issuer.IssueToken(t, ctx, toolset.ID, "stale-upstream-access", "upstream-refresh", &pastExpiry, []string{securityKey})
	issuer.ExpireExternalSecrets(t, ctx, toolset.ID, issued.AccessToken, time.Now().Add(-1*time.Minute))

	w, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), issued.AccessToken, nil)
	require.Error(t, err, "upstream refresh failure with security defs must fail closed")
	require.Contains(t, err.Error(), "unauthorized")
	require.NotEmpty(t, w.Header().Get("WWW-Authenticate"),
		"WWW-Authenticate must be set so MCP clients can re-auth via discovery")
}

// TestServePublic_CustomProxy_UpstreamRefreshFails4xx_WithSecurity_Returns401
// verifies Gram's fail-closed behavior when upstream /token refresh returns
// a 4xx (credential-class failure, e.g. invalid_grant) on a custom-proxy
// toolset that has security definitions deployed. The request is rejected
// with 401 + WWW-Authenticate so the MCP client can re-auth, rather than
// being allowed to proceed with a stale upstream secret that would only
// fail at the actual tool-call boundary.
//
// Distinct from TestServePublic_CustomProxy_UpstreamRefreshFailureAllowsInitialize,
// which exercises the no-security-defs branch where initialize trivially
// succeeds because there's nothing to satisfy. This test adds matching
// security defs so the resolved-env-vars path is forced to consider the
// expired external secret — and the impl correctly fails closed.
func TestServePublic_CustomProxy_UpstreamRefreshFails4xx_WithSecurity_Returns401(t *testing.T) {
	t.Parallel()
	assertCustomProxyRefreshFailsClosed(t, "refresh-fail-4xx", http.StatusBadRequest)
}

// TestServePublic_CustomProxy_UpstreamRefreshFails5xx_WithSecurity_Returns401
// verifies the same fail-closed behavior for transient (5xx) upstream
// failures. Listed separately from the 4xx case so any future divergence
// (e.g. treating 5xx as retryable and returning a stale secret) becomes
// an explicit, separately-tested change rather than a silent regression
// of the more permissive variant.
func TestServePublic_CustomProxy_UpstreamRefreshFails5xx_WithSecurity_Returns401(t *testing.T) {
	t.Parallel()
	assertCustomProxyRefreshFailsClosed(t, "refresh-fail-5xx", http.StatusServiceUnavailable)
}

// TestServePublic_PrivateWithOAuth_WrongToolsetToken_Returns401 verifies the
// audience-binding property: a Gram OAuth token issued for one toolset must
// not authenticate requests to a different toolset, even when both toolsets
// share the same OAuth proxy configuration.
//
// This is the OAuth 2.0 "confused deputy" defense (OAuth 2.1 BCP §4.10).
// Today ValidateAccessToken takes toolsetID as a lookup key, so a token
// bound to toolset A produces no cache hit when looked up under toolset B.
// The test pins that down end-to-end through /mcp/{slug} so future caching
// or lookup changes cannot silently break the property.
func TestServePublic_PrivateWithOAuth_WrongToolsetToken_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Two independent private OAuth toolsets in the same project.
	toolsetA := createPrivateOAuthToolset(t, ctx, ti.conn, authCtx, "wrong-aud-a")
	toolsetB := createPrivateOAuthToolset(t, ctx, ti.conn, authCtx, "wrong-aud-b")

	// Issue a real Gram OAuth token bound to toolset A. Use a session as
	// the upstream credential, mirroring the pattern in
	// TestServePublic_PrivateWithOAuth_ValidToken_Succeeds.
	sessionToken := ti.getSessionToken(ctx, t)
	require.NotEmpty(t, sessionToken)

	upstreamExpiry := time.Now().Add(24 * time.Hour)
	issuer := oauthtest.NewTokenIssuer(t, ti.cacheAdapter, ti.enc)
	issuedForA := issuer.IssueToken(t, ctx, toolsetA.ID, sessionToken, "", &upstreamExpiry, []string{})

	// Sanity check: the token works against its intended toolset.
	w, err := servePublicHTTP(t, context.Background(), ti, toolsetA.McpSlug.String, makeInitializeBody(), issuedForA.AccessToken, nil)
	require.NoError(t, err, "token should authenticate against its bound toolset")
	require.Empty(t, w.Header().Get("WWW-Authenticate"))

	// Now present the same token against toolset B. Must 401 with
	// WWW-Authenticate so the client knows to re-auth for the right resource.
	w, err = servePublicHTTP(t, context.Background(), ti, toolsetB.McpSlug.String, makeInitializeBody(), issuedForA.AccessToken, nil)
	require.Error(t, err, "token bound to toolset A must not authenticate at toolset B")
	require.Contains(t, err.Error(), "expired or invalid access token")
	require.NotEmpty(t, w.Header().Get("WWW-Authenticate"),
		"WWW-Authenticate must point at toolset B's protected-resource so the client re-auths for the right audience")
	require.Contains(t, w.Header().Get("WWW-Authenticate"), toolsetB.McpSlug.String,
		"WWW-Authenticate resource_metadata must reference toolset B (the requested resource), not toolset A")
}

// TestServePublic_CustomProxy_ExpiredGramToken_Returns401 verifies that when
// the Gram access token itself is expired (not the upstream external secrets),
// the request is rejected with 401 and a WWW-Authenticate header.
func TestServePublic_CustomProxy_ExpiredGramToken_Returns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	// Use a dummy upstream URL since refresh should never be attempted when the
	// Gram token itself is expired.
	mcpSlug, toolsetID := setupCustomOAuthToolset(t, ctx, ti, "http://localhost:0/should-not-be-called")

	// Issue a valid Gram token, then expire the Gram token itself.
	futureExpiry := time.Now().Add(24 * time.Hour)
	issuer := oauthtest.NewTokenIssuer(t, ti.cacheAdapter, ti.enc)
	issued := issuer.IssueToken(t, ctx, toolsetID, "upstream-access", "", &futureExpiry, []string{"api_key"})

	// Expire the Gram access token (NOT the external secrets).
	issuer.ExpireToken(t, ctx, toolsetID, issued.AccessToken, time.Now().Add(-1*time.Minute))

	w, err := servePublicHTTP(t, context.Background(), ti, mcpSlug, makeInitializeBody(), issued.AccessToken, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized", "expired Gram token should be rejected")

	// WWW-Authenticate header must be present on 401
	wwwAuth := w.Header().Get("WWW-Authenticate")
	require.NotEmpty(t, wwwAuth, "WWW-Authenticate header should be present when Gram token is expired")
	require.Contains(t, wwwAuth, "Bearer resource_metadata=")
}
