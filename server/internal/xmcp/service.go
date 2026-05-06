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
	"log/slog"
	"net/http"
	"net/url"

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
)

// RuntimePath is the experimental runtime path served by this package.
const RuntimePath = "/x/mcp/{slug}"

// Service owns dependencies for the experimental MCP runtime endpoint.
type Service struct {
	logger                       *slog.Logger
	tracer                       trace.Tracer
	db                           *pgxpool.Pool
	enc                          *encryption.Client
	authz                        *authz.Engine
	mcpService                   *mcp.Service
	serverURL                    *url.URL
	guardianPolicy               *guardian.Policy
	proxyMetrics                 *proxy.Metrics
	toolUsageLimitsInterceptor   *ToolUsageLimitsInterceptor
	toolUsageTrackingInterceptor *ToolUsageTrackingInterceptor
}

// NewService constructs a Service with its full dependency graph wired up.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	enc *encryption.Client,
	authzEngine *authz.Engine,
	guardianPolicy *guardian.Policy,
	billingRepo billing.Repository,
	billingTracker billing.Tracker,
	mcpService *mcp.Service,
	serverURL *url.URL,
) *Service {
	logger = logger.With(attr.SlogComponent("xmcp"))

	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/xmcp")

	return &Service{
		logger:                       logger,
		tracer:                       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/xmcp"),
		db:                           db,
		enc:                          enc,
		authz:                        authzEngine,
		mcpService:                   mcpService,
		serverURL:                    serverURL,
		guardianPolicy:               guardianPolicy,
		proxyMetrics:                 proxy.NewMetrics(meter, logger),
		toolUsageLimitsInterceptor:   NewToolUsageLimitsInterceptor(billingRepo, logger),
		toolUsageTrackingInterceptor: NewToolUsageTrackingInterceptor(billingTracker, logger),
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
}

// newHeadersRepo returns a per-request headers wrapper bound to the service
// DB pool. Using the wrapper ensures secret header values are transparently
// decrypted before reaching the proxy.
func (s *Service) newHeadersRepo() *remotemcp.Headers {
	return remotemcp.NewHeaders(s.logger, s.db, s.enc)
}
