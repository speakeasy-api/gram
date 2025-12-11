package oauth

import (
	"bytes"
	"context"
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
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	logger            *slog.Logger
	tracer            trace.Tracer
	meter             metric.Meter
	db                *pgxpool.Pool
	toolsetsRepo      *toolsets_repo.Queries
	customDomainsRepo *customdomains_repo.Queries
	environments      *environments.EnvironmentEntries
	serverURL         *url.URL

	clientRegistration *ClientRegistrationService
	grantManager       *GrantManager
	tokenService       *TokenService
	pkceService        *PKCEService
	oauthRepo          *repo.Queries
	enc                *encryption.Client
}

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, serverURL *url.URL, cacheImpl cache.Cache, enc *encryption.Client, env *environments.EnvironmentEntries) *Service {
	logger = logger.With(attr.SlogComponent("oauth"))
	tracer := tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/oauth")
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/oauth")

	clientRegistration := NewClientRegistrationService(cacheImpl, logger)
	pkceService := NewPKCEService(logger)
	grantManager := NewGrantManager(cacheImpl, clientRegistration, pkceService, logger, enc)
	tokenService := NewTokenService(cacheImpl, clientRegistration, grantManager, pkceService, logger, enc)

	return &Service{
		logger:            logger,
		tracer:            tracer,
		meter:             meter,
		db:                db,
		toolsetsRepo:      toolsets_repo.New(db),
		customDomainsRepo: customdomains_repo.New(db),
		environments:      env,
		serverURL:         serverURL,

		// OAuth 2.1 components
		clientRegistration: clientRegistration,
		grantManager:       grantManager,
		tokenService:       tokenService,
		pkceService:        pkceService,
		oauthRepo:          repo.New(db),
		enc:                enc,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	// OAuth 2.1 Dynamic Client Registration
	o11y.AttachHandler(mux, "POST", "/oauth/{mcpSlug}/register", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleClientRegistration).ServeHTTP(w, r)
	})

	// OAuth 2.1 Authorization Endpoint
	o11y.AttachHandler(mux, "GET", "/oauth/{mcpSlug}/authorize", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleAuthorize).ServeHTTP(w, r)
	})

	// OAuth 2.1 Authorization Callback
	o11y.AttachHandler(mux, "GET", "/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleAuthorizationCallback).ServeHTTP(w, r)
	})

	// OAuth 2.1 Token Endpoint
	o11y.AttachHandler(mux, "POST", "/oauth/{mcpSlug}/token", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleToken).ServeHTTP(w, r)
	})
}

// loadToolsetFromCurrentURLContext loads a toolset from the MCP slug and returns the full MCP URL
func (s *Service) loadToolsetFromCurrentURLContext(ctx context.Context, mcpSlug string) (*toolsets_repo.Toolset, string, error) {
	var toolset toolsets_repo.Toolset
	var toolsetErr error
	var mcpURL string

	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil {
		toolset, toolsetErr = s.toolsetsRepo.GetToolsetByMcpSlugAndCustomDomain(ctx, toolsets_repo.GetToolsetByMcpSlugAndCustomDomainParams{
			McpSlug:        conv.ToPGText(mcpSlug),
			CustomDomainID: uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true},
		})
		mcpURL = fmt.Sprintf("https://%s/mcp/%s", domainCtx.Domain, mcpSlug)
	} else {
		toolset, toolsetErr = s.toolsetsRepo.GetToolsetByMcpSlug(ctx, conv.ToPGText(mcpSlug))
		mcpURL = s.serverURL.String() + "/mcp/" + mcpSlug
	}

	if toolsetErr != nil {
		return nil, "", oops.E(oops.CodeNotFound, toolsetErr, "mcp server not found").Log(ctx, s.logger)
	}

	if !toolset.McpIsPublic {
		return nil, "", oops.E(oops.CodeNotFound, nil, "mcp server not found").Log(ctx, s.logger)
	}

	return &toolset, mcpURL, nil
}

func (s *Service) loadToolsetForProjectAndMCPSlug(ctx context.Context, projectID uuid.UUID, mcpSlug string) (*toolsets_repo.Toolset, string, error) {
	toolset, err := s.toolsetsRepo.GetToolsetByMCPSlug(ctx, toolsets_repo.GetToolsetByMCPSlugParams{
		ProjectID: projectID,
		McpSlug:   conv.ToPGText(mcpSlug),
	})
	if err != nil {
		return nil, "", oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	mcpURL := fmt.Sprintf("%s/mcp/%s", s.serverURL.String(), mcpSlug)
	if toolset.CustomDomainID.Valid {
		domain, err := s.customDomainsRepo.GetCustomDomainByID(ctx, toolset.CustomDomainID.UUID)
		if err != nil {
			return nil, "", oops.E(oops.CodeNotFound, err, "custom domain not found").Log(ctx, s.logger)
		}
		mcpURL = fmt.Sprintf("https://%s/mcp/%s", domain.Domain, mcpSlug)
	}
	return &toolset, mcpURL, nil
}

// handleAuthorize handles OAuth 2.1 authorization requests
func (s *Service) handleAuthorize(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Extract MCP slug from URL path
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	// Load toolset from MCP slug
	toolset, fullMCPURL, err := s.loadToolsetFromCurrentURLContext(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	// Parse authorization request
	req := &AuthorizationRequest{
		ResponseType:        r.URL.Query().Get("response_type"),
		ClientID:            r.URL.Query().Get("client_id"),
		RedirectURI:         r.URL.Query().Get("redirect_uri"),
		Scope:               r.URL.Query().Get("scope"),
		State:               r.URL.Query().Get("state"),
		CodeChallenge:       r.URL.Query().Get("code_challenge"),
		CodeChallengeMethod: r.URL.Query().Get("code_challenge_method"),
		Nonce:               r.URL.Query().Get("nonce"),
	}

	// Set default code challenge method if not provided
	if req.CodeChallenge != "" && req.CodeChallengeMethod == "" {
		req.CodeChallengeMethod = "plain"
	}

	if err := s.validateAuthorizationRequest(ctx, req, fullMCPURL); err != nil {
		s.logger.ErrorContext(ctx, "invalid authorization request", attr.SlogError(err))

		// Return 401 with error details instead of redirecting
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		errorResponse := map[string]string{
			"error": "invalid_client",
		}
		if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
			s.logger.ErrorContext(ctx, "failed to encode error response", attr.SlogError(err))
		}
		return nil
	}

	// Get OAuth proxy providers for this toolset
	providers, err := s.oauthRepo.ListOAuthProxyProvidersByServer(ctx, repo.ListOAuthProxyProvidersByServerParams{
		OauthProxyServerID: toolset.OauthProxyServerID.UUID,
		ProjectID:          toolset.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "OAuth providers not configured").Log(ctx, s.logger)
	}
	if len(providers) == 0 {
		return oops.E(oops.CodeUnexpected, nil, "OAuth providers not configured").Log(ctx, s.logger)
	}

	// TODO: Eventually support multiple providers
	provider := providers[0]

	var secrets map[string]string
	if err := json.Unmarshal(provider.Secrets, &secrets); err != nil {
		return oops.E(oops.CodeUnexpected, err, "OAuth provider secrets invalid").Log(ctx, s.logger)
	}

	clientID := secrets["client_id"]

	// Fallback to environment if client_id is missing and environment is specified
	if clientID == "" && secrets["environment_slug"] != "" {
		envMap, err := s.environments.Load(ctx, toolset.ProjectID, gateway.Slug(secrets["environment_slug"]))
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to load environment").Log(ctx, s.logger)
		}

		for k, v := range envMap {
			if strings.ToLower(k) == "client_id" {
				clientID = v
			}
		}
	}

	if clientID == "" {
		return oops.E(oops.CodeUnexpected, nil, "OAuth provider client_id not configured").Log(ctx, s.logger)
	}

	// Prepare OAuth request info to encode in state parameter
	oauthReqInfo := map[string]string{
		"response_type":         req.ResponseType,
		"client_id":             req.ClientID,
		"redirect_uri":          req.RedirectURI,
		"scope":                 req.Scope,
		"state":                 req.State,
		"code_challenge":        req.CodeChallenge,
		"code_challenge_method": req.CodeChallengeMethod,
		"mcp_slug":              toolset.McpSlug.String,
		"project_id":            toolset.ProjectID.String(),
	}

	oauthReqInfoJSON, err := json.Marshal(oauthReqInfo)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to encode OAuth request info").Log(ctx, s.logger)
	}

	callbackURL := fmt.Sprintf("%s/oauth/callback", s.serverURL.String())

	authURL, err := url.Parse(provider.AuthorizationEndpoint.String)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to parse OAuth authorization URL").Log(ctx, s.logger)
	}

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", callbackURL)
	params.Set("response_type", "code")
	params.Set("state", string(oauthReqInfoJSON)) // Use the original OAuth request as state

	// We will recommend the provider configuration, fallback to request scope
	if len(provider.ScopesSupported) > 0 {
		params.Set("scope", strings.Join(provider.ScopesSupported, " "))
	} else {
		params.Set("scope", req.Scope)
	}

	authURL.RawQuery = params.Encode()

	s.logger.InfoContext(ctx, "redirecting to OAuth authorization",
		attr.SlogOAuthProvider(provider.Slug),
		attr.SlogOAuthRedirectURIFull(authURL.String()))

	// Redirect to underlying OAuth provider, this MUST be a 302
	http.Redirect(w, r, authURL.String(), http.StatusFound)
	return nil
}

// validateAuthorizationRequest validates an authorization request
func (s *Service) validateAuthorizationRequest(ctx context.Context, req *AuthorizationRequest, mcpURL string) error {
	return s.grantManager.ValidateAuthorizationRequest(ctx, req, mcpURL)
}

// handleToken handles OAuth 2.1 token requests
func (s *Service) handleToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, fullMCPURL, err := s.loadToolsetFromCurrentURLContext(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	if err := r.ParseForm(); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse form data").Log(ctx, s.logger)
	}

	// Extract client credentials based on authentication method
	clientID, clientSecret, err := s.extractClientCredentials(r)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid client authentication").Log(ctx, s.logger)
	}

	req := &TokenRequest{
		GrantType:    r.FormValue("grant_type"),
		Code:         r.FormValue("code"),
		RedirectURI:  r.FormValue("redirect_uri"),
		ClientID:     clientID,
		ClientSecret: clientSecret,
		CodeVerifier: r.FormValue("code_verifier"),
	}

	var token *Token

	switch req.GrantType {
	case "authorization_code":
		token, err = s.tokenService.ExchangeAuthorizationCode(ctx, req, fullMCPURL, toolset.ID)
	default:
		return oops.E(oops.CodeBadRequest, nil, "unsupported grant type: %s", req.GrantType).Log(ctx, s.logger)
	}

	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "token exchange failed").Log(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.tokenService.CreateTokenResponse(token)); err != nil {
		s.logger.ErrorContext(ctx, "failed to encode token response", attr.SlogError(err))
	}
	return nil
}

// handleClientRegistration handles OAuth 2.1 dynamic client registration
func (s *Service) handleClientRegistration(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	_, fullMcpURL, err := s.loadToolsetFromCurrentURLContext(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").Log(ctx, s.logger)
	}
	// Create a new reader for JSON decoding
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Parse JSON request
	var req ClientInfo
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid JSON in request body").Log(ctx, s.logger)
	}

	// Register client
	client, err := s.clientRegistration.RegisterClient(ctx, &req, fullMcpURL)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "client registration failed").Log(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(client); err != nil {
		s.logger.ErrorContext(ctx, "failed to encode client registration response", attr.SlogError(err))
	}
	return nil
}

// handleAuthorizationCallback handles the authorization completion callback
func (s *Service) handleAuthorizationCallback(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	stateParam := r.URL.Query().Get("state")
	if stateParam == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").Log(ctx, s.logger)
	}

	var oauthReqInfo map[string]string
	if err := json.Unmarshal([]byte(stateParam), &oauthReqInfo); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to decode OAuth request info from state").Log(ctx, s.logger)
	}

	mcpSlug := oauthReqInfo["mcp_slug"]
	projectIDStr := oauthReqInfo["project_id"]
	if mcpSlug == "" || projectIDStr == "" {
		return oops.E(oops.CodeBadRequest, nil, "mcp slug and project id is required in context").Log(ctx, s.logger)
	}

	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid project id").Log(ctx, s.logger)
	}

	toolset, fullMCPURL, err := s.loadToolsetForProjectAndMCPSlug(ctx, projectID, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	externalCode := r.URL.Query().Get("code")
	if externalCode == "" {
		return oops.E(oops.CodeBadRequest, nil, "code is required").Log(ctx, s.logger)
	}

	// Get OAuth proxy providers for this toolset
	providers, err := s.oauthRepo.ListOAuthProxyProvidersByServer(ctx, repo.ListOAuthProxyProvidersByServerParams{
		OauthProxyServerID: toolset.OauthProxyServerID.UUID,
		ProjectID:          toolset.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "OAuth providers not configured").Log(ctx, s.logger)
	}
	if len(providers) == 0 {
		return oops.E(oops.CodeUnexpected, nil, "OAuth providers not configured").Log(ctx, s.logger)
	}

	// TODO: Eventually support multiple providers
	provider := providers[0]

	var secrets map[string]string
	if err := json.Unmarshal(provider.Secrets, &secrets); err != nil {
		return oops.E(oops.CodeUnexpected, err, "OAuth provider secrets invalid").Log(ctx, s.logger)
	}

	clientID := secrets["client_id"]
	clientSecret := secrets["client_secret"]

	// Fallback to environment if credentials are missing and environment is specified
	if (clientID == "" || clientSecret == "") && secrets["environment_slug"] != "" {
		envMap, err := s.environments.Load(ctx, toolset.ProjectID, gateway.Slug(secrets["environment_slug"]))
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to load environment").Log(ctx, s.logger)
		}

		for k, v := range envMap {
			if clientID == "" && strings.ToLower(k) == "client_id" {
				clientID = v
			}
			if clientSecret == "" && strings.ToLower(k) == "client_secret" {
				clientSecret = v
			}
		}
	}

	if clientID == "" {
		return oops.E(oops.CodeUnexpected, nil, "OAuth provider client_id not configured").Log(ctx, s.logger)
	}
	if clientSecret == "" {
		return oops.E(oops.CodeUnexpected, nil, "OAuth provider client_secret not configured").Log(ctx, s.logger)
	}

	callbackURL := fmt.Sprintf("%s/oauth/callback", s.serverURL.String())

	tokenURL := provider.TokenEndpoint.String
	tokenData := url.Values{}
	tokenData.Set("grant_type", "authorization_code")
	tokenData.Set("redirect_uri", callbackURL)
	tokenData.Set("code", externalCode)

	// Determine authentication method based on provider configuration
	// Default to client_secret_post (form body) if TokenEndpointAuthMethodsSupported is empty
	useBasicAuth := false
	if len(provider.TokenEndpointAuthMethodsSupported) > 0 {
		// Check if provider supports client_secret_basic
		for _, method := range provider.TokenEndpointAuthMethodsSupported {
			if method == "client_secret_basic" {
				useBasicAuth = true
				break
			}
		}
	}

	// For Post Auth, client credentials go in form body
	if !useBasicAuth {
		tokenData.Set("client_id", clientID)
		tokenData.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(tokenData.Encode()))
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to create token request", attr.SlogOAuthProvider(provider.Slug), attr.SlogError(err))
		errorURL, _ := s.grantManager.BuildErrorResponse(ctx, oauthReqInfo["redirect_uri"], "server_error", "Failed to exchange authorization code", oauthReqInfo["state"])
		http.Redirect(w, r, errorURL, http.StatusFound)
		return nil
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if useBasicAuth {
		req.SetBasicAuth(clientID, clientSecret)
	}

	tokenResp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to exchange code for token", attr.SlogOAuthProvider(provider.Slug), attr.SlogError(err))
		errorURL, _ := s.grantManager.BuildErrorResponse(ctx, oauthReqInfo["redirect_uri"], "server_error", "Failed to exchange authorization code", oauthReqInfo["state"])
		http.Redirect(w, r, errorURL, http.StatusFound)
		return nil
	}
	defer func() {
		if err := tokenResp.Body.Close(); err != nil {
			s.logger.ErrorContext(ctx, "failed to close response body", attr.SlogError(err))
		}
	}()

	if tokenResp.StatusCode != http.StatusOK {
		s.logger.ErrorContext(ctx, "OAuth token exchange failed", attr.SlogOAuthProvider(provider.Slug), attr.SlogHTTPResponseStatusCode(tokenResp.StatusCode))
		errorURL, _ := s.grantManager.BuildErrorResponse(ctx, oauthReqInfo["redirect_uri"], "server_error", "Authorization code exchange failed", oauthReqInfo["state"])
		http.Redirect(w, r, errorURL, http.StatusFound)
		return nil
	}

	tokenRespBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to read OAuth token response", attr.SlogOAuthProvider(provider.Slug), attr.SlogError(err))
		errorURL, _ := s.grantManager.BuildErrorResponse(ctx, oauthReqInfo["redirect_uri"], "server_error", "Failed to read token response", oauthReqInfo["state"])
		http.Redirect(w, r, errorURL, http.StatusFound)
		return nil
	}

	var oauthTokenResp map[string]interface{}
	if err := json.Unmarshal(tokenRespBody, &oauthTokenResp); err != nil {
		s.logger.ErrorContext(ctx, "failed to parse OAuth token response", attr.SlogOAuthProvider(provider.Slug), attr.SlogError(err))
		errorURL, _ := s.grantManager.BuildErrorResponse(ctx, oauthReqInfo["redirect_uri"], "server_error", "Invalid token response", oauthReqInfo["state"])
		http.Redirect(w, r, errorURL, http.StatusFound)
		return nil
	}

	// Technically the OAuth spec does expect snake_case field names in the response but we will be generous to mistakes and try with camelCase
	accessToken, ok := oauthTokenResp["access_token"].(string)
	if !ok {
		// Retry with camelCase field name
		accessToken, ok = oauthTokenResp["accessToken"].(string)
		if !ok {
			s.logger.ErrorContext(ctx, "missing access_token in OAuth response", attr.SlogOAuthProvider(provider.Slug))
			errorURL, _ := s.grantManager.BuildErrorResponse(ctx, oauthReqInfo["redirect_uri"], "server_error", "Invalid token response", oauthReqInfo["state"])
			http.Redirect(w, r, errorURL, http.StatusFound)
			return nil
		}
	}

	var expiresAt *time.Time
	if expiresInFloat, ok := oauthTokenResp["expires_in"].(float64); ok {
		expiryTime := time.Now().Add(time.Duration(expiresInFloat) * time.Second)
		expiresAt = &expiryTime
	}
	if expiresInFloat, ok := oauthTokenResp["expiresIn"].(float64); ok {
		// Retry with camelCase field name
		expiryTime := time.Now().Add(time.Duration(expiresInFloat) * time.Second)
		expiresAt = &expiryTime
	}

	// Reconstruct the original authorization request from decoded state
	authReq := &AuthorizationRequest{
		ResponseType:        oauthReqInfo["response_type"],
		ClientID:            oauthReqInfo["client_id"],
		RedirectURI:         oauthReqInfo["redirect_uri"],
		Scope:               oauthReqInfo["scope"],
		State:               oauthReqInfo["state"],
		CodeChallenge:       oauthReqInfo["code_challenge"],
		CodeChallengeMethod: oauthReqInfo["code_challenge_method"],
		Nonce:               "", // Nonce is not preserved in state for this flow
	}

	grant, err := s.grantManager.CreateAuthorizationGrant(ctx, authReq, fullMCPURL, toolset.ID, accessToken, expiresAt, provider.SecurityKeyNames)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to create authorization grant", attr.SlogError(err))

		// Build error response
		errorURL, _ := s.grantManager.BuildErrorResponse(ctx, authReq.RedirectURI, "server_error", "Failed to create authorization grant", authReq.State)
		http.Redirect(w, r, errorURL, http.StatusFound)
		return nil
	}

	s.logger.InfoContext(ctx, "authorization grant created after external provider callback",
		attr.SlogOAuthClientID(authReq.ClientID),
		attr.SlogOAuthCode(grant.Code),
		attr.SlogOAuthExternalCode(externalCode))

	// Build authorization response and redirect back to client
	responseURL, err := s.grantManager.BuildAuthorizationResponse(ctx, grant, authReq.RedirectURI)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to build authorization response").Log(ctx, s.logger)
	}

	// Redirect back to client with the authorization code
	http.Redirect(w, r, responseURL, http.StatusFound)
	return nil
}

// extractClientCredentials extracts client credentials from the request
// Supports both client_secret_post and client_secret_basic authentication methods
func (s *Service) extractClientCredentials(r *http.Request) (string, string, error) {
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	if clientID != "" {
		return clientID, clientSecret, nil
	}

	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if clientID, clientSecret, ok := s.parseBasicAuth(authHeader); ok {
			return clientID, clientSecret, nil
		}
	}

	return "", "", fmt.Errorf("client_id is required")
}

// parseBasicAuth parses the Authorization header for Basic authentication
func (s *Service) parseBasicAuth(authHeader string) (string, string, bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(authHeader[len(prefix):])
	if err != nil {
		return "", "", false
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	return parts[0], parts[1], true
}

// ValidateAccessToken validates an OAuth access token
func (s *Service) ValidateAccessToken(ctx context.Context, toolsetId uuid.UUID, accessToken string) (*Token, error) {
	return s.tokenService.ValidateAccessToken(ctx, toolsetId, accessToken)
}
