package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/jackc/pgx/v5/pgtype"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// ExternalOAuthState represents the state stored during external OAuth flow
type ExternalOAuthState struct {
	// User context
	UserID         string `json:"user_id"`
	OrganizationID string `json:"organization_id"`

	// OAuth flow context
	ToolsetID    uuid.UUID `json:"toolset_id"`
	RedirectURI  string    `json:"redirect_uri"`
	CodeVerifier string    `json:"code_verifier"`

	// External OAuth server info (for callback)
	OAuthServerIssuer string `json:"oauth_server_issuer"`
	TokenEndpoint     string `json:"token_endpoint"`
	ProviderName      string `json:"provider_name"`
	Scope             string `json:"scope"`

	// Timing
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

var _ cache.CacheableObject[ExternalOAuthState] = (*ExternalOAuthState)(nil)

func ExternalOAuthStateCacheKey(stateID string) string {
	return "externalOAuthState:" + stateID
}

func (s ExternalOAuthState) CacheKey() string {
	// Use a hash of the state for the cache key
	return ExternalOAuthStateCacheKey(s.CodeVerifier[:16])
}

func (s ExternalOAuthState) AdditionalCacheKeys() []string {
	return []string{}
}

func (s ExternalOAuthState) TTL() time.Duration {
	return time.Until(s.ExpiresAt)
}

// ExternalOAuthService handles OAuth flows where Gram acts as the OAuth client
// to external providers (e.g., Google, Atlassian) for external MCP servers.
type ExternalOAuthService struct {
	logger       *slog.Logger
	db           interface{}
	oauthRepo    *repo.Queries
	toolsetsRepo *toolsets_repo.Queries
	stateStorage cache.TypedCacheObject[ExternalOAuthState]
	serverURL    *url.URL
	enc          interface {
		Encrypt(plaintext []byte) (string, error)
		Decrypt(ciphertext string) ([]byte, error)
	}
	httpClient *http.Client
}

// NewExternalOAuthService creates a new ExternalOAuthService
func NewExternalOAuthService(
	logger *slog.Logger,
	db interface{},
	oauthRepo *repo.Queries,
	toolsetsRepo *toolsets_repo.Queries,
	cacheImpl cache.Cache,
	serverURL *url.URL,
	enc interface {
		Encrypt(plaintext []byte) (string, error)
		Decrypt(ciphertext string) ([]byte, error)
	},
) *ExternalOAuthService {
	stateStorage := cache.NewTypedObjectCache[ExternalOAuthState](
		logger.With(attr.SlogCacheNamespace("external_oauth_state")),
		cacheImpl,
		cache.SuffixNone,
	)

	return &ExternalOAuthService{
		logger:       logger.With(attr.SlogComponent("external_oauth")),
		db:           db,
		oauthRepo:    oauthRepo,
		toolsetsRepo: toolsetsRepo,
		stateStorage: stateStorage,
		serverURL:    serverURL,
		enc:          enc,
		httpClient:   retryablehttp.NewClient().StandardClient(),
	}
}

// AttachExternalOAuth attaches external OAuth endpoints to the router
func AttachExternalOAuth(mux goahttp.Muxer, service *ExternalOAuthService) {
	// External OAuth authorization endpoint - initiates OAuth flow with external provider
	o11y.AttachHandler(mux, "GET", "/oauth/external/authorize", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleExternalAuthorize).ServeHTTP(w, r)
	})

	// External OAuth callback - handles callback from external provider
	o11y.AttachHandler(mux, "GET", "/oauth/external/callback", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleExternalCallback).ServeHTTP(w, r)
	})

	// Check OAuth connection status for a toolset
	o11y.AttachHandler(mux, "GET", "/oauth/external/status/{toolsetID}", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleExternalStatus).ServeHTTP(w, r)
	})

	// Disconnect OAuth connection
	o11y.AttachHandler(mux, "DELETE", "/oauth/external/disconnect", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleExternalDisconnect).ServeHTTP(w, r)
	})

	// Get access token for MCP requests (used by Elements)
	o11y.AttachHandler(mux, "GET", "/oauth/external/token", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleGetToken).ServeHTTP(w, r)
	})
}

// handleExternalAuthorize initiates the OAuth flow with an external provider
func (s *ExternalOAuthService) handleExternalAuthorize(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Get user session
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.E(oops.CodeUnauthorized, nil, "authentication required").Log(ctx, s.logger)
	}

	// Parse query parameters
	toolsetIDStr := r.URL.Query().Get("toolset_id")
	redirectURI := r.URL.Query().Get("redirect_uri")

	if toolsetIDStr == "" {
		return oops.E(oops.CodeBadRequest, nil, "toolset_id is required").Log(ctx, s.logger)
	}
	if redirectURI == "" {
		return oops.E(oops.CodeBadRequest, nil, "redirect_uri is required").Log(ctx, s.logger)
	}

	toolsetID, err := uuid.Parse(toolsetIDStr)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
	}

	// Load toolset and verify user has access
	toolset, err := s.toolsetsRepo.GetToolsetByID(ctx, toolsetID)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	// Verify user has access to this toolset's organization
	if toolset.OrganizationID != authCtx.ActiveOrganizationID {
		return oops.E(oops.CodeForbidden, nil, "access denied").Log(ctx, s.logger)
	}

	// Get external MCP OAuth configuration from toolset
	oauthConfig, err := s.getExternalOAuthConfig(ctx, toolset)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "toolset does not require OAuth").Log(ctx, s.logger)
	}

	// Generate PKCE code_verifier (43-128 chars)
	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to generate code verifier").Log(ctx, s.logger)
	}

	// Generate code_challenge using S256
	codeChallenge := generateCodeChallenge(codeVerifier)

	// Generate state ID for cache key
	stateID, err := generateStateID()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to generate state ID").Log(ctx, s.logger)
	}

	// Create state object
	state := ExternalOAuthState{
		UserID:            authCtx.UserID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		ToolsetID:         toolsetID,
		RedirectURI:       redirectURI,
		CodeVerifier:      codeVerifier,
		OAuthServerIssuer: oauthConfig.Issuer,
		TokenEndpoint:     oauthConfig.TokenEndpoint,
		ProviderName:      oauthConfig.ProviderName,
		Scope:             strings.Join(oauthConfig.ScopesSupported, " "),
		CreatedAt:         time.Now(),
		ExpiresAt:         time.Now().Add(10 * time.Minute),
	}

	// Store state in cache
	if err := s.stateStorage.Store(ctx, state); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to store OAuth state").Log(ctx, s.logger)
	}

	// Build authorization URL
	authURL, err := url.Parse(oauthConfig.AuthorizationEndpoint)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "invalid authorization endpoint").Log(ctx, s.logger)
	}

	callbackURL := fmt.Sprintf("%s/oauth/external/callback", s.serverURL.String())

	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", oauthConfig.ClientID)
	params.Set("redirect_uri", callbackURL)
	params.Set("state", stateID)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	if state.Scope != "" {
		params.Set("scope", state.Scope)
	}

	authURL.RawQuery = params.Encode()

	s.logger.InfoContext(ctx, "redirecting to external OAuth provider",
		attr.SlogOAuthIssuer(oauthConfig.Issuer),
		attr.SlogUserID(authCtx.UserID))

	// Redirect to external OAuth provider
	http.Redirect(w, r, authURL.String(), http.StatusFound)
	return nil
}

// handleExternalCallback handles the callback from the external OAuth provider
func (s *ExternalOAuthService) handleExternalCallback(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Get authorization code and state from query
	code := r.URL.Query().Get("code")
	stateID := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")
	errorDesc := r.URL.Query().Get("error_description")

	if errorParam != "" {
		s.logger.ErrorContext(ctx, "external OAuth error",
			attr.SlogErrorMessage(errorParam),
			attr.SlogReason(errorDesc))
		// Redirect back with error
		return s.redirectWithError(w, r, "", "oauth_error", errorDesc)
	}

	if code == "" {
		return oops.E(oops.CodeBadRequest, nil, "code is required").Log(ctx, s.logger)
	}
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").Log(ctx, s.logger)
	}

	// Retrieve state from cache
	state, err := s.stateStorage.Get(ctx, ExternalOAuthStateCacheKey(stateID))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid or expired state").Log(ctx, s.logger)
	}

	// Check if state has expired
	if time.Now().After(state.ExpiresAt) {
		return oops.E(oops.CodeBadRequest, nil, "state has expired").Log(ctx, s.logger)
	}

	// Delete state from cache (one-time use)
	if err := s.stateStorage.Delete(ctx, state); err != nil {
		s.logger.WarnContext(ctx, "failed to delete OAuth state from cache", attr.SlogError(err))
	}

	// Get OAuth config to get client credentials
	toolset, err := s.toolsetsRepo.GetToolsetByID(ctx, state.ToolsetID)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	oauthConfig, err := s.getExternalOAuthConfig(ctx, toolset)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get OAuth config").Log(ctx, s.logger)
	}

	// Exchange code for tokens
	callbackURL := fmt.Sprintf("%s/oauth/external/callback", s.serverURL.String())
	tokenResp, err := s.exchangeCodeForTokens(ctx, oauthConfig, code, callbackURL, state.CodeVerifier)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to exchange code for tokens", attr.SlogError(err))
		return s.redirectWithError(w, r, state.RedirectURI, "token_exchange_failed", err.Error())
	}

	// Encrypt tokens before storing
	accessTokenEncrypted, err := s.enc.Encrypt([]byte(tokenResp.AccessToken))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to encrypt access token").Log(ctx, s.logger)
	}

	var refreshTokenEncrypted pgtype.Text
	if tokenResp.RefreshToken != "" {
		encrypted, err := s.enc.Encrypt([]byte(tokenResp.RefreshToken))
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to encrypt refresh token").Log(ctx, s.logger)
		}
		refreshTokenEncrypted = conv.ToPGText(encrypted)
	}

	// Calculate expiration time
	var expiresAt pgtype.Timestamptz
	if tokenResp.ExpiresIn > 0 {
		expiresAt = pgtype.Timestamptz{
			Time:             time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
			Valid:            true,
			InfinityModifier: pgtype.Finite,
		}
	}

	// Store token in database
	_, err = s.oauthRepo.UpsertUserOAuthToken(ctx, repo.UpsertUserOAuthTokenParams{
		UserID:                state.UserID,
		OrganizationID:        state.OrganizationID,
		OauthServerIssuer:     state.OAuthServerIssuer,
		AccessTokenEncrypted:  accessTokenEncrypted,
		RefreshTokenEncrypted: refreshTokenEncrypted,
		TokenType:             tokenResp.TokenType,
		ExpiresAt:             expiresAt,
		Scope:                 conv.ToPGText(tokenResp.Scope),
		ProviderName:          conv.ToPGText(state.ProviderName),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to store OAuth token").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "external OAuth token stored successfully",
		attr.SlogUserID(state.UserID),
		attr.SlogOAuthIssuer(state.OAuthServerIssuer))

	// Redirect back to original redirect_uri with success
	redirectURL, err := url.Parse(state.RedirectURI)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "invalid redirect URI").Log(ctx, s.logger)
	}

	params := redirectURL.Query()
	params.Set("oauth_success", "true")
	params.Set("provider", state.ProviderName)
	redirectURL.RawQuery = params.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
	return nil
}

// handleExternalStatus checks if the user has a valid OAuth token for a toolset
func (s *ExternalOAuthService) handleExternalStatus(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Get user session
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.E(oops.CodeUnauthorized, nil, "authentication required").Log(ctx, s.logger)
	}

	toolsetIDStr := chi.URLParam(r, "toolsetID")
	if toolsetIDStr == "" {
		return oops.E(oops.CodeBadRequest, nil, "toolset_id is required").Log(ctx, s.logger)
	}

	toolsetID, err := uuid.Parse(toolsetIDStr)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
	}

	// Load toolset
	toolset, err := s.toolsetsRepo.GetToolsetByID(ctx, toolsetID)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	// Verify user has access
	if toolset.OrganizationID != authCtx.ActiveOrganizationID {
		return oops.E(oops.CodeForbidden, nil, "access denied").Log(ctx, s.logger)
	}

	// Get OAuth config
	oauthConfig, err := s.getExternalOAuthConfig(ctx, toolset)
	if err != nil {
		// Toolset doesn't require OAuth
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"requires_oauth": false,
			"connected":      false,
		}); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode response", attr.SlogError(err))
		}
		return nil
	}

	// Check if user has a token for this OAuth server
	token, err := s.oauthRepo.GetUserOAuthToken(ctx, repo.GetUserOAuthTokenParams{
		UserID:            authCtx.UserID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		OauthServerIssuer: oauthConfig.Issuer,
	})

	connected := err == nil
	var expired bool
	if connected && token.ExpiresAt.Valid {
		expired = time.Now().After(token.ExpiresAt.Time)
	}

	response := map[string]interface{}{
		"requires_oauth": true,
		"connected":      connected && !expired,
		"provider_name":  oauthConfig.ProviderName,
		"issuer":         oauthConfig.Issuer,
	}

	if connected {
		response["expires_at"] = token.ExpiresAt.Time
		response["scope"] = token.Scope.String
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.ErrorContext(ctx, "failed to encode response", attr.SlogError(err))
	}
	return nil
}

// handleExternalDisconnect removes an OAuth token for a toolset
func (s *ExternalOAuthService) handleExternalDisconnect(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Get user session
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.E(oops.CodeUnauthorized, nil, "authentication required").Log(ctx, s.logger)
	}

	issuer := r.URL.Query().Get("issuer")
	if issuer == "" {
		return oops.E(oops.CodeBadRequest, nil, "issuer is required").Log(ctx, s.logger)
	}

	// Delete token
	if err := s.oauthRepo.DeleteUserOAuthTokenByIssuer(ctx, repo.DeleteUserOAuthTokenByIssuerParams{
		UserID:            authCtx.UserID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		OauthServerIssuer: issuer,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to disconnect OAuth").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "OAuth token disconnected",
		attr.SlogUserID(authCtx.UserID),
		attr.SlogOAuthIssuer(issuer))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to encode response", attr.SlogError(err))
	}
	return nil
}

// handleGetToken returns the decrypted access token for MCP requests
// This endpoint is called by Elements to get tokens for authenticated MCP connections
func (s *ExternalOAuthService) handleGetToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Get user session
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.E(oops.CodeUnauthorized, nil, "authentication required").Log(ctx, s.logger)
	}

	issuer := r.URL.Query().Get("issuer")
	if issuer == "" {
		return oops.E(oops.CodeBadRequest, nil, "issuer is required").Log(ctx, s.logger)
	}

	// Get stored token
	token, err := s.oauthRepo.GetUserOAuthToken(ctx, repo.GetUserOAuthTokenParams{
		UserID:            authCtx.UserID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		OauthServerIssuer: issuer,
	})
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "no OAuth token found for this issuer").Log(ctx, s.logger)
	}

	// Check if token is expired
	if token.ExpiresAt.Valid && time.Now().After(token.ExpiresAt.Time) {
		// Token is expired, try to refresh if we have a refresh token
		if token.RefreshTokenEncrypted.Valid && token.RefreshTokenEncrypted.String != "" {
			// TODO: Implement token refresh using the refresh token
			// For now, return an error indicating refresh is needed
			return oops.E(oops.CodeUnauthorized, nil, "token expired, re-authentication required").Log(ctx, s.logger)
		}
		return oops.E(oops.CodeUnauthorized, nil, "token expired, re-authentication required").Log(ctx, s.logger)
	}

	// Decrypt access token
	accessTokenBytes, err := s.enc.Decrypt(token.AccessTokenEncrypted)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to decrypt access token").Log(ctx, s.logger)
	}

	// Return the token response
	response := map[string]interface{}{
		"access_token": string(accessTokenBytes),
		"token_type":   token.TokenType,
	}

	if token.ExpiresAt.Valid {
		response["expires_at"] = token.ExpiresAt.Time.Unix()
	}

	if token.Scope.Valid {
		response["scope"] = token.Scope.String
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.ErrorContext(ctx, "failed to encode response", attr.SlogError(err))
	}
	return nil
}

// ExternalOAuthConfig contains the OAuth configuration for an external MCP server
type ExternalOAuthConfig struct {
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	ScopesSupported       []string
	ClientID              string
	ClientSecret          string
	ProviderName          string
}

// getExternalOAuthConfig extracts OAuth configuration from a toolset's external MCP tools
func (s *ExternalOAuthService) getExternalOAuthConfig(ctx context.Context, toolset toolsets_repo.Toolset) (*ExternalOAuthConfig, error) {
	// TODO: Implement actual lookup from external_mcp_tool_definitions
	// For now, return an error indicating OAuth is not configured
	// This will be implemented when we have the full toolset -> external MCP relationship
	return nil, fmt.Errorf("external OAuth not configured for toolset")
}

// ExternalTokenResponse represents the response from an external OAuth token endpoint
type ExternalTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// exchangeCodeForTokens exchanges an authorization code for tokens with the external provider
func (s *ExternalOAuthService) exchangeCodeForTokens(
	ctx context.Context,
	config *ExternalOAuthConfig,
	code string,
	redirectURI string,
	codeVerifier string,
) (*ExternalTokenResponse, error) {
	// Build token request
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("client_id", config.ClientID)
	if config.ClientSecret != "" {
		data.Set("client_secret", config.ClientSecret)
	}
	data.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.logger.WarnContext(ctx, "failed to close response body", attr.SlogError(err))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		s.logger.ErrorContext(ctx, "token exchange failed",
			attr.SlogHTTPResponseStatusCode(resp.StatusCode),
			attr.SlogHTTPRequestBody(string(body)))
		return nil, fmt.Errorf("token exchange failed: HTTP %d", resp.StatusCode)
	}

	var tokenResp ExternalTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if tokenResp.TokenType == "" {
		tokenResp.TokenType = "Bearer"
	}

	return &tokenResp, nil
}

// redirectWithError redirects to the redirect_uri with error parameters
func (s *ExternalOAuthService) redirectWithError(w http.ResponseWriter, r *http.Request, redirectURI, errorCode, errorDesc string) error {
	if redirectURI == "" {
		// No redirect URI, return JSON error
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"error":             errorCode,
			"error_description": errorDesc,
		}); err != nil {
			return fmt.Errorf("encode error response: %w", err)
		}
		return nil
	}

	parsed, err := url.Parse(redirectURI)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "invalid redirect URI").Log(r.Context(), s.logger)
	}

	params := parsed.Query()
	params.Set("error", errorCode)
	params.Set("error_description", errorDesc)
	parsed.RawQuery = params.Encode()

	http.Redirect(w, r, parsed.String(), http.StatusFound)
	return nil
}

// generateCodeVerifier generates a random PKCE code verifier
func generateCodeVerifier() (string, error) {
	// Generate 32 bytes of random data (will be 43 chars when base64url encoded)
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// generateCodeChallenge generates a PKCE code challenge using S256 method
func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// generateStateID generates a random state ID for the OAuth flow
func generateStateID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
