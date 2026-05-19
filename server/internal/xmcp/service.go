// Package xmcp implements the experimental MCP runtime endpoint at
// /x/mcp/{slug}. It is a temporary path used to prove out the
// MCP Servers / MCP Endpoints fronting model — slug + optional custom
// domain → mcp_endpoint → mcp_server → backend dispatch (Remote MCP proxy
// vs. existing toolset-backed serving). Once the model is exercised here,
// runtime handling will move under /mcp/... per AGE-1902.
//
// This package owns the HTTP lifecycle (routing, slug resolution, auth, DB
// loads) for the experimental endpoint and delegates the actual serving
// work to either [github.com/speakeasy-api/gram/server/internal/remotemcp/proxy]
// (Remote MCP backend) or
// [github.com/speakeasy-api/gram/server/internal/mcp.Service.ServeToolsetResolved]
// (toolset backend).
package xmcp

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

// RuntimePath is the experimental runtime path served by this package.
const RuntimePath = "/x/mcp/{slug}"

// Service owns dependencies for the experimental MCP runtime endpoint.
type Service struct {
	logger                            *slog.Logger
	tracer                            trace.Tracer
	db                                *pgxpool.Pool
	enc                               *encryption.Client
	authz                             *authz.Engine
	shadowmcpClient                   *shadowmcp.Client
	mcpService                        *mcp.Service
	serverURL                         *url.URL
	guardianPolicy                    *guardian.Policy
	posthog                           *posthog.Posthog
	proxyMetrics                      *proxy.Metrics
	xmcpMetrics                       *metrics
	toolsCallUsageLimitsInterceptor   *ToolsCallUsageLimitsInterceptor
	toolsCallUsageTrackingInterceptor *ToolsCallUsageTrackingInterceptor
	initializePostHogEventInterceptor *InitializePostHogEventInterceptor
}

// NewService constructs a Service with its full dependency graph wired up.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	enc *encryption.Client,
	authzEngine *authz.Engine,
	shadowmcpClient *shadowmcp.Client,
	guardianPolicy *guardian.Policy,
	posthogClient *posthog.Posthog,
	billingRepo billing.Repository,
	billingTracker billing.Tracker,
	mcpService *mcp.Service,
	serverURL *url.URL,
) *Service {
	logger = logger.With(attr.SlogComponent("xmcp"))

	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/xmcp")
	xmcpMetrics := newMetrics(meter, logger)

	return &Service{
		logger:                            logger,
		tracer:                            tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/xmcp"),
		db:                                db,
		enc:                               enc,
		authz:                             authzEngine,
		shadowmcpClient:                   shadowmcpClient,
		mcpService:                        mcpService,
		serverURL:                         serverURL,
		guardianPolicy:                    guardianPolicy,
		posthog:                           posthogClient,
		proxyMetrics:                      proxy.NewMetrics(meter, logger),
		xmcpMetrics:                       xmcpMetrics,
		toolsCallUsageLimitsInterceptor:   NewToolsCallUsageLimitsInterceptor(billingRepo, logger),
		toolsCallUsageTrackingInterceptor: NewToolsCallUsageTrackingInterceptor(billingTracker, logger),
		initializePostHogEventInterceptor: NewInitializePostHogEventInterceptor(posthogClient, logger),
	}
}

// Attach registers the experimental MCP runtime handler for all supported
// HTTP methods. DELETE, GET, and POST are required by the MCP Streamable
// HTTP transport (see spec § Session Management for DELETE and § Listening
// for Messages from the Server for GET).
//
// Attach also registers /x/mcp aliases for the install page and OAuth
// .well-known metadata routes. The install page delegates to mcpmetadata
// for parity with /mcp; the .well-known routes are owned by xmcp directly
// so they can dispatch per-backend (see [Service.HandleWellKnownOAuthServerMetadata]).
func Attach(mux goahttp.Muxer, service *Service, metadataService *mcpmetadata.Service) {
	handler := oops.ErrHandle(service.logger, service.ServeMCP).ServeHTTP
	o11y.AttachHandler(mux, http.MethodDelete, RuntimePath, handler)
	o11y.AttachHandler(mux, http.MethodGet, RuntimePath, handler)
	o11y.AttachHandler(mux, http.MethodPost, RuntimePath, handler)

	o11y.AttachHandler(mux, http.MethodGet, "/x/mcp/{mcpSlug}/install", oops.ErrHandle(service.logger, metadataService.ServeInstallPage).ServeHTTP)
	// The install page script URL is hardcoded to /mcp/install-page-{hash}.js
	// inside the rendered install page HTML (see mcpmetadata/impl.go), so the
	// /mcp variant registered by mcp.Attach is what the served HTML actually
	// loads. This is duplicated here (but commented out to prevent errors) to
	// ensure its not missed during future migration of the runtime endpoint
	// from /x/mcp to /mcp.
	// o11y.AttachHandler(mux, http.MethodGet, "/mcp/install-page-{hash}.js", oops.ErrHandle(service.logger, metadataService.ServeInstallPageScript).ServeHTTP)

	o11y.AttachHandler(mux, http.MethodGet, wellknown.OAuthAuthorizationServerPath+"/x/mcp/{mcpSlug}", oops.ErrHandle(service.logger, service.HandleWellKnownOAuthServerMetadata).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodGet, wellknown.OAuthProtectedResourcePath+"/x/mcp/{mcpSlug}", oops.ErrHandle(service.logger, service.HandleWellKnownOAuthProtectedResourceMetadata).ServeHTTP)

	// Issuer-gated OAuth handler family. Each route resolves the slug to
	// an /x/mcp-keyed *mcp.ResolvedMcpEndpoint via [Service.loadResolvedMcpEndpointBySlug]
	// and delegates to the matching mcp.Service.Serve* post-resolution
	// handler.
	//
	// idp_callback and remote_login_callback are mounted only at the
	// slug-less global URLs: the authorize and consent handlers build
	// their redirect_uris via endpoint.IDPCallbackURL and
	// ChallengeManager.callbackURL, both of which always emit
	// `<baseURL>/<RouteBase>/...` without the slug. The handlers recover
	// the originating slug from the cached challenge / login state. /mcp
	// also keeps per-slug variants for back-compat with pre-global-URL
	// clients; /x/mcp is a fresh surface so the dead routes aren't mounted.
	o11y.AttachHandler(mux, http.MethodPost, "/x/mcp/{mcpSlug}/register", oops.ErrHandle(service.logger, service.handleOAuthRegister).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodGet, "/x/mcp/{mcpSlug}/authorize", oops.ErrHandle(service.logger, service.handleOAuthAuthorize).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodGet, "/x/mcp/{mcpSlug}/connect", oops.ErrHandle(service.logger, service.handleOAuthConsent).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodPost, "/x/mcp/{mcpSlug}/connect", oops.ErrHandle(service.logger, service.handleOAuthConsent).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodPost, "/x/mcp/{mcpSlug}/token", oops.ErrHandle(service.logger, service.handleOAuthToken).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodPost, "/x/mcp/{mcpSlug}/revoke", oops.ErrHandle(service.logger, service.handleOAuthRevoke).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodGet, "/x/mcp/idp_callback", oops.ErrHandle(service.logger, service.mcpService.HandleIDPCallback).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodGet, "/x/mcp/remote_login_callback", oops.ErrHandle(service.logger, service.handleRemoteLoginCallback).ServeHTTP)
}

// handleRemoteLoginCallback adapts /x/mcp/remote_login_callback onto
// mcp.Service.HandleRemoteLoginCallback. The
// underlying handler reads the cached RemoteLoginState's RouteBase so the
// post-callback redirect lands back on /x/mcp/{slug}/connect — populated
// when the consent renderer built the parent challenge.
func (s *Service) handleRemoteLoginCallback(w http.ResponseWriter, r *http.Request) error {
	return s.mcpService.HandleRemoteLoginCallback(w, r) //nolint:wrapcheck // thin passthrough; the inner handler already writes the HTTP response.
}

// handleOAuthRegister adapts the chi /x/mcp/{mcpSlug}/register route to
// mcp.Service.ServeRegister by resolving the slug to an ResolvedMcpEndpoint.
func (s *Service) handleOAuthRegister(w http.ResponseWriter, r *http.Request) error {
	endpoint, err := s.resolveOAuthEndpoint(r)
	if err != nil {
		return err
	}
	if err := s.mcpService.ServeRegister(w, r, endpoint); err != nil {
		return fmt.Errorf("serve oauth register: %w", err)
	}
	return nil
}

// handleOAuthAuthorize adapts the chi /x/mcp/{mcpSlug}/authorize route
// to mcp.Service.ServeAuthorize by resolving the slug to an
// ResolvedMcpEndpoint.
func (s *Service) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) error {
	endpoint, err := s.resolveOAuthEndpoint(r)
	if err != nil {
		return err
	}
	if err := s.mcpService.ServeAuthorize(w, r, endpoint); err != nil {
		return fmt.Errorf("serve oauth authorize: %w", err)
	}
	return nil
}

// handleOAuthConsent adapts the chi /x/mcp/{mcpSlug}/connect (GET/POST)
// route to mcp.Service.ServeConsent.
func (s *Service) handleOAuthConsent(w http.ResponseWriter, r *http.Request) error {
	endpoint, err := s.resolveOAuthEndpoint(r)
	if err != nil {
		return err
	}
	if err := s.mcpService.ServeConsent(w, r, endpoint); err != nil {
		return fmt.Errorf("serve oauth consent: %w", err)
	}
	return nil
}

// handleOAuthToken adapts the chi /x/mcp/{mcpSlug}/token route to
// mcp.Service.ServeToken.
func (s *Service) handleOAuthToken(w http.ResponseWriter, r *http.Request) error {
	endpoint, err := s.resolveOAuthEndpoint(r)
	if err != nil {
		return err
	}
	if err := s.mcpService.ServeToken(w, r, endpoint); err != nil {
		return fmt.Errorf("serve oauth token: %w", err)
	}
	return nil
}

// handleOAuthRevoke adapts the chi /x/mcp/{mcpSlug}/revoke route to
// mcp.Service.ServeRevoke.
func (s *Service) handleOAuthRevoke(w http.ResponseWriter, r *http.Request) error {
	endpoint, err := s.resolveOAuthEndpoint(r)
	if err != nil {
		return err
	}
	if err := s.mcpService.ServeRevoke(w, r, endpoint); err != nil {
		return fmt.Errorf("serve oauth revoke: %w", err)
	}
	return nil
}

// resolveOAuthEndpoint reads the mcpSlug chi param and resolves it to
// an issuer-gated *mcp.ResolvedMcpEndpoint via the /x/mcp mcp_endpoints →
// mcp_servers path. Returned to the per-handler adapters above.
func (s *Service) resolveOAuthEndpoint(r *http.Request) (*mcp.ResolvedMcpEndpoint, error) {
	ctx := r.Context()
	slug := chi.URLParam(r, "mcpSlug")
	if slug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}
	logger := s.logger.With(attr.SlogToolsetMCPSlug(slug))
	return s.loadResolvedMcpEndpointBySlug(ctx, logger, slug)
}

// newHeadersRepo returns a per-request headers wrapper bound to the service
// DB pool. Using the wrapper ensures secret header values are transparently
// decrypted before reaching the proxy.
func (s *Service) newHeadersRepo() *remotemcp.Headers {
	return remotemcp.NewHeaders(s.logger, s.db, s.enc)
}
