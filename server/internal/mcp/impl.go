package mcp

import (
	"context"
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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
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
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	auth_repo "github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
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
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformtoolsruntime "github.com/speakeasy-api/gram/server/internal/platformtools/runtime"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	logger              *slog.Logger
	tracer              trace.Tracer
	metrics             *metrics
	guardianPolicy      *guardian.Policy
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
	telemLogger         *tm.Logger
	vectorToolStore     *rag.ToolsetVectorStore
	temporal            *temporal.Environment
	assistantTokens     *assistanttokens.Manager
	sessions            *sessions.Manager
	chatSessionsManager *chatsessions.Manager
	externalmcpRepo     *externalmcp_repo.Queries
	deploymentsRepo     *deployments_repo.Queries
	enc                 *encryption.Client
	authz               *authz.Engine
	shadowMCPClient     *shadowmcp.Client
}

type oauthTokenInputs struct {
	securityKeys []string // can be empty if a single token applies to the whole server
	Token        string
}

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
	telemLogger *tm.Logger,
	telemSvc *tm.Service,
	vectorToolStore *rag.ToolsetVectorStore,
	triggerApp *bgtriggers.App,
	temporal *temporal.Environment,
	authzEngine *authz.Engine,
	assistantTokens *assistanttokens.Manager,
	shadowMCPClient *shadowmcp.Client,
	auditLogger *audit.Logger,
) *Service {
	tracer := tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcp")
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/mcp")
	logger = logger.With(attr.SlogComponent("mcp"))

	platformSvc := platformtoolsruntime.NewService(
		logger,
		db,
		telemSvc,
		auditLogger,
		platformtoolsruntime.WithTriggerTools(triggerApp),
		platformtoolsruntime.WithSlackHTTPClient(guardianPolicy.PooledClient()),
	)

	return &Service{
		logger:          logger,
		tracer:          tracer,
		metrics:         newMetrics(meter, logger),
		guardianPolicy:  guardianPolicy,
		db:              db,
		authRepo:        auth_repo.New(db),
		toolsetsRepo:    toolsets_repo.New(db),
		mcpMetadataRepo: metadata_repo.New(db),
		orgsRepo:        organizations_repo.New(db),
		deploymentsRepo: deployments_repo.New(db),
		externalmcpRepo: externalmcp_repo.New(db),
		auth:            auth.New(logger, db, sessions, authzEngine),
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
			platformSvc,
		),
		oauthService:        oauthService,
		oauthRepo:           oauth_repo.New(db),
		billingTracker:      billingTracker,
		billingRepository:   billingRepository,
		toolsetCache:        cache.NewTypedObjectCache[mv.ToolsetBaseContents](logger.With(attr.SlogCacheNamespace("toolset")), cacheImpl, cache.SuffixNone),
		telemLogger:         telemLogger,
		vectorToolStore:     vectorToolStore,
		temporal:            temporal,
		assistantTokens:     assistantTokens,
		sessions:            sessions,
		chatSessionsManager: chatSessionsManager,
		enc:                 enc,
		authz:               authzEngine,
		shadowMCPClient:     shadowMCPClient,
	}
}

func Attach(mux goahttp.Muxer, service *Service, metadataService *mcpmetadata.Service) {
	o11y.AttachHandler(mux, "POST", "/mcp/{mcpSlug}", oops.ErrHandle(service.logger, service.ServePublic).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/mcp/{mcpSlug}", oops.ErrHandle(service.logger, func(w http.ResponseWriter, r *http.Request) error {
		return service.HandleGetServer(w, r, metadataService)
	}).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/mcp/{mcpSlug}/install", oops.ErrHandle(service.logger, metadataService.ServeInstallPage).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/mcp/install-page-{hash}.js", oops.ErrHandle(service.logger, metadataService.ServeInstallPageScript).ServeHTTP)

	// OAuth 2.1 Authorization Server Metadata
	o11y.AttachHandler(mux, "GET", wellknown.OAuthAuthorizationServerPath+"/mcp/{mcpSlug}", oops.ErrHandle(service.logger, service.HandleWellKnownOAuthServerMetadata).ServeHTTP)
	o11y.AttachHandler(mux, "GET", wellknown.OAuthProtectedResourcePath+"/mcp/{mcpSlug}", oops.ErrHandle(service.logger, service.HandleWellKnownOAuthProtectedResourceMetadata).ServeHTTP)
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
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
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
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
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

	return writeOAuthServerMetadataResponse(ctx, s.logger, w, result)
}

// writeOAuthServerMetadataResponse builds the OAuth server metadata body and
// only commits the 200 OK status once the body is ready. This ordering matters:
// if marshaling fails or the result kind is unrecognized, the caller's error
// handler middleware needs an unwritten ResponseWriter so it can emit the real
// error status — Go's net/http silently drops a second WriteHeader call.
func writeOAuthServerMetadataResponse(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, result *wellknown.OAuthServerMetadataResult) error {
	var body []byte
	switch result.Kind {
	case wellknown.OAuthServerMetadataResultKindRaw:
		body = result.Raw
	case wellknown.OAuthServerMetadataResultKindStatic:
		var marshalErr error
		body, marshalErr = json.Marshal(result.Static)
		if marshalErr != nil {
			return oops.E(oops.CodeUnexpected, marshalErr, "failed to marshal OAuth server metadata").Log(ctx, logger)
		}
	default:
		return oops.E(oops.CodeUnexpected, nil, "unexpected OAuth server metadata result kind").Log(ctx, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write response body").Log(ctx, logger)
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
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
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
		baseURL+"/mcp/"+mcpSlug,
	)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to resolve OAuth protected resource metadata").Log(ctx, s.logger)
	}

	if metadata == nil {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}

	return writeOAuthProtectedResourceMetadataResponse(ctx, s.logger, w, metadata)
}

// writeOAuthProtectedResourceMetadataResponse builds the OAuth protected
// resource metadata body and only commits the 200 OK status once the body is
// ready. See writeOAuthServerMetadataResponse for the rationale behind the
// ordering.
func writeOAuthProtectedResourceMetadataResponse(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, metadata *wellknown.OAuthProtectedResourceMetadata) error {
	body, err := json.Marshal(metadata)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal OAuth protected resource metadata").Log(ctx, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write response body").Log(ctx, logger)
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

	toolset, _, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}

	return s.ServeToolsetResolved(w, r, toolset, mcpSlug, "mcp")
}

// ServeToolsetResolved serves an MCP runtime request after the slug has
// already been resolved to a toolset. It is exported so other runtime
// surfaces (currently /x/mcp) can delegate the toolset-backed serving body
// without re-implementing the OAuth/visibility/RBAC and tool dispatch flow.
//
// mcpSlug and mcpRouteBase are used to build the WWW-Authenticate
// resource_metadata URL. mcpRouteBase is the route segment that sits
// between the well-known prefix and the slug — "mcp" for /mcp/{slug} or
// "x/mcp" for /x/mcp/{slug}, no leading or trailing slashes.
//
// The caller is responsible for closing r.Body.
func (s *Service) ServeToolsetResolved(w http.ResponseWriter, r *http.Request, toolset *toolsets_repo.Toolset, mcpSlug, mcpRouteBase string) error {
	ctx := r.Context()
	var err error

	baseURL := s.serverURL.String()
	if customDomainCtx := customdomains.FromContext(ctx); customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	// Extract tokens from headers separately:
	// - authToken: from Authorization header (for OAuth flows)
	// - sessionToken: from Gram-Chat-Session header (for chat session fallback on non-OAuth endpoints)
	authToken := AuthorizationBearerToken(r)

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

	// Token extraction — best effort for public MCPs with OAuth.
	// We collect tokens if present but don't return 401 here.
	// checkToolsetSecurity below enforces auth requirements and returns
	// false when unsatisfied, which triggers the 401 + WWW-Authenticate response.
	//
	// Private MCPs still enforce identity auth at this level since that's user
	// identity, not per-tool security.
	oauthRequired := toolset.ExternalOauthServerID.Valid || (oAuthProxyProvider != nil)
	oauthProtectedResourceURL, err := url.JoinPath(baseURL, wellknown.OAuthProtectedResourcePath, mcpRouteBase, mcpSlug)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to build OAuth protected resource URL").Log(ctx, s.logger)
	}
	switch {
	case toolset.McpIsPublic && toolset.ExternalOauthServerID.Valid:
		// External OAuth server flow — collect token if present
		if authToken != "" {
			tokenInputs = append(tokenInputs, oauthTokenInputs{
				securityKeys: []string{},
				Token:        authToken,
			})
		}
	case toolset.McpIsPublic && oAuthProxyProvider != nil && oAuthProxyProvider.ProviderType == "custom":
		// Custom OAuth provider flow — validate and collect tokens if present
		if authToken != "" {
			oauthToken, err := s.oauthService.ValidateAccessToken(ctx, toolset.ID, authToken)
			if errors.Is(err, oauth.ErrExpiredExternalSecrets) && oauthToken != nil {
				s.logger.InfoContext(ctx, "upstream credentials expired, attempting refresh", attr.SlogToolsetID(toolset.ID.String()), attr.SlogOAuthProvider(oAuthProxyProvider.Slug))
				var refreshedToken *oauth.Token
				refreshedToken, err = s.oauthService.RefreshProxyToken(ctx, toolset.ID, oauthToken, oAuthProxyProvider, toolset)
				if err != nil {
					s.logger.WarnContext(ctx, "upstream token refresh failed", attr.SlogToolsetID(toolset.ID.String()), attr.SlogOAuthProvider(oAuthProxyProvider.Slug), attr.SlogError(err))
				} else {
					oauthToken = refreshedToken
				}
			}
			if err != nil {
				s.logger.WarnContext(ctx, "OAuth token validation failed", attr.SlogToolsetID(toolset.ID.String()), attr.SlogError(err))
			} else {
				s.logger.InfoContext(ctx, "OAuth token validated successfully", attr.SlogToolsetID(toolset.ID.String()), attr.SlogOAuthProvider(oAuthProxyProvider.Slug))
			}
			// Collect upstream secrets so checkToolsetSecurity knows the user
			// authenticated. We skip this when the Gram access token itself has
			// expired (ErrExpiredAccessToken) — an expired token must not grant
			// access. We still collect when only the upstream credentials expired
			// (ErrExpiredExternalSecrets) because the user's Gram session is
			// valid; the upstream refresh is best-effort.
			if oauthToken != nil && !errors.Is(err, oauth.ErrExpiredAccessToken) {
				for _, externalSecret := range oauthToken.ExternalSecrets {
					tokenInputs = append(tokenInputs, oauthTokenInputs{
						securityKeys: externalSecret.SecurityKeys,
						Token:        externalSecret.Token,
					})
				}
			}
		}
	case !toolset.McpIsPublic:
		isOAuthCapable := oAuthProxyProvider != nil && oAuthProxyProvider.ProviderType == "gram"
		ctx, err = s.RequirePrivateIdentityAuth(ctx, w, r, isOAuthCapable, toolset.ID, oauthProtectedResourceURL)
		if err != nil {
			return err
		}
	default:
		ctx, err = s.TryPublicIdentityAuth(ctx, r, false, toolset.ID)
		if err != nil {
			return err
		}
	}

	var selectedEnvironment string
	var authenticated bool
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ActiveOrganizationID != "" {
		projects, err := s.authRepo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
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

	if authenticated {
		// Private MCPs require mcp:connect on the specific toolset.
		// Public MCPs are open to everyone — no RBAC enforcement.
		if !toolset.McpIsPublic {
			// Ensure grants are loaded — not all auth strategies in authenticateToken
			// go through auth.Authorize (which calls PrepareContext). This is a no-op
			// if grants are already in context.
			ctx, err = s.authz.PrepareContext(ctx)
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "failed to load access grants").Log(ctx, s.logger)
			}
			if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceKind: "", ResourceID: toolset.ID.String(), Dimensions: nil}); err != nil {
				return err
			}
		}

		// IMPORTANT: We should not use gram environments if we are not in an authenticated context
		selectedEnvironment = conv.PtrValOr(conv.FromPGText[string](toolset.DefaultEnvironmentSlug), "")
		if passedEnv := r.Header.Get("Gram-Environment"); passedEnv != "" {
			selectedEnvironment = conv.ToSlug(passedEnv)
		}
	}

	// Decode the raw body first to check for batch requests
	bodyBytes, err := io.ReadAll(r.Body)
	switch {
	case errors.Is(err, io.EOF) || len(bodyBytes) == 0:
		return nil
	case err != nil:
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").Log(ctx, s.logger)
	}

	// Reject batch (array) requests — batch is deprecated in the MCP spec
	if err := inv.Check("mcp request",
		"not a batch request", len(bodyBytes) == 0 || bodyBytes[0] != '[',
	); err != nil {
		return oops.E(oops.CodeBadRequest, err, "batch requests are not supported").Log(ctx, s.logger)
	}

	var req rawRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to decode request body").Log(ctx, s.logger)
	}

	sessionID := parseMcpSessionID(r.Header)
	if req.Method == "initialize" {
		w.Header().Set("Mcp-Session-Id", sessionID)
	}

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

	// Check security schemes before dispatching any RPC — including initialize.
	// Some MCP clients (e.g. Claude Desktop) require 401 on initialize to trigger
	// their OAuth flow, so we can't defer this to individual RPC handlers.
	satisfied, err := s.checkToolsetSecurity(ctx, toolset, mcpInputs)
	if err != nil {
		return err
	}
	if !satisfied {
		if oauthRequired {
			w.Header().Set(
				"WWW-Authenticate",
				fmt.Sprintf(`Bearer resource_metadata="%s"`, oauthProtectedResourceURL),
			)
		}
		return oops.C(oops.CodeUnauthorized)
	}

	body, err := s.handleRequest(ctx, mcpInputs, &req)
	switch {
	case body == nil && err == nil:
		return respondWithNoContent(true, w)
	case err != nil:
		bs, merr := json.Marshal(NewErrorFromCause(req.ID, err))
		if merr != nil {
			return oops.E(oops.CodeUnexpected, merr, "failed to serialize error response").Log(ctx, s.logger)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bs)
		return nil
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, writeErr := w.Write(body)
	if writeErr != nil {
		return oops.E(oops.CodeUnexpected, writeErr, "failed to write response body")
	}

	return nil
}

// checkToolsetSecurity loads the toolset's security variables and checks if the
// request environment satisfies at least one scheme. Returns true if satisfied
// (or if the toolset has no security requirements).
func (s *Service) checkToolsetSecurity(ctx context.Context, toolset *toolsets_repo.Toolset, payload *mcpInputs) (bool, error) {
	projectID := mv.ProjectID(payload.projectID)
	described, err := mv.DescribeToolset(ctx, s.logger, s.db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)), &s.toolsetCache)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "failed to describe toolset for security check").Log(ctx, s.logger)
	}

	schemes := describeToolSecurity(described.SecurityVariables)
	if len(schemes) == 0 {
		// No per-tool security annotations, but the toolset may still require
		// OAuth at the server level (proxy or external). If so, require the
		// user to have provided a token — otherwise the 401 + WWW-Authenticate
		// must be sent so MCP clients can initiate the OAuth flow.
		oauthRequired := toolset.McpIsPublic && (toolset.ExternalOauthServerID.Valid || toolset.OauthProxyServerID.Valid)
		if oauthRequired {
			for _, t := range payload.oauthTokenInputs {
				if t.Token != "" {
					return true, nil
				}
			}
			return false, nil
		}
		return true, nil
	}

	systemEnv, err := s.env.LoadSystemEnv(ctx, payload.projectID, toolset.ID, "", "")
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "failed to load system environment").Log(ctx, s.logger)
	}

	mergedEnv := toolconfig.NewCaseInsensitiveEnv()
	for k, v := range systemEnv.All() {
		mergedEnv.Set(k, v)
	}

	// Load authenticated user's Gram environment.
	if payload.environment != "" && payload.authenticated {
		storedEnvVars, err := s.env.Load(ctx, payload.projectID, toolconfig.Slug(payload.environment))
		if err != nil && !errors.Is(err, toolconfig.ErrNotFound) {
			s.logger.WarnContext(ctx, "failed to load user environment for security check", attr.SlogError(err))
		}
		for k, v := range storedEnvVars {
			mergedEnv.Set(k, v)
		}
	}

	// Merge MCP request headers.
	for k, v := range payload.mcpEnvVariables {
		mergedEnv.Set(k, v)
	}

	// Map any OAuth tokens to ACCESS_TOKEN env vars on OAuth schemes.
	var oauthToken string
	for _, t := range payload.oauthTokenInputs {
		if t.Token != "" {
			oauthToken = t.Token
			break
		}
	}
	if oauthToken != "" {
		for _, sv := range described.SecurityVariables {
			if sv.Type == nil {
				continue
			}
			if *sv.Type == "oauth2" || *sv.Type == "openIdConnect" {
				for _, envVar := range sv.EnvVariables {
					if strings.HasSuffix(envVar, "ACCESS_TOKEN") {
						mergedEnv.Set(envVar, oauthToken)
					}
				}
			}
		}
	}

	return anySchemeSatisfied(schemes, mergedEnv, oauthToken), nil
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
		toolset, toolsetErr = s.toolsetsRepo.GetToolsetByMcpSlug(ctx, conv.ToPGText(mcpSlug))
	}

	switch {
	case errors.Is(toolsetErr, pgx.ErrNoRows):
		return nil, nil, errToolsetNotFound
	case toolsetErr != nil:
		return nil, nil, fmt.Errorf("lookup toolset: %w", toolsetErr)
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
		return handleToolsList(ctx, s.logger, s.authz, s.guardianPolicy, s.db, s.env, payload, req, s.posthog, &s.toolsetCache, s.vectorToolStore, s.temporal, s.shadowMCPClient)
	case "tools/call":
		return handleToolsCall(ctx, s.logger, s.metrics, s.authz, s.guardianPolicy, s.db, s.env, payload, req, s.toolProxy, s.billingTracker, s.billingRepository, &s.toolsetCache, s.telemLogger, s.vectorToolStore, s.temporal, s.mcpMetadataRepo)
	case "prompts/list":
		return handlePromptsList(ctx, s.logger, s.db, payload, req, &s.toolsetCache)
	case "prompts/get":
		return handlePromptsGet(ctx, s.logger, s.db, payload, req)
	case "resources/list":
		return handleResourcesList(ctx, s.logger, s.db, payload, req, &s.toolsetCache)
	case "resources/read":
		return handleResourcesRead(ctx, s.logger, s.db, payload, req, s.toolProxy, s.env, s.billingTracker, s.billingRepository, s.telemLogger)
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

// ResolveOAuthProxyUpstreamToken validates the caller's Gram-issued
// Bearer for the supplied OAuth proxy server, refreshing stored upstream
// credentials when needed, and returns the upstream Bearer that should
// replace the caller's on the outgoing request to the remote MCP server.
//
// Returns ("", nil) when no upstream token is available — the caller
// supplied no Bearer, the lookup found no stored upstream credentials,
// or token validation failed in a non-fatal way. Callers should fall
// through to "forward with no Authorization." A non-nil error is fatal
// and the caller should reject the request.
//
// TODO: this method is currently a stub that always returns ("", nil).
// The supporting oauth machinery (oauthService.ValidateAccessToken,
// RefreshProxyToken, and the underlying ExternalSecret storage) is keyed
// by toolset_id today; generalising the resource model so it can be
// keyed by mcp_servers.id is a prerequisite to wiring this up. Until
// that lands, OAuth-proxy-backed mcp_servers behave like a public
// no-token flow at this layer (the upstream remote MCP returns 401 if
// it requires auth).
func (s *Service) ResolveOAuthProxyUpstreamToken(_ context.Context, _, _ uuid.UUID, _ string) (string, error) {
	return "", nil
}

// RequirePrivateIdentityAuth runs identity authentication for a non-public
// MCP. It tries the Authorization header first, then the
// Gram-Chat-Session header, returning the authenticated context on success.
// On failure, when isOAuthCapable, it sets a WWW-Authenticate header with
// the supplied resource_metadata URL so MCP clients can initiate OAuth, and
// returns 401.
func (s *Service) RequirePrivateIdentityAuth(ctx context.Context, w http.ResponseWriter, r *http.Request, isOAuthCapable bool, oauthResourceID uuid.UUID, wwwAuthResourceMetadataURL string) (context.Context, error) {
	token := AuthorizationOrChatSessionToken(r)

	authedCtx, err := s.authenticateToken(ctx, token, oauthResourceID, isOAuthCapable)
	if err == nil {
		return authedCtx, nil
	}

	if isOAuthCapable {
		w.Header().Set(
			"WWW-Authenticate",
			fmt.Sprintf(`Bearer resource_metadata="%s"`, wwwAuthResourceMetadataURL),
		)
	}

	return ctx, oops.E(oops.CodeUnauthorized, nil, "expired or invalid access token")
}

// TryPublicIdentityAuth optionally authenticates a public MCP request when
// the caller supplies an Authorization or Gram-Chat-Session token. Missing
// tokens are not an error; an invalid supplied token is.
func (s *Service) TryPublicIdentityAuth(ctx context.Context, r *http.Request, isOAuthCapable bool, oauthResourceID uuid.UUID) (context.Context, error) {
	token := AuthorizationOrChatSessionToken(r)
	if token == "" {
		return ctx, nil
	}

	authedCtx, err := s.authenticateToken(ctx, token, oauthResourceID, isOAuthCapable)
	if err != nil {
		return ctx, err
	}

	if authCtx, ok := contextvalues.GetAuthContext(authedCtx); !ok || authCtx == nil {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "no auth context found").Log(ctx, s.logger)
	}
	return authedCtx, nil
}

// authenticateToken authenticates the caller using the supplied token across
// several strategies (assistant tokens, gram OAuth via oauthResourceID, API
// keys, chat sessions). oauthResourceID is consumed only when isOAuthCapable
// is true — today that path is exercised only by toolset-backed flows so
// the resource is a toolset id; remote-backend callers pass false and the
// id is decorative.
func (s *Service) authenticateToken(ctx context.Context, token string, oauthResourceID uuid.UUID, isOAuthCapable bool) (context.Context, error) {
	if token == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	if authorizedCtx, _, err := s.assistantTokens.Authorize(ctx, token); err == nil {
		return authorizedCtx, nil
	}

	var oAuthToken *oauth.Token
	var err error
	if isOAuthCapable {
		oAuthToken, err = s.oauthService.ValidateAccessToken(ctx, oauthResourceID, token)
	}
	if err == nil && oAuthToken != nil {
		// OAuth token validated, authenticate with session
		if len(oAuthToken.ExternalSecrets) == 0 {
			return ctx, oops.E(oops.CodeUnauthorized, nil, "no session token found")
		}

		ctx, err = s.sessions.Authenticate(ctx, oAuthToken.ExternalSecrets[0].Token)
		if err != nil {
			return ctx, oops.E(oops.CodeUnauthorized, err, "failed to authenticate session")
		}

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		if !ok || authCtx == nil {
			return ctx, oops.E(oops.CodeUnauthorized, nil, "no auth context found")
		}

		s.logger.InfoContext(ctx, "authenticated via gram OAuth", attr.SlogToolsetID(oauthResourceID.String()))
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

//nolint:unused // kept for follow-up: restore stored-credential resolution for session-authenticated users
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

// HandleToolsList executes tools/list RPC for internal clients (e.g., agent workflows).
// This method provides direct access to tool listing without HTTP overhead.
func (s *Service) HandleToolsList(
	ctx context.Context,
	inputs *McpInputs,
) (*ToolListResult, error) {
	// Convert exported inputs to internal format
	payload := inputs.toInternal()

	// Create a dummy rawRequest for the internal handler
	req := &rawRequest{
		JSONRPC: "2.0",
		ID:      msgID{format: 1, Number: 1, String: ""},
		Method:  "tools/list",
		Params:  json.RawMessage("{}"),
	}

	// Call existing handleToolsList with all dependencies
	result, err := handleToolsList(
		ctx,
		s.logger,
		s.authz,
		s.guardianPolicy,
		s.db,
		s.env,
		payload,
		req,
		s.posthog,
		&s.toolsetCache,
		s.vectorToolStore,
		s.temporal,
		s.shadowMCPClient,
	)
	if err != nil {
		return nil, fmt.Errorf("handle tools list: %w", err)
	}

	// Parse the JSON result
	var internalResult toolsListResult
	if err := json.Unmarshal(result, &internalResult); err != nil {
		return nil, fmt.Errorf("unmarshal tools list result: %w", err)
	}

	// Convert internal result to exported format
	tools := make([]ToolListEntry, len(internalResult.Result.Tools))
	for i, t := range internalResult.Result.Tools {
		tools[i] = ToolListEntry{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Annotations: t.Annotations,
			Meta:        t.Meta,
		}
	}

	return &ToolListResult{
		Tools: tools,
	}, nil
}

// HandleToolsCall executes tools/call RPC for internal clients (e.g., agent workflows).
// This method provides direct access to tool execution without HTTP overhead.
func (s *Service) HandleToolsCall(
	ctx context.Context,
	inputs *McpInputs,
	toolName string,
	arguments json.RawMessage,
) (*ToolCallResult, error) {
	// Convert exported inputs to internal format
	payload := inputs.toInternal()

	// Construct rawRequest with tools/call parameters
	params, err := json.Marshal(map[string]any{
		"name":      toolName,
		"arguments": arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal tool call params: %w", err)
	}

	req := &rawRequest{
		JSONRPC: "2.0",
		ID:      msgID{format: 1, Number: 1, String: ""},
		Method:  "tools/call",
		Params:  params,
	}

	// Call existing handleToolsCall
	result, err := handleToolsCall(
		ctx,
		s.logger,
		s.metrics,
		s.authz,
		s.guardianPolicy,
		s.db,
		s.env,
		payload,
		req,
		s.toolProxy,
		s.billingTracker,
		s.billingRepository,
		&s.toolsetCache,
		s.telemLogger,
		s.vectorToolStore,
		s.temporal,
		s.mcpMetadataRepo,
	)
	if err != nil {
		return nil, fmt.Errorf("handle tool call: %w", err)
	}

	// Parse the JSON result wrapper
	var wrapper struct {
		Result struct {
			Content []json.RawMessage `json:"content"`
			IsError bool              `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal tool call result: %w", err)
	}

	// Convert content chunks from json.RawMessage to ContentChunk
	content := make([]ContentChunk, len(wrapper.Result.Content))
	for i, rawChunk := range wrapper.Result.Content {
		var chunk struct {
			Type     string  `json:"type"`
			Text     string  `json:"text,omitempty"`
			Data     string  `json:"data,omitempty"`
			MimeType *string `json:"mimeType,omitempty"`
		}
		if err := json.Unmarshal(rawChunk, &chunk); err != nil {
			return nil, fmt.Errorf("unmarshal content chunk %d: %w", i, err)
		}

		mimeType := ""
		if chunk.MimeType != nil {
			mimeType = *chunk.MimeType
		}

		content[i] = ContentChunk{
			Type:     chunk.Type,
			Text:     chunk.Text,
			Data:     chunk.Data,
			MimeType: mimeType,
		}
	}

	return &ToolCallResult{
		Content: content,
		IsError: wrapper.Result.IsError,
	}, nil
}
