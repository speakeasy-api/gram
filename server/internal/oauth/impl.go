package oauth

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	_ "embed"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/providers"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

//go:embed hosted_oauth_success_page.html.tmpl
var oauthSuccessPageTmplData string

//go:embed hosted_oauth_failure_page.html.tmpl
var oauthFailurePageTmplData string

//go:embed hosted_oauth_status_script.js
var oauthSuccessScriptData []byte

type gramOAuthResultPageData struct {
	RedirectURL template.URL
	ScriptHash  string

	// Error fields for failure page
	ErrorDescription string
	ErrorCode        string
}

type Service struct {
	logger                    *slog.Logger
	tracer                    trace.Tracer
	meter                     metric.Meter
	db                        *pgxpool.Pool
	toolsetsRepo              *toolsets_repo.Queries
	customDomainsRepo         *customdomains_repo.Queries
	environments              *environments.EnvironmentEntries
	serverURL                 *url.URL
	clientRegistration        *ClientRegistrationService
	grantManager              *GrantManager
	tokenService              *TokenService
	pkceService               *PKCEService
	oauthRepo                 *repo.Queries
	enc                       *encryption.Client
	sessions                  *sessions.Manager
	gramProvider              *providers.GramProvider
	customProvider            *providers.CustomProvider
	successPageTmpl           *template.Template
	failurePageTmpl           *template.Template
	oauthStatusPageScriptHash string
	oauthStatusPageScriptData []byte
}

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, serverURL *url.URL, cacheImpl cache.Cache, enc *encryption.Client, env *environments.EnvironmentEntries, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("oauth"))
	tracer := tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/oauth")
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/oauth")

	clientRegistration := NewClientRegistrationService(cacheImpl, logger)
	pkceService := NewPKCEService(logger)
	grantManager := NewGrantManager(cacheImpl, clientRegistration, pkceService, logger, enc)
	tokenService := NewTokenService(cacheImpl, clientRegistration, grantManager, pkceService, logger, enc)

	// Initialize OAuth providers
	gramProvider := providers.NewGramProvider(logger, sessions)
	customProvider := providers.NewCustomProvider(logger, env)

	// Parse templates once during initialization
	successPageTmpl := template.Must(template.New("oauth_success").Parse(oauthSuccessPageTmplData))
	failurePageTmpl := template.Must(template.New("oauth_failure").Parse(oauthFailurePageTmplData))

	// Calculate content hash for success script (for cache busting)
	hash := sha256.Sum256(oauthSuccessScriptData)
	scriptHash := hex.EncodeToString(hash[:])[:8] // Use first 8 chars like hosted page

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
		sessions:           sessions,

		// OAuth providers
		gramProvider:   gramProvider,
		customProvider: customProvider,

		// HTML templates
		successPageTmpl: successPageTmpl,
		failurePageTmpl: failurePageTmpl,

		// Success page script with hash for cache busting
		oauthStatusPageScriptHash: scriptHash,
		oauthStatusPageScriptData: oauthSuccessScriptData,
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

	// OAuth success page script (with cache busting hash)
	o11y.AttachHandler(mux, "GET", "/oauth/oauth_success-{hash}.js", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.serveSuccessScript).ServeHTTP(w, r)
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

	if !toolset.McpIsPublic && !toolset.OauthProxyServerID.Valid {
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
	availableProviders, err := s.oauthRepo.ListOAuthProxyProvidersByServer(ctx, repo.ListOAuthProxyProvidersByServerParams{
		OauthProxyServerID: toolset.OauthProxyServerID.UUID,
		ProjectID:          toolset.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "OAuth providers not configured").Log(ctx, s.logger)
	}
	if len(availableProviders) == 0 {
		return oops.E(oops.CodeUnexpected, nil, "OAuth providers not configured").Log(ctx, s.logger)
	}

	// TODO: Eventually support multiple providers
	provider := availableProviders[0]

	var clientID string

	if provider.ProviderType == string(OAuthProxyProviderTypeCustom) {
		var secrets map[string]string
		if err := json.Unmarshal(provider.Secrets, &secrets); err != nil {
			return oops.E(oops.CodeUnexpected, err, "OAuth provider secrets invalid").Log(ctx, s.logger)
		}

		clientID = secrets["client_id"]

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
		"nonce":                 req.Nonce,
		"mcp_slug":              toolset.McpSlug.String,
		"project_id":            toolset.ProjectID.String(),
	}

	oauthReqInfoJSON, err := json.Marshal(oauthReqInfo)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to encode OAuth request info").Log(ctx, s.logger)
	}

	callbackURL := fmt.Sprintf("%s/oauth/callback", s.serverURL.String())

	var authURL *url.URL

	switch provider.ProviderType {
	case string(OAuthProxyProviderTypeGram):
		authURL, err = s.sessions.BuildAuthorizationURL(ctx, sessions.AuthURLParams{
			CallbackURL:     callbackURL,
			Scope:           req.Scope,
			State:           string(oauthReqInfoJSON),
			ScopesSupported: provider.ScopesSupported,
			ClientID:        "",
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to build gram OAuth URL").Log(ctx, s.logger)
		}
	default:
		// For custom providers, build the URL directly
		authURL, err = url.Parse(provider.AuthorizationEndpoint.String)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to parse OAuth authorization URL").Log(ctx, s.logger)
		}

		urlParams := url.Values{}
		urlParams.Set("client_id", clientID)
		urlParams.Set("redirect_uri", callbackURL)
		urlParams.Set("response_type", "code")
		urlParams.Add("state", string(oauthReqInfoJSON))

		// We will recommend the provider configuration, fallback to request scope
		if len(provider.ScopesSupported) > 0 {
			urlParams.Set("scope", strings.Join(provider.ScopesSupported, " "))
		} else {
			urlParams.Set("scope", req.Scope)
		}

		authURL.RawQuery = urlParams.Encode()
	}

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
	availableProviders, err := s.oauthRepo.ListOAuthProxyProvidersByServer(ctx, repo.ListOAuthProxyProvidersByServerParams{
		OauthProxyServerID: toolset.OauthProxyServerID.UUID,
		ProjectID:          toolset.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "OAuth providers not configured").Log(ctx, s.logger)
	}
	if len(availableProviders) == 0 {
		return oops.E(oops.CodeUnexpected, nil, "OAuth providers not configured").Log(ctx, s.logger)
	}

	// TODO: Eventually support multiple providers
	provider := availableProviders[0]

	// Provider-specific token exchange
	var accessToken string
	var expiresAt *time.Time

	var oauthProvider providers.Provider
	switch provider.ProviderType {
	case string(OAuthProxyProviderTypeGram):
		oauthProvider = s.gramProvider
	default:
		oauthProvider = s.customProvider
	}

	result, err := oauthProvider.ExchangeToken(ctx, externalCode, provider, toolset, s.serverURL)
	if err != nil {
		s.logger.ErrorContext(ctx, "provider token exchange failed",
			attr.SlogOAuthProvider(provider.Slug),
			attr.SlogError(err))

		// Determine error code based on error type
		errorCode := "server_error"
		errorDescription := "Failed to authorize. Please try again."
		if providers.IsAccessDeniedError(err) {
			errorCode = "access_denied"
			errorDescription = "User does not have access to the requested organization."
		}

		errorURL, buildErr := s.grantManager.BuildErrorResponse(
			ctx,
			oauthReqInfo["redirect_uri"],
			errorCode,
			errorDescription,
			oauthReqInfo["state"],
		)
		if buildErr != nil {
			s.logger.ErrorContext(ctx, "failed to build error response URL", attr.SlogError(buildErr))
			return oops.E(oops.CodeUnexpected, buildErr, "failed to build error response").Log(ctx, s.logger)
		}

		// Add defensive check for empty error description
		if errorDescription == "" {
			errorDescription = "Authorization failed. Please try again."
		}

		if provider.ProviderType == string(OAuthProxyProviderTypeGram) {
			data := gramOAuthResultPageData{
				RedirectURL:      template.URL(errorURL), // #nosec G203 // This has been checked and escaped
				ErrorDescription: errorDescription,
				ErrorCode:        errorCode,
				ScriptHash:       s.oauthStatusPageScriptHash,
			}

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := s.failurePageTmpl.Execute(w, data); err != nil {
				return oops.E(oops.CodeUnexpected, err, "failed to render oauth failure page").Log(ctx, s.logger)
			}
		} else {
			http.Redirect(w, r, errorURL, http.StatusFound)
		}

		return nil
	}

	accessToken = result.AccessToken
	expiresAt = result.ExpiresAt

	// Reconstruct the original authorization request from decoded state
	authReq := &AuthorizationRequest{
		ResponseType:        oauthReqInfo["response_type"],
		ClientID:            oauthReqInfo["client_id"],
		RedirectURI:         oauthReqInfo["redirect_uri"],
		Scope:               oauthReqInfo["scope"],
		State:               oauthReqInfo["state"],
		CodeChallenge:       oauthReqInfo["code_challenge"],
		CodeChallengeMethod: oauthReqInfo["code_challenge_method"],
		Nonce:               oauthReqInfo["nonce"],
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

	if provider.ProviderType == string(OAuthProxyProviderTypeGram) {
		data := gramOAuthResultPageData{
			RedirectURL:      template.URL(responseURL), // #nosec G203 // This has been checked and escaped
			ScriptHash:       s.oauthStatusPageScriptHash,
			ErrorDescription: "",
			ErrorCode:        "",
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.successPageTmpl.Execute(w, data); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to render oauth success page").Log(ctx, s.logger)
		}
	} else {
		http.Redirect(w, r, responseURL, http.StatusFound)
	}

	return nil
}

// serveSuccessScript serves the OAuth success page JavaScript file with cache headers
func (s *Service) serveSuccessScript(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	// Get hash from URL
	hash := chi.URLParam(r, "hash")

	// Validate hash matches our current script hash
	if hash != s.oauthStatusPageScriptHash {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	// Set cache headers (immutable since hash is in filename)
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)

	_, err := w.Write(s.oauthStatusPageScriptData)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write script response").Log(ctx, s.logger)
	}

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
