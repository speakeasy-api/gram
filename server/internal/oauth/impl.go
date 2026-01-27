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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	_ "embed"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	chatsessions_pkg "github.com/speakeasy-api/gram/server/internal/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/providers"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

//go:embed hosted_oauth_success_page.html.tmpl
var oauthSuccessPageTmplData string

//go:embed hosted_oauth_failure_page.html.tmpl
var oauthFailurePageTmplData string

//go:embed hosted_oauth_status_script.js
var oauthSuccessScriptData []byte

//go:embed session_oauth_result.html.tmpl
var sessionOAuthResultTmplData string

type gramOAuthResultPageData struct {
	RedirectURL template.URL
	ScriptHash  string

	// Error fields for failure page
	ErrorDescription string
	ErrorCode        string
}

type sessionOAuthResultPageData struct {
	Success     bool
	ToolsetSlug string
	Origin      string
	Error       string
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
	chatSessionsManager       *chatsessions.Manager
	credentialStore           *chatsessions_pkg.CredentialStore
	jwtSecret                 string
	gramProvider              *providers.GramProvider
	customProvider            *providers.CustomProvider
	externalProvider          *providers.ExternalOAuthProvider
	mcpOAuthProvider          *providers.MCPOAuthProvider
	successPageTmpl           *template.Template
	failurePageTmpl           *template.Template
	sessionOAuthResultTmpl    *template.Template
	oauthStatusPageScriptHash string
	oauthStatusPageScriptData []byte
	toolsetCache              cache.TypedCacheObject[mv.ToolsetBaseContents]
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	serverURL *url.URL,
	cacheImpl cache.Cache,
	enc *encryption.Client,
	env *environments.EnvironmentEntries,
	sessions *sessions.Manager,
	chatSessionsManager *chatsessions.Manager,
	jwtSecret string,
	toolsetCache cache.TypedCacheObject[mv.ToolsetBaseContents],
) *Service {
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
	externalProvider := providers.NewExternalOAuthProvider(logger, enc)
	mcpOAuthProvider := providers.NewMCPOAuthProvider(logger, enc)

	// Parse templates once during initialization
	successPageTmpl := template.Must(template.New("oauth_success").Parse(oauthSuccessPageTmplData))
	failurePageTmpl := template.Must(template.New("oauth_failure").Parse(oauthFailurePageTmplData))
	sessionOAuthResultTmpl := template.Must(template.New("session_oauth_result").Parse(sessionOAuthResultTmplData))

	// Calculate content hash for success script (for cache busting)
	hash := sha256.Sum256(oauthSuccessScriptData)
	scriptHash := hex.EncodeToString(hash[:])[:8] // Use first 8 chars like hosted page

	// Initialize credential store for session-scoped OAuth
	credentialStore := chatsessions_pkg.NewCredentialStore(db, enc)

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

		// Session-scoped OAuth support
		chatSessionsManager: chatSessionsManager,
		credentialStore:     credentialStore,
		jwtSecret:           jwtSecret,
		toolsetCache:        toolsetCache,

		// OAuth providers
		gramProvider:     gramProvider,
		customProvider:   customProvider,
		externalProvider: externalProvider,
		mcpOAuthProvider: mcpOAuthProvider,

		// HTML templates
		successPageTmpl:        successPageTmpl,
		failurePageTmpl:        failurePageTmpl,
		sessionOAuthResultTmpl: sessionOAuthResultTmpl,

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

	// Session-scoped OAuth endpoints
	// Get authorization URL for session OAuth (requires Gram-Chat-Session header)
	o11y.AttachHandler(mux, "GET", "/oauth/{mcpSlug}/session-authorize-url", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleSessionAuthorizeURL).ServeHTTP(w, r)
	})

	// Session OAuth callback (stores credentials and sends postMessage)
	o11y.AttachHandler(mux, "GET", "/oauth/session-callback", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleSessionCallback).ServeHTTP(w, r)
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

// handleSessionAuthorizeURL returns the OAuth authorization URL for session-scoped OAuth.
// This endpoint requires a valid Gram-Chat-Session token in the header.
// It supports all OAuth types: OAuth proxy, external OAuth servers, and external MCP OAuth.
//
// The well-known endpoint provides a uniform interface for OAuth metadata regardless of type,
// so we use that to get the authorization endpoint and build the URL with our session state.
func (s *Service) handleSessionAuthorizeURL(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	// Validate the chat session token
	sessionToken := r.Header.Get(constants.ChatSessionsTokenHeader)
	if sessionToken == "" {
		return oops.E(oops.CodeUnauthorized, nil, "chat session token required").Log(ctx, s.logger)
	}

	claims, err := s.chatSessionsManager.ValidateToken(ctx, sessionToken)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "invalid chat session token").Log(ctx, s.logger)
	}

	// Load toolset
	toolset, _, err := s.loadToolsetFromCurrentURLContext(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	// Verify the session belongs to the same project
	if claims.ProjectID != toolset.ProjectID.String() {
		return oops.E(oops.CodeForbidden, nil, "session does not belong to this project").Log(ctx, s.logger)
	}

	// Build signed state for the OAuth flow
	nonce, err := conv.GenerateRandomSlug(16)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to generate nonce").Log(ctx, s.logger)
	}

	// Get origin from request or use default
	origin := r.URL.Query().Get("origin")
	if origin == "" {
		origin = s.serverURL.String()
	}

	state := &OAuthSessionState{
		SessionID: claims.SessionID,
		ToolsetID: toolset.ID.String(),
		ProjectID: toolset.ProjectID.String(),
		Origin:    origin,
		Nonce:     nonce,
	}

	signedState, err := SignSessionState(state, s.jwtSecret)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to sign state").Log(ctx, s.logger)
	}

	// Use wellknown to resolve OAuth metadata uniformly for any OAuth type
	oauthMetadata, err := wellknown.ResolveOAuthServerMetadataFromToolset(
		ctx,
		s.logger,
		s.db,
		s.oauthRepo,
		&s.toolsetCache,
		toolset,
		s.serverURL.String(),
		mcpSlug,
	)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to resolve OAuth metadata").Log(ctx, s.logger)
	}
	if oauthMetadata == nil {
		return oops.E(oops.CodeBadRequest, nil, "toolset does not have OAuth configured").Log(ctx, s.logger)
	}

	// Extract OAuth endpoints from metadata (works uniformly for all OAuth types)
	endpoints, err := extractOAuthEndpoints(oauthMetadata)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to extract OAuth endpoints").Log(ctx, s.logger)
	}
	if endpoints.AuthorizationEndpoint == "" {
		return oops.E(oops.CodeBadRequest, nil, "OAuth metadata missing authorization_endpoint").Log(ctx, s.logger)
	}

	callbackURL := fmt.Sprintf("%s/oauth/session-callback", s.serverURL.String())

	// Build the authorization URL
	authURL, err := s.buildSessionAuthURL(ctx, toolset, endpoints.AuthorizationEndpoint, callbackURL, signedState)
	if err != nil {
		return err
	}

	response := map[string]string{
		"authorization_url": authURL.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.ErrorContext(ctx, "failed to encode response", attr.SlogError(err))
	}
	return nil
}

// oauthEndpoints contains the OAuth endpoints extracted from metadata.
type oauthEndpoints struct {
	AuthorizationEndpoint string
	TokenEndpoint         string
}

// extractOAuthEndpoints extracts the required OAuth endpoints from metadata.
// Works uniformly regardless of whether metadata is static or raw.
func extractOAuthEndpoints(metadata *wellknown.OAuthServerMetadataResult) (*oauthEndpoints, error) {
	if metadata.Static != nil {
		return &oauthEndpoints{
			AuthorizationEndpoint: metadata.Static.AuthorizationEndpoint,
			TokenEndpoint:         metadata.Static.TokenEndpoint,
		}, nil
	}

	if metadata.Raw != nil {
		var parsed struct {
			AuthorizationEndpoint string `json:"authorization_endpoint"`
			TokenEndpoint         string `json:"token_endpoint"`
		}
		if err := json.Unmarshal(metadata.Raw, &parsed); err != nil {
			return nil, fmt.Errorf("failed to parse OAuth metadata: %w", err)
		}
		return &oauthEndpoints{
			AuthorizationEndpoint: parsed.AuthorizationEndpoint,
			TokenEndpoint:         parsed.TokenEndpoint,
		}, nil
	}

	return nil, fmt.Errorf("OAuth metadata has no static or raw content")
}

// buildSessionAuthURL builds the authorization URL with session-specific parameters.
// For OAuth proxy providers, it uses the internal provider mechanism.
// For external OAuth, it builds a standard OAuth authorization URL.
func (s *Service) buildSessionAuthURL(
	ctx context.Context,
	toolset *toolsets_repo.Toolset,
	authorizationEndpoint, callbackURL, signedState string,
) (*url.URL, error) {
	// For OAuth proxy with Gram provider, use the special sessions manager
	if toolset.OauthProxyServerID.Valid {
		oauthProviders, err := s.oauthRepo.ListOAuthProxyProvidersByServer(ctx, repo.ListOAuthProxyProvidersByServerParams{
			OauthProxyServerID: toolset.OauthProxyServerID.UUID,
			ProjectID:          toolset.ProjectID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to load OAuth providers").Log(ctx, s.logger)
		}
		if len(oauthProviders) == 0 {
			return nil, oops.E(oops.CodeNotFound, nil, "no OAuth providers configured").Log(ctx, s.logger)
		}

		provider := oauthProviders[0]

		// Gram provider uses special session-based authentication
		if provider.ProviderType == string(OAuthProxyProviderTypeGram) {
			authURL, err := s.sessions.BuildAuthorizationURL(ctx, sessions.AuthURLParams{
				CallbackURL:     callbackURL,
				Scope:           strings.Join(provider.ScopesSupported, " "),
				State:           signedState,
				ScopesSupported: provider.ScopesSupported,
				ClientID:        "",
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to build Gram authorization URL").Log(ctx, s.logger)
			}
			return authURL, nil
		}

		// Custom OAuth proxy provider - get client_id from secrets
		var secrets map[string]string
		if err := json.Unmarshal(provider.Secrets, &secrets); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "OAuth provider secrets invalid").Log(ctx, s.logger)
		}

		clientID := secrets["client_id"]
		if clientID == "" && secrets["environment_slug"] != "" {
			envMap, err := s.environments.Load(ctx, toolset.ProjectID, gateway.Slug(secrets["environment_slug"]))
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to load environment").Log(ctx, s.logger)
			}
			for k, v := range envMap {
				if strings.ToLower(k) == "client_id" {
					clientID = v
				}
			}
		}

		if clientID == "" {
			return nil, oops.E(oops.CodeUnexpected, nil, "OAuth provider client_id not configured").Log(ctx, s.logger)
		}

		authURL, err := url.Parse(authorizationEndpoint)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to parse OAuth authorization URL").Log(ctx, s.logger)
		}

		urlParams := url.Values{}
		urlParams.Set("client_id", clientID)
		urlParams.Set("redirect_uri", callbackURL)
		urlParams.Set("response_type", "code")
		urlParams.Set("state", signedState)

		if len(provider.ScopesSupported) > 0 {
			urlParams.Set("scope", strings.Join(provider.ScopesSupported, " "))
		}

		authURL.RawQuery = urlParams.Encode()
		return authURL, nil
	}

	// For external OAuth servers, get client_id from encrypted secrets
	if toolset.ExternalOauthServerID.Valid {
		clientID, scopes, err := s.getExternalOAuthServerClientID(ctx, toolset)
		if err != nil {
			return nil, err
		}

		authURL, err := url.Parse(authorizationEndpoint)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to parse authorization endpoint").Log(ctx, s.logger)
		}

		urlParams := url.Values{}
		urlParams.Set("client_id", clientID)
		urlParams.Set("redirect_uri", callbackURL)
		urlParams.Set("response_type", "code")
		urlParams.Set("state", signedState)

		if len(scopes) > 0 {
			urlParams.Set("scope", strings.Join(scopes, " "))
		}

		authURL.RawQuery = urlParams.Encode()
		return authURL, nil
	}

	// For external MCP OAuth, perform dynamic client registration if needed
	// Load the full toolset to get external MCP OAuth configuration
	fullToolset, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(toolset.ProjectID), mv.ToolsetSlug(toolset.Slug), &s.toolsetCache)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load toolset").Log(ctx, s.logger)
	}

	oauthConfig := externalmcp.ResolveOAuthConfig(fullToolset)

	if oauthConfig != nil && oauthConfig.OAuthVersion == externalmcp.OAuthVersion21 {
		// This is external MCP OAuth - perform dynamic registration if needed
		attachmentID, err := uuid.Parse(oauthConfig.AttachmentID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "invalid attachment ID").Log(ctx, s.logger)
		}

		clientID, _, err := s.getOrRegisterMCPOAuthClient(ctx, toolset.ProjectID, attachmentID, oauthConfig, callbackURL)
		if err != nil {
			return nil, err
		}

		authURL, err := url.Parse(authorizationEndpoint)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to parse authorization endpoint").Log(ctx, s.logger)
		}

		urlParams := url.Values{}
		urlParams.Set("client_id", clientID)
		urlParams.Set("redirect_uri", callbackURL)
		urlParams.Set("response_type", "code")
		urlParams.Set("state", signedState)

		if len(oauthConfig.ScopesSupported) > 0 {
			urlParams.Set("scope", strings.Join(oauthConfig.ScopesSupported, " "))
		}

		authURL.RawQuery = urlParams.Encode()
		return authURL, nil
	}

	// Fallback: build authorization URL without client_id (some OAuth providers may still work)
	s.logger.WarnContext(ctx, "building authorization URL without client_id - OAuth flow may fail",
		attr.SlogToolsetID(toolset.ID.String()))

	authURL, err := url.Parse(authorizationEndpoint)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse authorization endpoint").Log(ctx, s.logger)
	}

	urlParams := url.Values{}
	urlParams.Set("redirect_uri", callbackURL)
	urlParams.Set("response_type", "code")
	urlParams.Set("state", signedState)

	authURL.RawQuery = urlParams.Encode()
	return authURL, nil
}

// getExternalOAuthServerClientID retrieves the client_id from external OAuth server secrets.
func (s *Service) getExternalOAuthServerClientID(ctx context.Context, toolset *toolsets_repo.Toolset) (clientID string, scopes []string, err error) {
	server, err := s.oauthRepo.GetExternalOAuthServerWithSecrets(ctx, repo.GetExternalOAuthServerWithSecretsParams{
		ProjectID: toolset.ProjectID,
		ID:        toolset.ExternalOauthServerID.UUID,
	})
	if err != nil {
		return "", nil, oops.E(oops.CodeUnexpected, err, "failed to get external OAuth server").Log(ctx, s.logger)
	}

	if len(server.Secrets) == 0 {
		return "", nil, oops.E(oops.CodeBadRequest, nil, "external OAuth server has no client credentials configured").Log(ctx, s.logger)
	}

	secrets, err := s.externalProvider.DecryptSecrets(server.Secrets)
	if err != nil {
		return "", nil, oops.E(oops.CodeUnexpected, err, "failed to decrypt external OAuth secrets").Log(ctx, s.logger)
	}

	if secrets.ClientID == "" {
		return "", nil, oops.E(oops.CodeBadRequest, nil, "external OAuth server has no client_id configured").Log(ctx, s.logger)
	}

	// Parse scopes from metadata
	var metadata map[string]interface{}
	if err := json.Unmarshal(server.Metadata, &metadata); err == nil {
		if scopesRaw, ok := metadata["scopes_supported"].([]interface{}); ok {
			for _, s := range scopesRaw {
				if str, ok := s.(string); ok {
					scopes = append(scopes, str)
				}
			}
		}
	}

	return secrets.ClientID, scopes, nil
}

// handleSessionCallback handles the OAuth callback for session-scoped credentials.
// It validates the signed state, exchanges the code for tokens, stores the credentials,
// and returns an HTML page that sends a postMessage to the opener window.
// Supports all OAuth types: OAuth proxy, external OAuth servers, and external MCP OAuth.
func (s *Service) handleSessionCallback(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Validate signed state
	stateParam := r.URL.Query().Get("state")
	if stateParam == "" {
		return s.renderSessionOAuthError(w, "Missing state parameter", "", "")
	}

	state, err := ValidateSessionState(stateParam, s.jwtSecret)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid session state", attr.SlogError(err))
		return s.renderSessionOAuthError(w, "Invalid state parameter", "", "")
	}

	// Parse IDs from state
	sessionID, err := uuid.Parse(state.SessionID)
	if err != nil {
		return s.renderSessionOAuthError(w, "Invalid session ID", state.Origin, "")
	}

	toolsetID, err := uuid.Parse(state.ToolsetID)
	if err != nil {
		return s.renderSessionOAuthError(w, "Invalid toolset ID", state.Origin, "")
	}

	projectID, err := uuid.Parse(state.ProjectID)
	if err != nil {
		return s.renderSessionOAuthError(w, "Invalid project ID", state.Origin, "")
	}

	// Check for error response from OAuth provider
	if errorCode := r.URL.Query().Get("error"); errorCode != "" {
		errorDesc := r.URL.Query().Get("error_description")
		if errorDesc == "" {
			errorDesc = errorCode
		}
		return s.renderSessionOAuthError(w, errorDesc, state.Origin, "")
	}

	// Get the authorization code
	code := r.URL.Query().Get("code")
	if code == "" {
		return s.renderSessionOAuthError(w, "Missing authorization code", state.Origin, "")
	}

	// Load the toolset to get OAuth provider info
	toolset, err := s.toolsetsRepo.GetToolsetByID(ctx, toolsets_repo.GetToolsetByIDParams{
		ID:        toolsetID,
		ProjectID: projectID,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to load toolset", attr.SlogError(err))
		return s.renderSessionOAuthError(w, "Toolset not found", state.Origin, "")
	}

	callbackURL := fmt.Sprintf("%s/oauth/session-callback", s.serverURL.String())

	// Exchange code for tokens
	tokenResult, err := s.exchangeSessionToken(ctx, &toolset, projectID, code, callbackURL)
	if err != nil {
		s.logger.ErrorContext(ctx, "token exchange failed", attr.SlogError(err))
		return s.renderSessionOAuthError(w, "Failed to exchange authorization code", state.Origin, toolset.Slug)
	}

	// Store the credential
	err = s.credentialStore.StoreCredential(ctx, chatsessions_pkg.StoreCredentialParams{
		SessionID:    sessionID,
		ProjectID:    projectID,
		ToolsetID:    toolsetID,
		AccessToken:  tokenResult.AccessToken,
		RefreshToken: tokenResult.RefreshToken,
		TokenType:    "Bearer",
		Scope:        "",
		ExpiresAt:    tokenResult.ExpiresAt,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to store credential", attr.SlogError(err))
		return s.renderSessionOAuthError(w, "Failed to store credentials", state.Origin, toolset.Slug)
	}

	s.logger.InfoContext(ctx, "session OAuth completed successfully",
		attr.SlogSessionID(sessionID.String()),
		attr.SlogToolsetID(toolsetID.String()),
	)

	// Render success page with postMessage
	return s.renderSessionOAuthSuccess(w, state.Origin, toolset.Slug)
}

// tokenExchangeResult holds the result of a token exchange operation.
type tokenExchangeResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
}

// exchangeSessionToken exchanges an authorization code for tokens.
// For OAuth proxy providers, it uses the internal provider mechanism.
// For external OAuth, it uses standard OAuth token exchange.
func (s *Service) exchangeSessionToken(ctx context.Context, toolset *toolsets_repo.Toolset, projectID uuid.UUID, code, callbackURL string) (*tokenExchangeResult, error) {
	// OAuth proxy providers use internal provider implementations
	if toolset.OauthProxyServerID.Valid {
		return s.exchangeOAuthProxyToken(ctx, toolset, projectID, code)
	}

	// External OAuth server (manually configured credentials)
	if toolset.ExternalOauthServerID.Valid {
		return s.exchangeExternalOAuthServerToken(ctx, toolset, projectID, code, callbackURL)
	}

	// External MCP OAuth (dynamic client registration)
	// Load full toolset to check for external MCP OAuth config
	fullToolset, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(projectID), mv.ToolsetSlug(toolset.Slug), &s.toolsetCache)
	if err != nil {
		return nil, fmt.Errorf("load toolset for external MCP OAuth: %w", err)
	}

	oauthConfig := externalmcp.ResolveOAuthConfig(fullToolset)
	if oauthConfig != nil {
		return s.exchangeExternalMCPOAuthToken(ctx, toolset, projectID, oauthConfig, code, callbackURL)
	}

	return nil, fmt.Errorf("no OAuth configuration found for toolset")
}

// exchangeExternalOAuthServerToken exchanges the authorization code using external OAuth server credentials.
func (s *Service) exchangeExternalOAuthServerToken(ctx context.Context, toolset *toolsets_repo.Toolset, projectID uuid.UUID, code, callbackURL string) (*tokenExchangeResult, error) {
	// Load external OAuth server metadata with secrets
	serverMeta, err := s.oauthRepo.GetExternalOAuthServerWithSecrets(ctx, repo.GetExternalOAuthServerWithSecretsParams{
		ProjectID: projectID,
		ID:        toolset.ExternalOauthServerID.UUID,
	})
	if err != nil {
		return nil, fmt.Errorf("load external OAuth server metadata: %w", err)
	}

	// Check if secrets are configured
	if len(serverMeta.Secrets) == 0 {
		return nil, fmt.Errorf("external OAuth server credentials not configured - please configure client_id and client_secret")
	}

	// Decrypt secrets
	secrets, err := s.externalProvider.DecryptSecrets(serverMeta.Secrets)
	if err != nil {
		return nil, fmt.Errorf("decrypt external OAuth secrets: %w", err)
	}

	// Parse the metadata to get token endpoint
	var metadata wellknown.OAuthServerMetadata
	if err := json.Unmarshal(serverMeta.Metadata, &metadata); err != nil {
		return nil, fmt.Errorf("parse external OAuth server metadata: %w", err)
	}

	if metadata.TokenEndpoint == "" {
		return nil, fmt.Errorf("external OAuth server has no token endpoint configured")
	}

	// Exchange the code
	result, err := s.externalProvider.ExchangeToken(ctx, providers.ExternalTokenExchangeParams{
		Code:          code,
		TokenEndpoint: metadata.TokenEndpoint,
		ClientID:      secrets.ClientID,
		ClientSecret:  secrets.ClientSecret,
		RedirectURI:   callbackURL,
		AuthMethods:   nil, // Will use default (client_secret_post)
	})
	if err != nil {
		return nil, fmt.Errorf("external OAuth token exchange failed: %w", err)
	}

	return &tokenExchangeResult{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    result.ExpiresAt,
	}, nil
}

// exchangeExternalMCPOAuthToken exchanges the authorization code using external MCP OAuth.
// This uses dynamic client registration if no client credentials are stored.
func (s *Service) exchangeExternalMCPOAuthToken(ctx context.Context, toolset *toolsets_repo.Toolset, projectID uuid.UUID, oauthConfig *externalmcp.ExternalMCPOAuthConfig, code, callbackURL string) (*tokenExchangeResult, error) {
	if oauthConfig.TokenEndpoint == "" {
		return nil, fmt.Errorf("external MCP OAuth has no token endpoint")
	}

	if oauthConfig.AttachmentID == "" {
		return nil, fmt.Errorf("external MCP OAuth has no attachment ID")
	}

	attachmentID, err := uuid.Parse(oauthConfig.AttachmentID)
	if err != nil {
		return nil, fmt.Errorf("invalid external MCP attachment ID: %w", err)
	}

	// Try to get existing client registration
	clientID, clientSecret, err := s.getOrRegisterMCPOAuthClient(ctx, projectID, attachmentID, oauthConfig, callbackURL)
	if err != nil {
		return nil, fmt.Errorf("get or register MCP OAuth client: %w", err)
	}

	// Exchange the authorization code
	result, err := s.externalProvider.ExchangeToken(ctx, providers.ExternalTokenExchangeParams{
		Code:          code,
		TokenEndpoint: oauthConfig.TokenEndpoint,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		RedirectURI:   callbackURL,
		AuthMethods:   nil, // Use default (client_secret_post)
	})
	if err != nil {
		return nil, fmt.Errorf("external MCP OAuth token exchange failed: %w", err)
	}

	return &tokenExchangeResult{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    result.ExpiresAt,
	}, nil
}

// getOrRegisterMCPOAuthClient retrieves existing OAuth client credentials or performs dynamic registration.
func (s *Service) getOrRegisterMCPOAuthClient(ctx context.Context, projectID, attachmentID uuid.UUID, oauthConfig *externalmcp.ExternalMCPOAuthConfig, redirectURI string) (clientID, clientSecret string, err error) {
	// Try to get existing client registration
	existingClient, err := s.oauthRepo.GetExternalMCPOAuthClient(ctx, attachmentID)
	if err == nil {
		// Check if client ID has expired
		if existingClient.ClientIDExpiresAt.Valid && existingClient.ClientIDExpiresAt.Time.Before(time.Now()) {
			s.logger.InfoContext(ctx, "External MCP OAuth client expired, re-registering",
				attr.SlogToolsetID(attachmentID.String()))
		} else {
			// Decrypt and return existing credentials
			clientID, err = s.enc.Decrypt(string(existingClient.ClientIDEncrypted))
			if err != nil {
				return "", "", fmt.Errorf("decrypt client ID: %w", err)
			}

			if len(existingClient.ClientSecretEncrypted) > 0 {
				clientSecret, err = s.enc.Decrypt(string(existingClient.ClientSecretEncrypted))
				if err != nil {
					return "", "", fmt.Errorf("decrypt client secret: %w", err)
				}
			}

			s.logger.InfoContext(ctx, "Using existing external MCP OAuth client",
				attr.SlogToolsetID(attachmentID.String()))
			return clientID, clientSecret, nil
		}
	}

	// No valid existing client - perform dynamic registration
	if oauthConfig.RegistrationEndpoint == "" {
		return "", "", fmt.Errorf("external MCP OAuth requires dynamic client registration but no registration endpoint is available")
	}

	s.logger.InfoContext(ctx, "Performing dynamic client registration for external MCP OAuth",
		attr.SlogToolsetID(attachmentID.String()))

	reg, err := s.mcpOAuthProvider.RegisterClient(ctx, providers.MCPDynamicRegistrationParams{
		RegistrationEndpoint: oauthConfig.RegistrationEndpoint,
		ClientName:           "Gram",
		RedirectURIs:         []string{redirectURI},
		TokenEndpointAuth:    "client_secret_post",
		GrantTypes:           []string{"authorization_code", "refresh_token"},
		ResponseTypes:        []string{"code"},
	})
	if err != nil {
		return "", "", fmt.Errorf("dynamic client registration failed: %w", err)
	}

	// Encrypt and store the registration
	clientIDEncrypted, err := s.enc.Encrypt([]byte(reg.ClientID))
	if err != nil {
		return "", "", fmt.Errorf("encrypt client ID: %w", err)
	}

	var clientSecretEncrypted []byte
	if reg.ClientSecret != "" {
		encrypted, err := s.enc.Encrypt([]byte(reg.ClientSecret))
		if err != nil {
			return "", "", fmt.Errorf("encrypt client secret: %w", err)
		}
		clientSecretEncrypted = []byte(encrypted)
	}

	var registrationAccessTokenEncrypted []byte
	if reg.RegistrationAccessToken != "" {
		encrypted, err := s.enc.Encrypt([]byte(reg.RegistrationAccessToken))
		if err != nil {
			return "", "", fmt.Errorf("encrypt registration access token: %w", err)
		}
		registrationAccessTokenEncrypted = []byte(encrypted)
	}

	var expiresAt pgtype.Timestamptz
	if reg.ClientIDExpiresAt != nil {
		expiresAt = pgtype.Timestamptz{Time: *reg.ClientIDExpiresAt, Valid: true, InfinityModifier: 0}
	}

	_, err = s.oauthRepo.UpsertExternalMCPOAuthClient(ctx, repo.UpsertExternalMCPOAuthClientParams{
		ProjectID:                        projectID,
		ExternalMcpAttachmentID:          attachmentID,
		ClientIDEncrypted:                []byte(clientIDEncrypted),
		ClientSecretEncrypted:            clientSecretEncrypted,
		ClientIDExpiresAt:                expiresAt,
		RegistrationAccessTokenEncrypted: registrationAccessTokenEncrypted,
		RegistrationClientUri:            conv.ToPGText(reg.RegistrationClientURI),
	})
	if err != nil {
		return "", "", fmt.Errorf("store client registration: %w", err)
	}

	s.logger.InfoContext(ctx, "Successfully registered and stored external MCP OAuth client",
		attr.SlogToolsetID(attachmentID.String()))

	return reg.ClientID, reg.ClientSecret, nil
}

// exchangeOAuthProxyToken exchanges the authorization code using OAuth proxy providers.
func (s *Service) exchangeOAuthProxyToken(ctx context.Context, toolset *toolsets_repo.Toolset, projectID uuid.UUID, code string) (*tokenExchangeResult, error) {
	oauthProviders, err := s.oauthRepo.ListOAuthProxyProvidersByServer(ctx, repo.ListOAuthProxyProvidersByServerParams{
		OauthProxyServerID: toolset.OauthProxyServerID.UUID,
		ProjectID:          projectID,
	})
	if err != nil || len(oauthProviders) == 0 {
		return nil, fmt.Errorf("failed to load OAuth providers: %w", err)
	}

	provider := oauthProviders[0]

	var oauthProvider providers.Provider
	switch provider.ProviderType {
	case string(OAuthProxyProviderTypeGram):
		oauthProvider = s.gramProvider
	default:
		oauthProvider = s.customProvider
	}

	result, err := oauthProvider.ExchangeToken(ctx, code, provider, toolset, s.serverURL)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	return &tokenExchangeResult{
		AccessToken:  result.AccessToken,
		RefreshToken: "", // Not returned by current providers
		ExpiresAt:    result.ExpiresAt,
	}, nil
}

func (s *Service) renderSessionOAuthSuccess(w http.ResponseWriter, origin, toolsetSlug string) error {
	data := sessionOAuthResultPageData{
		Success:     true,
		ToolsetSlug: toolsetSlug,
		Origin:      origin,
		Error:       "",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.sessionOAuthResultTmpl.Execute(w, data); err != nil {
		return fmt.Errorf("render session OAuth success: %w", err)
	}
	return nil
}

func (s *Service) renderSessionOAuthError(w http.ResponseWriter, errorMsg, origin, toolsetSlug string) error {
	data := sessionOAuthResultPageData{
		Success:     false,
		ToolsetSlug: toolsetSlug,
		Origin:      origin,
		Error:       errorMsg,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.sessionOAuthResultTmpl.Execute(w, data); err != nil {
		return fmt.Errorf("render session OAuth error: %w", err)
	}
	return nil
}
