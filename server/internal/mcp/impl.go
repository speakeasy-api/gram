package mcp

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	auth_repo "github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcp_repo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	logger              *slog.Logger
	tracer              trace.Tracer
	metrics             *metrics
	db                  *pgxpool.Pool
	authRepo            *auth_repo.Queries
	toolsetsRepo        *toolsets_repo.Queries
	mcpMetadataRepo     *metadata_repo.Queries
	orgsRepo            *organizations_repo.Queries
	auth                *auth.Auth
	env                 toolconfig.EnvironmentLoader
	serverURL           *url.URL
	posthog             *posthog.Posthog // posthog metrics will no-op if the dependency is not provided
	toolProxy           *gateway.ToolProxy
	oauthService        OAuthService
	oauthRepo           *oauth_repo.Queries
	billingTracker      billing.Tracker
	billingRepository   billing.Repository
	toolsetCache        cache.TypedCacheObject[mv.ToolsetBaseContents]
	features            *productfeatures.Client
	telemetryService    *tm.Service
	vectorToolStore     *rag.ToolsetVectorStore
	temporal            *temporal.Environment
	sessions            *sessions.Manager
	chatSessionsManager *chatsessions.Manager
	externalmcpRepo     *externalmcp_repo.Queries
	deploymentsRepo     *deployments_repo.Queries
	enc                 *encryption.Client
}

type oauthTokenInputs struct {
	securityKeys []string // can be empty if a single token applies to the whole server
	Token        string
}

type ToolMode string

const (
	ToolModeStatic  ToolMode = "static"
	ToolModeDynamic ToolMode = "dynamic"
)

type mcpInputs struct {
	projectID        uuid.UUID
	toolset          string
	environment      string
	mcpEnvVariables  map[string]string
	oauthTokenInputs []oauthTokenInputs
	authenticated    bool
	sessionID        string
	chatID           string
	mode             ToolMode
	userID           string
	externalUserID   string
	apiKeyID         string
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	chatSessionsManager *chatsessions.Manager,
	env toolconfig.EnvironmentLoader,
	posthog *posthog.Posthog,
	serverURL *url.URL,
	enc *encryption.Client,
	cacheImpl cache.Cache,
	guardianPolicy *guardian.Policy,
	funcCaller functions.ToolCaller,
	oauthService OAuthService,
	billingTracker billing.Tracker,
	billingRepository billing.Repository,
	telemSvc *tm.Service,
	features *productfeatures.Client,
	vectorToolStore *rag.ToolsetVectorStore,
	temporal *temporal.Environment,
) *Service {
	tracer := tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcp")
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/mcp")
	logger = logger.With(attr.SlogComponent("mcp"))

	return &Service{
		logger:          logger,
		tracer:          tracer,
		metrics:         newMetrics(meter, logger),
		db:              db,
		authRepo:        auth_repo.New(db),
		toolsetsRepo:    toolsets_repo.New(db),
		mcpMetadataRepo: metadata_repo.New(db),
		orgsRepo:        organizations_repo.New(db),
		deploymentsRepo: deployments_repo.New(db),
		externalmcpRepo: externalmcp_repo.New(db),
		auth:            auth.New(logger, db, sessions),
		env:             env,
		serverURL:       serverURL,
		posthog:         posthog,
		toolProxy: gateway.NewToolProxy(
			logger,
			tracerProvider,
			meterProvider,
			gateway.ToolCallSourceMCP,
			enc,
			cacheImpl,
			guardianPolicy,
			funcCaller,
		),
		oauthService:        oauthService,
		oauthRepo:           oauth_repo.New(db),
		billingTracker:      billingTracker,
		billingRepository:   billingRepository,
		toolsetCache:        cache.NewTypedObjectCache[mv.ToolsetBaseContents](logger.With(attr.SlogCacheNamespace("toolset")), cacheImpl, cache.SuffixNone),
		telemetryService:    telemSvc,
		features:            features,
		vectorToolStore:     vectorToolStore,
		temporal:            temporal,
		sessions:            sessions,
		chatSessionsManager: chatSessionsManager,
		enc:                 enc,
	}
}

func Attach(mux goahttp.Muxer, service *Service, metadataService *mcpmetadata.Service) {
	o11y.AttachHandler(mux, "POST", "/mcp/{mcpSlug}", oops.ErrHandle(service.logger, service.ServePublic).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/mcp/{mcpSlug}", oops.ErrHandle(service.logger, func(w http.ResponseWriter, r *http.Request) error {
		return service.HandleGetServer(w, r, metadataService)
	}).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/mcp/{mcpSlug}/install", oops.ErrHandle(service.logger, metadataService.ServeInstallPage).ServeHTTP)
	o11y.AttachHandler(mux, "POST", "/mcp/{project}/{toolset}/{environment}", oops.ErrHandle(service.logger, service.ServeAuthenticated).ServeHTTP)

	// OAuth 2.1 Authorization Server Metadata
	o11y.AttachHandler(mux, "GET", "/.well-known/oauth-authorization-server/mcp/{mcpSlug}", oops.ErrHandle(service.logger, service.HandleWellKnownOAuthServerMetadata).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/.well-known/oauth-protected-resource/mcp/{mcpSlug}", oops.ErrHandle(service.logger, service.HandleWellKnownOAuthProtectedResourceMetadata).ServeHTTP)
}

// HandleGetServer handles GET requests to /mcp/{mcpSlug}, checking for HTML requests
// and delegating to metadata service, or returning method not allowed for others.
func (s *Service) HandleGetServer(w http.ResponseWriter, r *http.Request, metadataService *mcpmetadata.Service) error {
	// Check if this is a browser request (HTML Accept header)
	for mediaTypeFull := range strings.SplitSeq(r.Header.Get("Accept"), ",") {
		if mediatype, _, err := mime.ParseMediaType(mediaTypeFull); err == nil && (mediatype == "text/html" || mediatype == "application/xhtml+xml") {
			if err := metadataService.ServeInstallPage(w, r); err != nil {
				return fmt.Errorf("failed to serve install page: %w", err)
			}
			return nil
		}
	}

	body, err := json.Marshal(rpcError{
		ID:      msgID{format: 0, String: "", Number: 0},
		Code:    methodNotAllowed,
		Message: "This MCP server uses POST-based Streamable HTTP transport. This GET request is a normal compatibility probe by the MCP client and can be safely ignored. The client will automatically use POST for actual communication.",
		Data:    nil,
	})
	if err != nil {
		s.logger.ErrorContext(r.Context(), "failed to marshal MCP 405 response", attr.SlogError(err))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return fmt.Errorf("failed to marshal MCP 405 response: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusMethodNotAllowed)
	_, writeErr := w.Write(body)
	if writeErr != nil {
		s.logger.ErrorContext(r.Context(), "failed to write response body", attr.SlogError(writeErr))
		return fmt.Errorf("failed to write response body: %w", writeErr)
	}

	return nil
}

// handleWellKnownMetadata handles OAuth 2.1 authorization server metadata discovery
func (s *Service) HandleWellKnownOAuthServerMetadata(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	result, err := wellknown.ResolveOAuthServerMetadataFromToolset(
		ctx,
		s.logger,
		s.db,
		s.oauthRepo,
		&s.toolsetCache,
		toolset,
		baseURL,
		mcpSlug,
	)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to resolve OAuth server metadata").Log(ctx, s.logger)
	}

	if result == nil {
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found").Log(ctx, s.logger)
	}

	// Handle proxy case - reverse proxy to external MCP OAuth server
	if result.Kind == wellknown.OAuthServerMetadataResultKindProxy {
		target, parseErr := url.Parse(result.ProxyURL)
		if parseErr != nil {
			return oops.E(oops.CodeUnexpected, parseErr, "failed to parse well-known URL").Log(ctx, s.logger)
		}

		proxy := &httputil.ReverseProxy{
			Director: nil,
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(target)
			},
			Transport:      nil,
			FlushInterval:  0,
			ErrorLog:       nil,
			BufferPool:     nil,
			ModifyResponse: nil,
			ErrorHandler:   nil,
		}
		proxy.ServeHTTP(w, r)
		return nil
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	var body []byte
	switch result.Kind {
	case wellknown.OAuthServerMetadataResultKindRaw:
		body = result.Raw
	case wellknown.OAuthServerMetadataResultKindStatic:
		var marshalErr error
		body, marshalErr = json.Marshal(result.Static)
		if marshalErr != nil {
			return oops.E(oops.CodeUnexpected, marshalErr, "failed to marshal OAuth server metadata").Log(ctx, s.logger)
		}
	default:
		return oops.E(oops.CodeUnexpected, nil, "unexpected OAuth server metadata result kind").Log(ctx, s.logger)
	}

	_, writeErr := w.Write(body)
	if writeErr != nil {
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) HandleWellKnownOAuthProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	metadata, err := wellknown.ResolveOAuthProtectedResourceFromToolset(
		ctx,
		s.logger,
		s.db,
		&s.toolsetCache,
		toolset,
		baseURL,
		mcpSlug,
	)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to resolve OAuth protected resource metadata").Log(ctx, s.logger)
	}

	if metadata == nil {
		return oops.E(oops.CodeNotFound, nil, "not found").Log(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	body, err := json.Marshal(metadata)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal OAuth protected resource metadata").Log(ctx, s.logger)
	}

	_, writeErr := w.Write(body)
	if writeErr != nil {
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) ServePublic(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	fullToolset, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(toolset.ProjectID), mv.ToolsetSlug(toolset.Slug), &s.toolsetCache)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to load toolset").Log(ctx, s.logger)
	}
	hasExternalMCPOAuth := externalmcp.ResolveOAuthConfig(fullToolset) != nil

	// Extract tokens from headers separately:
	// - authToken: from Authorization header (for OAuth flows)
	// - sessionToken: from Gram-Chat-Session header (for chat session fallback on non-OAuth endpoints)
	authToken := r.Header.Get("Authorization")
	authToken = strings.TrimPrefix(authToken, "Bearer ")
	authToken = strings.TrimPrefix(authToken, "bearer ")
	chatSessionJwt := r.Header.Get(constants.ChatSessionsTokenHeader)

	var tokenInputs []oauthTokenInputs

	var oAuthProxyProvider *oauth_repo.OauthProxyProvider
	if toolset.OauthProxyServerID.Valid {
		providers, err := s.oauthRepo.ListOAuthProxyProvidersByServer(
			ctx,
			oauth_repo.ListOAuthProxyProvidersByServerParams{
				OauthProxyServerID: toolset.OauthProxyServerID.UUID,
				ProjectID:          toolset.ProjectID,
			},
		)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to load OAuth proxy providers").Log(ctx, s.logger)
		}

		if len(providers) == 0 {
			return oops.E(oops.CodeUnexpected, nil, "no OAuth proxy providers found").Log(ctx, s.logger)
		}

		oAuthProxyProvider = &providers[0]
	}

	// Switch handling auth based on both MCP configuration and request context.
	//
	// Possible MCP configurations, for reference:
	// - "External OAuth" - User-provided OAuth server separate from Gram
	// - "External MCP OAuth" - OAuth provided by a 3rd party MCP server
	//   (usually via catalog)
	// - "OAuth Proxy" - Gram acts as the OAuth2.1 DCR server between MCP client
	//   & non-DCR OAuth Server
	switch {
	case toolset.McpIsPublic && toolset.ExternalOauthServerID.Valid:
		// External OAuth server flow - only accept Authorization header
		if authToken == "" {
			s.logger.WarnContext(ctx, "No authorization token provided")
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%s`, baseURL+"/.well-known/oauth-protected-resource/mcp/"+mcpSlug))
			return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
		}

		tokenInputs = append(tokenInputs, oauthTokenInputs{
			securityKeys: []string{},
			Token:        authToken,
		})
	case toolset.McpIsPublic && oAuthProxyProvider != nil && oAuthProxyProvider.ProviderType == "custom":
		// Custom OAuth provider flow - only accept Authorization header
		oauthToken, err := s.oauthService.ValidateAccessToken(ctx, toolset.ID, authToken)
		if err != nil {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%s`, baseURL+"/.well-known/oauth-protected-resource/mcp/"+mcpSlug))
			return oops.E(oops.CodeUnauthorized, err, "invalid or expired access token").Log(ctx, s.logger)
		}
		s.logger.InfoContext(ctx, "OAuth token validated successfully", attr.SlogToolsetID(toolset.ID.String()))

		for _, externalSecret := range oauthToken.ExternalSecrets {
			tokenInputs = append(tokenInputs, oauthTokenInputs{
				securityKeys: externalSecret.SecurityKeys,
				Token:        externalSecret.Token,
			})
		}
	case toolset.McpIsPublic && hasExternalMCPOAuth:
		wwwAuth := fmt.Sprintf(`Bearer resource_metadata=%s`,
			baseURL+"/.well-known/oauth-protected-resource/mcp/"+mcpSlug)

		// Prioritize literal tokens sent by the client. Pass through directly
		// to underlying server.
		if authToken != "" {
			tokenInputs = append(tokenInputs, oauthTokenInputs{
				securityKeys: []string{},
				Token:        authToken,
			})

			break
		}

		// Attempt to look for a stored OAuth credential if the requests comes
		// from a Gram app (eg: Dashboard/Playground)
		if gramSession, _ := r.Cookie(constants.SessionCookie); gramSession != nil {
			resolvedToken, err := s.resolveExternalMcpOAuthToken(ctx, fullToolset)
			if err != nil {
				w.Header().Set("WWW-Authenticate", wwwAuth)
				return oops.E(oops.CodeUnauthorized, err, "unauthorized")
			}

			tokenInputs = append(tokenInputs, oauthTokenInputs{
				securityKeys: []string{},
				Token:        resolvedToken,
			})

			break
		}

		w.Header().Set("WWW-Authenticate", wwwAuth)
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	case !toolset.McpIsPublic:
		// Private MCP - always allow chatSessionJwt fallback since private servers require user authentication
		isOAuthCapable := oAuthProxyProvider != nil && oAuthProxyProvider.ProviderType == "gram"
		token := authToken
		if token == "" {
			token = chatSessionJwt
		}

		ctx, err = s.authenticateToken(ctx, token, toolset.ID, isOAuthCapable)
		if err == nil {
			break
		}

		if isOAuthCapable {
			w.Header().Set(
				"WWW-Authenticate",
				fmt.Sprintf(`Bearer resource_metadata=%s`, baseURL+"/.well-known/oauth-protected-resource/mcp/"+mcpSlug),
			)
		}

		return oops.E(oops.CodeUnauthorized, nil, "expired or invalid access token")
	default:
		// Public MCP without OAuth - allow chatSessionJwt fallback
		token := authToken
		if token == "" {
			token = chatSessionJwt
		}
		if token != "" {
			ctx, err = s.authenticateToken(ctx, token, toolset.ID, false)
			if err != nil {
				return err
			}

			authCtx, ok := contextvalues.GetAuthContext(ctx)
			if !ok || authCtx == nil {
				return oops.E(oops.CodeUnauthorized, nil, "no auth context found").Log(ctx, s.logger)
			}
		}
	}

	var selectedEnvironment string
	var authenticated bool
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ActiveOrganizationID != "" {
		projects, err := s.authRepo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return oops.E(oops.CodeForbidden, nil, "no projects found").Log(ctx, s.logger)
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "error checking project access").Log(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
		}

		projectInOrg := false
		for _, project := range projects {
			if project.ID == toolset.ProjectID {
				projectInOrg = true
				break
			}
		}

		if projectInOrg {
			authenticated = true
		} else if !toolset.McpIsPublic {
			// Only return 401 for non-public MCPs when the user is not in the owning org
			return oops.C(oops.CodeUnauthorized)
		}
		// For public MCPs accessed from outside the owning org, authenticated stays false
		// so they get public access without environment/secrets
	}

	if !toolset.McpIsPublic && !authenticated {
		return oops.C(oops.CodeNotFound)
	}

	// IMPORTANT: We should not use gram environments if we are not in an authenticated context
	if authenticated {
		selectedEnvironment = conv.PtrValOr(conv.FromPGText[string](toolset.DefaultEnvironmentSlug), "")
		if passedEnv := r.Header.Get("Gram-Environment"); passedEnv != "" {
			selectedEnvironment = conv.ToSlug(passedEnv)
		}
	}

	var batch batchedRawRequest
	err = json.NewDecoder(r.Body).Decode(&batch)
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return oops.E(oops.CodeBadRequest, err, "failed to decode request body").Log(ctx, s.logger)
	}

	if len(batch) == 0 {
		return respondWithNoContent(true, w)
	}

	sessionID := parseMcpSessionID(r.Header)
	w.Header().Set("Mcp-Session-Id", sessionID)

	// Load header display names for remapping
	headerDisplayNames := s.loadHeaderDisplayNames(ctx, toolset.ID)

	// Extract user IDs for telemetry
	var userID, externalUserID, apiKeyID string
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil {
		userID = authCtx.UserID
		externalUserID = authCtx.ExternalUserID
		apiKeyID = authCtx.APIKeyID
	}

	mcpInputs := &mcpInputs{
		projectID:        toolset.ProjectID,
		toolset:          toolset.Slug,
		environment:      selectedEnvironment,
		mcpEnvVariables:  parseMcpEnvVariables(r, headerDisplayNames),
		authenticated:    authenticated,
		oauthTokenInputs: tokenInputs,
		sessionID:        sessionID,
		chatID:           r.Header.Get("Gram-Chat-ID"),
		mode:             resolveToolMode(r, *toolset),
		userID:           userID,
		externalUserID:   externalUserID,
		apiKeyID:         apiKeyID,
	}

	body, err := s.handleBatch(ctx, mcpInputs, batch)
	switch {
	case body == nil && err == nil:
		return respondWithNoContent(true, w)
	case err != nil:
		return NewErrorFromCause(batch[0].ID, err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, writeErr := w.Write(body)
	if writeErr != nil {
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body")
	}

	return nil
}

func (s *Service) loadToolsetFromMcpSlug(ctx context.Context, mcpSlug string) (*toolsets_repo.Toolset, *customdomains.Context, error) {
	var toolset toolsets_repo.Toolset
	var toolsetErr error
	var customDomainCtx *customdomains.Context
	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil {
		toolset, toolsetErr = s.toolsetsRepo.GetToolsetByMcpSlugAndCustomDomain(ctx, toolsets_repo.GetToolsetByMcpSlugAndCustomDomainParams{
			McpSlug:        conv.ToPGText(mcpSlug),
			CustomDomainID: uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true},
		})
		customDomainCtx = domainCtx
	} else {
		toolset, toolsetErr = s.toolsetsRepo.GetToolsetByMcpSlug(ctx, conv.ToPGText(mcpSlug)) //
	}

	if toolsetErr != nil {
		return nil, nil, oops.E(oops.CodeNotFound, toolsetErr, "mcp server not found").Log(ctx, s.logger)
	}

	return &toolset, customDomainCtx, nil
}

// loadHeaderDisplayNames loads the header display names mapping from MCP metadata.
// Returns an empty map if no metadata exists or on error (non-critical operation).
func (s *Service) loadHeaderDisplayNames(ctx context.Context, toolsetID uuid.UUID) map[string]string {
	result := make(map[string]string)

	displayNamesJSON, err := s.mcpMetadataRepo.GetHeaderDisplayNames(ctx, toolsetID)
	if err != nil {
		// Not found or error - return empty map, this is non-critical
		return result
	}

	if len(displayNamesJSON) > 0 {
		if parseErr := json.Unmarshal(displayNamesJSON, &result); parseErr != nil {
			s.logger.WarnContext(ctx, "failed to parse header display names", attr.SlogError(parseErr))
		}
	}

	return result
}

func (s *Service) ServeAuthenticated(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var err error

	projectSlug := chi.URLParam(r, "project")
	if projectSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "a project slug must be provided")
	}

	toolsetSlug := chi.URLParam(r, "toolset")
	if toolsetSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "a toolset slug must be provided")
	}

	environmentSlug := chi.URLParam(r, "environment")
	if environmentSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an environment slug must be provided")
	}

	sc := security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		Scopes:         []string{"consumer"},
		RequiredScopes: []string{},
	}
	token := r.Header.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	ctx, err = s.auth.Authorize(ctx, token, &sc)
	if err != nil {
		return oops.C(oops.CodeUnauthorized)
	}

	// Authorize with project
	sc = security.APIKeyScheme{
		Name:           constants.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}
	ctx, err = s.auth.Authorize(ctx, projectSlug, &sc)
	if err != nil {
		return oops.C(oops.CodeUnauthorized)
	}

	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	// authorization check
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	var batch batchedRawRequest
	err = json.NewDecoder(r.Body).Decode(&batch)
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return oops.E(oops.CodeBadRequest, err, "failed to decode request body").Log(ctx, s.logger)
	}

	if len(batch) == 0 {
		return respondWithNoContent(true, w)
	}

	sessionID := parseMcpSessionID(r.Header)
	w.Header().Set("Mcp-Session-Id", sessionID)

	toolset, err := s.toolsetsRepo.GetToolset(ctx, toolsets_repo.GetToolsetParams{
		Slug:      toolsetSlug,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
	}

	// Load header display names for remapping
	headerDisplayNames := s.loadHeaderDisplayNames(ctx, toolset.ID)

	mcpInputs := &mcpInputs{
		projectID:        *authCtx.ProjectID,
		toolset:          toolsetSlug,
		environment:      environmentSlug,
		mcpEnvVariables:  parseMcpEnvVariables(r, headerDisplayNames),
		authenticated:    true,
		oauthTokenInputs: []oauthTokenInputs{},
		sessionID:        sessionID,
		chatID:           r.Header.Get("Gram-Chat-ID"),
		mode:             resolveToolMode(r, toolset),
		userID:           authCtx.UserID,
		externalUserID:   authCtx.ExternalUserID,
		apiKeyID:         authCtx.APIKeyID,
	}

	body, err := s.handleBatch(ctx, mcpInputs, batch)
	switch {
	case body == nil && err == nil:
		return respondWithNoContent(true, w)
	case err != nil:
		return NewErrorFromCause(batch[0].ID, err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, writeErr := w.Write(body)
	if writeErr != nil {
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body").Log(ctx, s.logger)
	}
	return nil
}

// TODO: this is for demo. There probably needs to still be annotation per toolset on if it allows dynamic tool calling
// Realistically you would need to embed and vectorize ahead of time
func resolveToolMode(r *http.Request, toolset toolsets_repo.Toolset) ToolMode {
	mode := r.Header.Get("Gram-Mode")
	mode = strings.TrimSpace(mode)
	mode = strings.ToLower(mode)

	if mode != "" {
		return ToolMode(mode)
	} else if toolset.ToolSelectionMode != "" {
		return ToolMode(toolset.ToolSelectionMode)
	}

	return ToolModeStatic
}

func (s *Service) handleBatch(ctx context.Context, payload *mcpInputs, batch batchedRawRequest) (json.RawMessage, error) {
	results := make([]json.RawMessage, 0, len(batch))
	for _, req := range batch {
		result, err := s.handleRequest(ctx, payload, req)
		switch {
		case result == nil && err == nil:
			return nil, nil
		case err != nil:
			bs, merr := json.Marshal(NewErrorFromCause(req.ID, err))
			if merr != nil {
				return nil, oops.E(oops.CodeUnexpected, merr, "failed to serialize error response").Log(ctx, s.logger)
			}

			result = bs
		}

		results = append(results, result)
	}

	if len(results) == 1 {
		return results[0], nil
	} else {
		m, err := json.Marshal(results)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize results").Log(ctx, s.logger)
		}

		return m, nil
	}
}

// parseMcpEnvVariables: Map potential user provided mcp variables into inputs
// Only inputs that match up with a security or server env var in the proxy will be used in the proxy
// headerDisplayNames maps actual header names (e.g., "X-RapidAPI-Key") to display names (e.g., "API Key")
// When a display name is used in the MCP header, it's mapped back to the actual header's env var
func parseMcpEnvVariables(r *http.Request, headerDisplayNames map[string]string) map[string]string {
	ignoredHeaders := []string{
		"mcp-session-id",
	}

	// Build reverse mapping: normalized_display_name -> normalized_actual_name
	// This allows users to send MCP-API-KEY and have it mapped to X_RAPIDAPI_KEY
	displayNameToActual := make(map[string]string)
	for actualName, displayName := range headerDisplayNames {
		if displayName != "" {
			// Normalize: lowercase and replace dashes with underscores
			normalizedDisplayName := strings.ToLower(strings.ReplaceAll(displayName, "-", "_"))
			normalizedDisplayName = strings.ReplaceAll(normalizedDisplayName, " ", "_")
			normalizedActual := strings.ToLower(strings.ReplaceAll(actualName, "-", "_"))
			displayNameToActual[normalizedDisplayName] = normalizedActual
		}
	}

	envVars := map[string]string{}
	for k := range r.Header {
		keySanitized := strings.ToLower(k)
		if strings.HasPrefix(keySanitized, "mcp-") && !slices.Contains(ignoredHeaders, keySanitized) {
			// Extract the key without MCP- prefix and normalize
			normalizedKey := strings.ReplaceAll(strings.TrimPrefix(keySanitized, "mcp-"), "-", "_")

			// Check if this is a display name and map to actual header name
			if actualKey, ok := displayNameToActual[normalizedKey]; ok {
				normalizedKey = actualKey
			}

			envVars[normalizedKey] = r.Header.Get(k)
		}

	}

	return envVars
}

func (s *Service) handleRequest(ctx context.Context, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		start := time.Now()
		defer func() {
			s.metrics.RecordMCPRequestDuration(ctx, req.Method, requestContext.Host+requestContext.ReqURL, time.Since(start))
		}()
	}

	switch req.Method {
	case "ping":
		return handlePing(ctx, s.logger, req.ID)
	case "initialize":
		return handleInitialize(ctx, s.logger, req, payload, s.posthog, s.toolsetsRepo, s.mcpMetadataRepo)
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "tools/list":
		return handleToolsList(ctx, s.logger, s.db, s.env, payload, req, s.posthog, &s.toolsetCache, s.vectorToolStore, s.temporal)
	case "tools/call":
		return handleToolsCall(ctx, s.logger, s.metrics, s.db, s.env, payload, req, s.toolProxy, s.billingTracker, s.billingRepository, &s.toolsetCache, s.telemetryService, s.features, s.vectorToolStore, s.temporal, s.mcpMetadataRepo)
	case "prompts/list":
		return handlePromptsList(ctx, s.logger, s.db, payload, req, &s.toolsetCache)
	case "prompts/get":
		return handlePromptsGet(ctx, s.logger, s.db, payload, req)
	case "resources/list":
		return handleResourcesList(ctx, s.logger, s.db, payload, req, &s.toolsetCache)
	case "resources/read":
		return handleResourcesRead(ctx, s.logger, s.db, payload, req, s.toolProxy, s.env, s.billingTracker, s.billingRepository, s.telemetryService, s.features)
	default:
		return nil, &rpcError{
			ID:      req.ID,
			Code:    methodNotFound,
			Message: fmt.Sprintf("%s: %s", req.Method, methodNotFound.UserMessage()),
			Data:    nil,
		}
	}
}

func parseMcpSessionID(headers http.Header) string {
	session := headers.Get("Mcp-Session-Id")
	if session == "" {
		session = uuid.New().String()
	}
	return session
}

func (s *Service) authenticateToken(ctx context.Context, token string, toolsetID uuid.UUID, isOAuthCapable bool) (context.Context, error) {
	if token == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	var oAuthToken *oauth.Token
	var err error
	if isOAuthCapable {
		oAuthToken, err = s.oauthService.ValidateAccessToken(ctx, toolsetID, token)
	}
	if err == nil && oAuthToken != nil {
		// OAuth token validated, authenticate with session
		if len(oAuthToken.ExternalSecrets) == 0 {
			return ctx, oops.E(oops.CodeUnauthorized, nil, "no session token found")
		}

		ctx, err = s.sessions.Authenticate(ctx, oAuthToken.ExternalSecrets[0].Token, false)
		if err != nil {
			return ctx, oops.E(oops.CodeUnauthorized, err, "failed to authenticate session")
		}

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		if !ok || authCtx == nil {
			return ctx, oops.E(oops.CodeUnauthorized, nil, "no auth context found")
		}

		s.logger.InfoContext(ctx, "authenticated via gram OAuth", attr.SlogToolsetID(toolsetID.String()))
		return ctx, nil
	}

	if errors.Is(err, oauth.ErrExpiredAccessToken) {
		return ctx, oops.E(oops.CodeUnauthorized, err, "expired access token")
	}

	// Strategy 2: Try API key authentication (consumer scope)
	sc := security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		RequiredScopes: []string{"consumer"},
		Scopes:         []string{},
	}

	ctx, err = s.auth.Authorize(ctx, token, &sc)
	if err == nil {
		return ctx, nil
	}

	// Strategy 3: Try API key authentication (chat scope fallback)
	sc = security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		RequiredScopes: []string{"chat"},
		Scopes:         []string{},
	}
	ctx, err = s.auth.Authorize(ctx, token, &sc)
	if err == nil {
		return ctx, nil
	}

	// Strategy 4: Try Chat Sessions Token authentication
	ctx, err = s.chatSessionsManager.Authorize(ctx, token)
	if err == nil {
		return ctx, nil
	}

	// All strategies failed
	return ctx, oops.E(oops.CodeUnauthorized, nil, "failed to authorize").Log(ctx, s.logger)
}

func (s *Service) resolveExternalMcpOAuthToken(ctx context.Context, toolset *types.Toolset) (string, error) {
	sessionCtx, err := s.sessions.AuthenticateWithCookie(ctx)
	if err != nil {
		return "", oops.E(oops.CodeUnauthorized, err, "failed to authenticate session for OAuth token lookup")
	}

	authCtx, ok := contextvalues.GetAuthContext(sessionCtx)
	if !ok || authCtx == nil {
		return "", oops.C(oops.CodeUnauthorized)
	}

	oauthConfig := externalmcp.ResolveOAuthConfig(toolset)
	if oauthConfig == nil {
		return "", oops.C(oops.CodeUnauthorized)
	}

	toolsetID, err := uuid.Parse(toolset.ID)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "invalid toolset ID")
	}

	token, err := s.oauthRepo.GetUserOAuthToken(ctx, oauth_repo.GetUserOAuthTokenParams{
		UserID:         authCtx.UserID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ToolsetID:      toolsetID,
	})

	if err != nil {
		return "", oops.E(oops.CodeUnauthorized, err, "failed to get user OAuth token")
	}

	if token.ExpiresAt.Valid && token.ExpiresAt.Time.Before(time.Now()) {
		return "", oops.E(oops.CodeUnauthorized, err, "OAuth token has expired")
	}

	accessToken, err := s.enc.Decrypt(token.AccessTokenEncrypted)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "unable to access oauth token")
	}

	return accessToken, nil
}
