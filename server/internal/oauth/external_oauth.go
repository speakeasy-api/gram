package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
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
	"github.com/jackc/pgx/v5/pgxpool"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	externalmcp_repo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

//go:embed hosted_external_oauth_success_page.html.tmpl
var externalOAuthSuccessPageTmplData string

//go:embed hosted_external_oauth_success_page.css
var externalOAuthSuccessStyleData []byte

//go:embed hosted_external_oauth_success_script.js
var externalOAuthSuccessScriptData []byte

// externalOAuthSuccessPageData is the template data for the external OAuth success page.
type externalOAuthSuccessPageData struct {
	ProviderName string
	ScriptHash   string
	StyleHash    string
}

// ExternalOAuthState represents the state stored during external OAuth flow
type ExternalOAuthState struct {
	// User context
	UserID         string    `json:"user_id"`
	OrganizationID string    `json:"organization_id"`
	ProjectID      uuid.UUID `json:"project_id,omitempty"`

	// OAuth flow context
	ToolsetID       uuid.UUID `json:"toolset_id"`
	RedirectURI     string    `json:"redirect_uri"`
	CodeVerifier    string    `json:"code_verifier"`
	StateID         string    `json:"state_id"` // Random ID used as the OAuth state parameter and cache key
	ExternalMCPSlug string    `json:"external_mcp_slug,omitempty"`

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
	// Use the state ID as the cache key (matches what's sent to OAuth provider)
	return ExternalOAuthStateCacheKey(s.StateID)
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
	logger            *slog.Logger
	oauthRepo         *repo.Queries
	toolsetsRepo      *toolsets_repo.Queries
	deploymentsRepo   *deployments_repo.Queries
	externalmcpRepo   *externalmcp_repo.Queries
	projectsRepo      *projects_repo.Queries
	stateStorage      cache.TypedCacheObject[ExternalOAuthState]
	serverURL         *url.URL
	auth              *auth.Auth
	enc               *encryption.Client
	httpClient        *http.Client
	successPageTmpl   *template.Template
	successScriptHash string
	successScriptData []byte
	successStyleHash  string
	successStyleData  []byte
}

// NewExternalOAuthService creates a new ExternalOAuthService
func NewExternalOAuthService(
	logger *slog.Logger,
	db *pgxpool.Pool,
	cacheImpl cache.Cache,
	serverURL *url.URL,
	auth *auth.Auth,
	enc *encryption.Client,
) *ExternalOAuthService {
	stateStorage := cache.NewTypedObjectCache[ExternalOAuthState](
		logger.With(attr.SlogCacheNamespace("external_oauth_state")),
		cacheImpl,
		cache.SuffixNone,
	)

	successPageTmpl := template.Must(template.New("external_oauth_success").Parse(externalOAuthSuccessPageTmplData))

	scriptHash := sha256.Sum256(externalOAuthSuccessScriptData)
	scriptHashStr := hex.EncodeToString(scriptHash[:])[:8]

	styleHash := sha256.Sum256(externalOAuthSuccessStyleData)
	styleHashStr := hex.EncodeToString(styleHash[:])[:8]

	return &ExternalOAuthService{
		logger:            logger.With(attr.SlogComponent("external_oauth")),
		oauthRepo:         repo.New(db),
		toolsetsRepo:      toolsets_repo.New(db),
		deploymentsRepo:   deployments_repo.New(db),
		externalmcpRepo:   externalmcp_repo.New(db),
		projectsRepo:      projects_repo.New(db),
		stateStorage:      stateStorage,
		serverURL:         serverURL,
		auth:              auth,
		enc:               enc,
		httpClient:        retryablehttp.NewClient().StandardClient(),
		successPageTmpl:   successPageTmpl,
		successScriptHash: scriptHashStr,
		successScriptData: externalOAuthSuccessScriptData,
		successStyleHash:  styleHashStr,
		successStyleData:  externalOAuthSuccessStyleData,
	}
}

// AttachExternalOAuth attaches external OAuth endpoints to the router.
// These endpoints use the /oauth-external prefix to avoid route conflicts
// with the MCP OAuth proxy endpoints at /oauth/{mcpSlug}/*.
func AttachExternalOAuth(mux goahttp.Muxer, service *ExternalOAuthService) {
	// External OAuth authorization endpoint - initiates OAuth flow with external provider
	o11y.AttachHandler(mux, "GET", "/oauth-external/authorize", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.withAuth(service.handleExternalAuthorize)).ServeHTTP(w, r)
	})

	// Disconnect OAuth connection
	o11y.AttachHandler(mux, "DELETE", "/oauth-external/disconnect", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.withAuth(service.handleExternalDisconnect)).ServeHTTP(w, r)
	})

	// Check OAuth connection status for a toolset (query params: toolset_id, issuer)
	o11y.AttachHandler(mux, "GET", "/oauth-external/status", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.withAuth(service.handleExternalStatus)).ServeHTTP(w, r)
	})

	// External OAuth callback - handles callback from external provider
	o11y.AttachHandler(mux, "GET", "/oauth-external/callback", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleExternalCallback).ServeHTTP(w, r)
	})

	// External OAuth success page static assets (with cache busting hash)
	o11y.AttachHandler(mux, "GET", "/oauth-external/oauth_success-{hash}.js", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.serveExternalSuccessScript).ServeHTTP(w, r)
	})
	o11y.AttachHandler(mux, "GET", "/oauth-external/oauth_success-{hash}.css", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.serveExternalSuccessStyle).ServeHTTP(w, r)
	})
}

func (s *ExternalOAuthService) withAuth(h func(w http.ResponseWriter, r *http.Request) error) func(w http.ResponseWriter, r *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()

		sessionToken, ok := contextvalues.GetSessionTokenFromContext(ctx)
		if !ok {
			return oops.E(oops.CodeUnauthorized, nil, "authentication required").Log(ctx, s.logger)
		}

		// ctx, err = s.sessionManager.Authenticate(ctx, cookie.Value, false)
		ctx, err := s.auth.Authorize(ctx, sessionToken, &security.APIKeyScheme{
			Name:           constants.SessionSecurityScheme,
			Scopes:         []string{},
			RequiredScopes: []string{},
		})
		if err != nil {
			return err
		}

		projectSlug := r.Header.Get(constants.ProjectHeader)
		if projectSlug == "" {
			projectSlug = r.URL.Query().Get("project")
		}

		if projectSlug == "" {
			return oops.E(oops.CodeBadRequest, nil, "project is required").Log(ctx, s.logger)
		}

		ctx, err = s.auth.Authorize(ctx, projectSlug, &security.APIKeyScheme{
			Name:           constants.ProjectSlugSecuritySchema,
			Scopes:         []string{},
			RequiredScopes: []string{},
		})
		if err != nil {
			return err
		}

		return h(w, r.WithContext(ctx))
	}
}

// handleExternalAuthorize initiates the OAuth flow with an external provider
func (s *ExternalOAuthService) handleExternalAuthorize(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "authentication required").Log(ctx, s.logger)
	}

	// Parse query parameters
	toolsetIDStr := r.URL.Query().Get("toolset_id")
	if toolsetIDStr == "" {
		return oops.E(oops.CodeBadRequest, nil, "toolset_id is required").Log(ctx, s.logger)
	}

	redirectURI := r.URL.Query().Get("redirect_uri")
	if redirectURI == "" {
		return oops.E(oops.CodeBadRequest, nil, "redirect_uri is required").Log(ctx, s.logger)
	}

	externalMCPSlug := r.URL.Query().Get("external_mcp_slug")

	// Validate redirect_uri origin matches server origin to prevent open redirects.
	// In local dev the frontend and backend run on different ports, so we only
	// compare the hostname (without port) when the server is on localhost.
	parsedRedirect, err := url.Parse(redirectURI)
	if err != nil || parsedRedirect.Scheme == "" || parsedRedirect.Host == "" {
		return oops.E(oops.CodeBadRequest, nil, "invalid redirect_uri").Log(ctx, s.logger)
	}
	serverHost := s.serverURL.Hostname()
	redirectHost := parsedRedirect.Hostname()
	if serverHost == "localhost" || serverHost == "127.0.0.1" {
		// Local dev: allow any localhost redirect regardless of port
		if redirectHost != "localhost" && redirectHost != "127.0.0.1" {
			return oops.E(oops.CodeBadRequest, nil, "redirect_uri must be localhost in local mode").Log(ctx, s.logger)
		}
	} else {
		// Prod/dev: strict origin match (scheme + host + port)
		if parsedRedirect.Scheme != s.serverURL.Scheme || parsedRedirect.Host != s.serverURL.Host {
			return oops.E(oops.CodeBadRequest, nil, "redirect_uri origin must match server origin").Log(ctx, s.logger)
		}
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
	oauthConfig, err := s.getExternalOAuthConfig(ctx, toolset, externalMCPSlug)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "toolset does not require OAuth or external MCP not found").Log(ctx, s.logger)
	}

	// Get or register OAuth client via DCR (for MCP OAuth 2.1)
	oauthClient, err := s.getOrRegisterClient(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, oauthConfig)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to register OAuth client").Log(ctx, s.logger)
	}
	// Update the config with the obtained client_id
	oauthConfig.ClientID = oauthClient.ClientID

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

	// Build authorization URL
	authURL, err := url.Parse(oauthConfig.AuthorizationEndpoint)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "invalid authorization endpoint").Log(ctx, s.logger)
	}

	// Create state object
	state := ExternalOAuthState{
		UserID:            authCtx.UserID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         *authCtx.ProjectID,
		ToolsetID:         toolsetID,
		RedirectURI:       redirectURI,
		CodeVerifier:      codeVerifier,
		StateID:           stateID, // Store the state ID for cache key consistency
		ExternalMCPSlug:   externalMCPSlug,
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

	callbackURL := fmt.Sprintf("%s/oauth-external/callback", s.serverURL.String())

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

	oauthConfig, err := s.getExternalOAuthConfig(ctx, toolset, state.ExternalMCPSlug)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get OAuth config").Log(ctx, s.logger)
	}

	// Get the registered client credentials for token exchange
	oauthClient, err := s.getOrRegisterClient(ctx, state.OrganizationID, state.ProjectID, oauthConfig)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get OAuth client credentials").Log(ctx, s.logger)
	}
	oauthConfig.ClientID = oauthClient.ClientID
	oauthConfig.ClientSecret = oauthClient.ClientSecret

	// Exchange code for tokens
	callbackURL := fmt.Sprintf("%s/oauth-external/callback", s.serverURL.String())
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
		ProjectID:             state.ProjectID,
		ClientRegistrationID:  oauthClient.RegistrationID,
		ToolsetID:             toolset.ID,
		OauthServerIssuer:     state.OAuthServerIssuer,
		AccessTokenEncrypted:  accessTokenEncrypted,
		RefreshTokenEncrypted: refreshTokenEncrypted,
		TokenType:             conv.ToPGText("bearer"),
		ExpiresAt:             expiresAt,
		Scopes:                strings.Fields(tokenResp.Scope),
		ProviderName:          conv.ToPGText(state.ProviderName),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to store OAuth token").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "external OAuth token stored successfully",
		attr.SlogUserID(state.UserID),
		attr.SlogOAuthIssuer(state.OAuthServerIssuer))

	// Return a success page that auto-closes the popup
	// The parent window polls for popup close and refetches status
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := externalOAuthSuccessPageData{
		ProviderName: state.ProviderName,
		ScriptHash:   s.successScriptHash,
		StyleHash:    s.successStyleHash,
	}
	if err := s.successPageTmpl.Execute(w, data); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to render external OAuth success page").Log(ctx, s.logger)
	}
	return nil
}

// serveExternalSuccessScript serves the external OAuth success page JavaScript file with cache headers.
func (s *ExternalOAuthService) serveExternalSuccessScript(w http.ResponseWriter, r *http.Request) error {
	hash := chi.URLParam(r, "hash")
	if hash != s.successScriptHash {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(s.successScriptData); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write script response").Log(r.Context(), s.logger)
	}
	return nil
}

// serveExternalSuccessStyle serves the external OAuth success page CSS file with cache headers.
func (s *ExternalOAuthService) serveExternalSuccessStyle(w http.ResponseWriter, r *http.Request) error {
	hash := chi.URLParam(r, "hash")
	if hash != s.successStyleHash {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(s.successStyleData); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write style response").Log(r.Context(), s.logger)
	}
	return nil
}

type handleExternalStatusResponse struct {
	Status       string  `json:"status"`
	ExpiresAt    *string `json:"expires_at,omitempty"`
	ProviderName *string `json:"provider_name,omitempty"`
}

// handleExternalStatus checks if the user has a valid OAuth token for an OAuth issuer
func (s *ExternalOAuthService) handleExternalStatus(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Get user session - try context first, then Gram-Session header
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "authentication required").Log(ctx, s.logger)
	}

	// Parse query parameters - issuer is required for status check
	toolsetIDStr := r.URL.Query().Get("toolset_id")
	issuer := r.URL.Query().Get("issuer")

	if toolsetIDStr == "" {
		return oops.E(oops.CodeBadRequest, nil, "toolset_id is required").Log(ctx, s.logger)
	}
	if issuer == "" {
		return oops.E(oops.CodeBadRequest, nil, "issuer is required").Log(ctx, s.logger)
	}

	toolsetID, err := uuid.Parse(toolsetIDStr)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid toolset_id").Log(ctx, s.logger)
	}

	// Load toolset to verify user has access
	toolset, err := s.toolsetsRepo.GetToolsetByID(ctx, toolsetID)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	// Verify user has access
	if toolset.OrganizationID != authCtx.ActiveOrganizationID {
		return oops.E(oops.CodeForbidden, nil, "access denied").Log(ctx, s.logger)
	}

	// Check if user has a token for this OAuth server (issuer)
	s.logger.InfoContext(ctx, "checking OAuth token status",
		attr.SlogUserID(authCtx.UserID),
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogOAuthIssuer(issuer))

	token, err := s.oauthRepo.GetUserOAuthToken(ctx, repo.GetUserOAuthTokenParams{
		UserID:            authCtx.UserID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		OauthServerIssuer: issuer,
	})

	if err != nil {
		s.logger.InfoContext(ctx, "no OAuth token found", attr.SlogError(err))
	}

	connected := err == nil
	var expired bool
	if connected && token.ExpiresAt.Valid {
		expired = time.Now().After(token.ExpiresAt.Time)
	}

	// Determine status based on connection state
	status := "needs_auth"
	if connected && !expired {
		status = "authenticated"
	} else if connected && expired {
		status = "disconnected" // Token exists but expired
	}

	response := handleExternalStatusResponse{
		Status:       status,
		ExpiresAt:    nil,
		ProviderName: nil,
	}

	if connected {
		response.ExpiresAt = conv.Ptr(token.ExpiresAt.Time.Format(time.RFC3339))
		if token.ProviderName.Valid {
			response.ProviderName = conv.Ptr(token.ProviderName.String)
		}
	}

	s.logger.InfoContext(ctx, "OAuth status check completed",
		attr.SlogOAuthStatus(status),
		attr.SlogOAuthConnected(connected),
		attr.SlogOAuthExpired(expired))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.ErrorContext(ctx, "failed to encode response", attr.SlogError(err))
	}
	return nil
}

// handleExternalDisconnect removes an OAuth token for a toolset
func (s *ExternalOAuthService) handleExternalDisconnect(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Get user session - try context first, then Gram-Session header
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
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
	if err := json.NewEncoder(w).Encode(map[string]any{
		"success": true,
	}); err != nil {
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

// getExternalOAuthConfig extracts OAuth configuration from a toolset's external MCP tools.
// If externalMCPSlug is provided, it filters by that specific MCP attachment.
// Otherwise, it returns the first OAuth-requiring tool found.
func (s *ExternalOAuthService) getExternalOAuthConfig(ctx context.Context, toolset toolsets_repo.Toolset, externalMCPSlug string) (*ExternalOAuthConfig, error) {
	// Get the active deployment for this toolset's project
	deploymentID, err := s.deploymentsRepo.GetActiveDeploymentID(ctx, toolset.ProjectID)
	if err != nil {
		s.logger.DebugContext(ctx, "no active deployment found for toolset",
			attr.SlogToolsetID(toolset.ID.String()),
			attr.SlogError(err))
		return nil, fmt.Errorf("no active deployment for toolset: %w", err)
	}

	// Query external MCP tools that require OAuth
	oauthTools, err := s.externalmcpRepo.GetExternalMCPToolsRequiringOAuth(ctx, deploymentID)
	if err != nil {
		s.logger.DebugContext(ctx, "failed to query OAuth-requiring MCP tools",
			attr.SlogDeploymentID(deploymentID.String()),
			attr.SlogError(err))
		return nil, fmt.Errorf("failed to query OAuth tools: %w", err)
	}

	if len(oauthTools) == 0 {
		return nil, fmt.Errorf("no OAuth-requiring external MCP tools found")
	}

	// Find the matching tool (by slug if provided, or first one)
	var matchedTool *externalmcp_repo.GetExternalMCPToolsRequiringOAuthRow
	for i, tool := range oauthTools {
		if externalMCPSlug == "" || tool.Slug == externalMCPSlug {
			matchedTool = &oauthTools[i]
			break
		}
	}

	if matchedTool == nil {
		return nil, fmt.Errorf("external MCP tool with slug %q not found", externalMCPSlug)
	}

	// Validate required OAuth endpoints
	if !matchedTool.OauthAuthorizationEndpoint.Valid || matchedTool.OauthAuthorizationEndpoint.String == "" {
		return nil, fmt.Errorf("OAuth authorization endpoint not configured for %s", matchedTool.Slug)
	}
	if !matchedTool.OauthTokenEndpoint.Valid || matchedTool.OauthTokenEndpoint.String == "" {
		return nil, fmt.Errorf("OAuth token endpoint not configured for %s", matchedTool.Slug)
	}

	// Derive issuer from authorization endpoint (use the origin)
	authURL, err := url.Parse(matchedTool.OauthAuthorizationEndpoint.String)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization endpoint URL: %w", err)
	}
	issuer := fmt.Sprintf("%s://%s", authURL.Scheme, authURL.Host)

	// Build registration endpoint (for DCR)
	registrationEndpoint := ""
	if matchedTool.OauthRegistrationEndpoint.Valid {
		registrationEndpoint = matchedTool.OauthRegistrationEndpoint.String
	}

	// Note: ClientID and ClientSecret will be populated via DCR or manual configuration
	// For MCP OAuth 2.1, we typically use Dynamic Client Registration
	config := &ExternalOAuthConfig{
		Issuer:                issuer,
		AuthorizationEndpoint: matchedTool.OauthAuthorizationEndpoint.String,
		TokenEndpoint:         matchedTool.OauthTokenEndpoint.String,
		RegistrationEndpoint:  registrationEndpoint,
		ScopesSupported:       matchedTool.OauthScopesSupported,
		ClientID:              "", // Populated via DCR
		ClientSecret:          "", // Populated via DCR
		ProviderName:          matchedTool.Name.String,
	}

	s.logger.DebugContext(ctx, "found OAuth config for external MCP tool",
		attr.SlogExternalMCPSlug(matchedTool.Slug),
		attr.SlogOAuthIssuer(issuer),
		attr.SlogOAuthVersion(matchedTool.OauthVersion))

	return config, nil
}

// DCRRequest represents the Dynamic Client Registration request per RFC 7591
type DCRRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// DCRResponse represents the Dynamic Client Registration response per RFC 7591
type DCRResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
}

type getOrRegisterClientResult struct {
	RegistrationID uuid.UUID
	ClientID       string
	ClientSecret   string
}

// getOrRegisterClient retrieves existing client credentials or performs DCR to get new ones.
// This is used for MCP OAuth 2.1 which requires Dynamic Client Registration.
func (s *ExternalOAuthService) getOrRegisterClient(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	oauthConfig *ExternalOAuthConfig,
) (_ *getOrRegisterClientResult, err error) {
	// First, check if we already have a registered client for this org/issuer
	existing, err := s.oauthRepo.GetExternalOAuthClientRegistration(ctx, repo.GetExternalOAuthClientRegistrationParams{
		OrganizationID:    organizationID,
		OauthServerIssuer: oauthConfig.Issuer,
	})
	if err == nil {
		// Check if the client secret has expired
		if existing.ClientSecretExpiresAt.Valid && existing.ClientSecretExpiresAt.Time.Before(time.Now()) {
			s.logger.InfoContext(ctx, "client secret expired, re-registering",
				attr.SlogOAuthIssuer(oauthConfig.Issuer))
		} else {
			// Valid existing registration
			clientSecret := ""
			if existing.ClientSecretEncrypted.Valid && existing.ClientSecretEncrypted.String != "" {
				decrypted, decryptErr := s.enc.Decrypt(existing.ClientSecretEncrypted.String)
				if decryptErr != nil {
					s.logger.WarnContext(ctx, "failed to decrypt client secret, re-registering",
						attr.SlogError(decryptErr))
				} else {
					clientSecret = decrypted
				}
			}
			if clientSecret != "" || !existing.ClientSecretEncrypted.Valid {
				s.logger.DebugContext(ctx, "using existing client registration",
					attr.SlogOAuthClientID(existing.ClientID),
					attr.SlogOAuthIssuer(oauthConfig.Issuer))

				result := &getOrRegisterClientResult{
					RegistrationID: existing.ID,
					ClientID:       existing.ClientID,
					ClientSecret:   clientSecret,
				}

				return result, nil
			}
		}
	}

	// No valid registration exists, perform DCR
	if oauthConfig.RegistrationEndpoint == "" {
		return nil, fmt.Errorf("no registration endpoint configured for OAuth server %s", oauthConfig.Issuer)
	}

	// Build DCR request
	callbackURL := fmt.Sprintf("%s/oauth-external/callback", s.serverURL.String())
	dcrReq := DCRRequest{
		RedirectURIs:            []string{callbackURL},
		TokenEndpointAuthMethod: "none", // Public client with PKCE
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Gram",
		ClientURI:               s.serverURL.String(),
		Scope:                   strings.Join(oauthConfig.ScopesSupported, " "),
	}

	reqBody, err := json.Marshal(dcrReq)
	if err != nil {
		return nil, fmt.Errorf("marshal DCR request: %w", err)
	}

	s.logger.InfoContext(ctx, "performing dynamic client registration",
		attr.SlogOAuthRegistrationEndpoint(oauthConfig.RegistrationEndpoint),
		attr.SlogOAuthIssuer(oauthConfig.Issuer))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthConfig.RegistrationEndpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create DCR request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DCR request failed: %w", err)
	}

	defer o11y.LogDefer(ctx, s.logger, func() error {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return fmt.Errorf("failed to close DCR response body: %s", attr.SlogError(closeErr))
		}
		return nil
	})

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read DCR response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		s.logger.ErrorContext(ctx, "DCR failed",
			attr.SlogHTTPResponseStatusCode(resp.StatusCode),
			attr.SlogHTTPRequestBody(string(body)))
		return nil, fmt.Errorf("DCR failed with status %d: %s", resp.StatusCode, string(body))
	}

	var dcrResp DCRResponse
	if err := json.Unmarshal(body, &dcrResp); err != nil {
		return nil, fmt.Errorf("unmarshal DCR response: %w", err)
	}

	if dcrResp.ClientID == "" {
		return nil, fmt.Errorf("DCR response missing client_id")
	}

	// Encrypt client secret if present
	var encryptedSecret *string
	if dcrResp.ClientSecret != "" {
		encrypted, encErr := s.enc.Encrypt([]byte(dcrResp.ClientSecret))
		if encErr != nil {
			return nil, fmt.Errorf("encrypt client secret: %w", encErr)
		}
		encryptedSecret = &encrypted
	}

	// Convert timestamps
	var issuedAt, expiresAt *time.Time
	if dcrResp.ClientIDIssuedAt > 0 {
		t := time.Unix(dcrResp.ClientIDIssuedAt, 0)
		issuedAt = &t
	}
	if dcrResp.ClientSecretExpiresAt > 0 {
		t := time.Unix(dcrResp.ClientSecretExpiresAt, 0)
		expiresAt = &t
	}

	// Store the registration
	storedRegistration, err := s.oauthRepo.UpsertExternalOAuthClientRegistration(ctx, repo.UpsertExternalOAuthClientRegistrationParams{
		OrganizationID:        organizationID,
		ProjectID:             projectID,
		OauthServerIssuer:     oauthConfig.Issuer,
		ClientID:              dcrResp.ClientID,
		ClientSecretEncrypted: conv.PtrToPGTextEmpty(encryptedSecret),
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: conv.PtrValOr(issuedAt, time.Time{}), Valid: issuedAt != nil, InfinityModifier: pgtype.Finite},
		ClientSecretExpiresAt: pgtype.Timestamptz{Time: conv.PtrValOr(expiresAt, time.Time{}), Valid: expiresAt != nil, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		return nil, fmt.Errorf("store client registration: %w", err)
	}

	s.logger.InfoContext(ctx, "dynamic client registration successful",
		attr.SlogOAuthClientID(dcrResp.ClientID),
		attr.SlogOAuthIssuer(oauthConfig.Issuer))

	result := &getOrRegisterClientResult{
		RegistrationID: storedRegistration.ID,
		ClientID:       dcrResp.ClientID,
		ClientSecret:   dcrResp.ClientSecret,
	}

	return result, nil
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

	defer o11y.LogDefer(ctx, s.logger, func() error {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return fmt.Errorf("failed to close token response body: %s", attr.SlogError(closeErr))
		}
		return nil
	})

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

	// Defense-in-depth: validate redirect URI origin matches server origin.
	// Fall back to JSON error response if validation fails.
	serverHost := s.serverURL.Hostname()
	redirectHost := parsed.Hostname()
	if serverHost == "localhost" || serverHost == "127.0.0.1" {
		if redirectHost != "localhost" && redirectHost != "127.0.0.1" {
			s.logger.WarnContext(r.Context(), "redirectWithError: redirect_uri origin mismatch, returning JSON error instead",
				attr.SlogOAuthRedirectURIFull(redirectURI))
			return s.redirectWithError(w, r, "", errorCode, errorDesc)
		}
	} else {
		if parsed.Scheme != s.serverURL.Scheme || parsed.Host != s.serverURL.Host {
			s.logger.WarnContext(r.Context(), "redirectWithError: redirect_uri origin mismatch, returning JSON error instead",
				attr.SlogOAuthRedirectURIFull(redirectURI))
			return s.redirectWithError(w, r, "", errorCode, errorDesc)
		}
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
