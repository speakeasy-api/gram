package remotemcp

import (
	"log/slog"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

// ProxyManager builds configured remote-MCP proxies wired up with the
// MCP-aware interceptor stack: usage limits and tracking, per-tool RBAC,
// shadow-MCP validation/injection, ClickHouse logging, OTel counters, and
// PostHog event capture.
//
// One factory is constructed at server startup and reused across requests.
// The interceptors that hold no per-request state (usage limits/tracking,
// PostHog initialize emitter) are constructed once on the factory; the
// rest are instantiated per-call in [ProxyManager.Build] so the closure
// over the remote-server id stays request-scoped.
type ProxyManager struct {
	logger          *slog.Logger
	tracer          trace.Tracer
	guardianPolicy  *guardian.Policy
	authz           *authz.Engine
	shadowmcpClient *shadowmcp.Client
	posthog         *posthog.Posthog
	telemLogger     *tm.Logger

	proxyMetrics *proxy.Metrics
	mcpMetrics   *ProxyMetrics

	toolsCallUsageLimitsInterceptor       *ToolsCallUsageLimitsInterceptor
	toolsCallUsageTrackingInterceptor     *ToolsCallUsageTrackingInterceptor
	resourcesReadUsageLimitsInterceptor   *ResourcesReadUsageLimitsInterceptor
	resourcesReadUsageTrackingInterceptor *ResourcesReadUsageTrackingInterceptor
	initializePostHogEventInterceptor     *InitializePostHogEventInterceptor
}

// NewProxyManager wires the MCP-aware proxy stack with its dependencies.
// The factory is safe for reuse across requests; only the per-request
// interceptors materialised in [ProxyManager.Build] are instantiated on
// each call.
func NewProxyManager(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	guardianPolicy *guardian.Policy,
	authzEngine *authz.Engine,
	shadowmcpClient *shadowmcp.Client,
	posthogClient *posthog.Posthog,
	telemLogger *tm.Logger,
	billingRepo billing.Repository,
	billingTracker billing.Tracker,
) *ProxyManager {
	logger = logger.With(attr.SlogComponent("remotemcp"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/remotemcp")

	return &ProxyManager{
		logger:                                logger,
		tracer:                                tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/remotemcp"),
		guardianPolicy:                        guardianPolicy,
		authz:                                 authzEngine,
		shadowmcpClient:                       shadowmcpClient,
		posthog:                               posthogClient,
		telemLogger:                           telemLogger,
		proxyMetrics:                          proxy.NewMetrics(meter, logger),
		mcpMetrics:                            NewProxyMetrics(meter, logger),
		toolsCallUsageLimitsInterceptor:       NewToolsCallUsageLimitsInterceptor(billingRepo, logger),
		toolsCallUsageTrackingInterceptor:     NewToolsCallUsageTrackingInterceptor(billingTracker, logger),
		resourcesReadUsageLimitsInterceptor:   NewResourcesReadUsageLimitsInterceptor(billingRepo, logger),
		resourcesReadUsageTrackingInterceptor: NewResourcesReadUsageTrackingInterceptor(billingTracker, logger),
		initializePostHogEventInterceptor:     NewInitializePostHogEventInterceptor(posthogClient, logger),
	}
}

// Build constructs a configured [*proxy.Proxy] for a single request against
// the given remote MCP server. logger should already carry the slug and
// remote-server id attributes the caller wants to propagate to interceptor
// log lines.
//
// upstreamAuth is the Authorization header value to forward upstream
// (typically the user-session JWT collected by the issuer gate); the proxy
// always drops the incoming Authorization header and only sends an
// upstream Authorization when this is non-empty.
//
// visibility scopes which interceptors attach: per-tool RBAC fires on
// private servers only since public servers bypass server-level RBAC.
// projectID is forwarded to the per-tool authz interceptor as a dimension
// so project-scoped grants can match.
func (f *ProxyManager) Build(
	logger *slog.Logger,
	server *remotemcprepo.RemoteMcpServer,
	headers []remotemcprepo.RemoteMcpServerHeader,
	visibility string,
	projectID string,
	upstreamAuth string,
) *proxy.Proxy {
	configured := make([]proxy.ConfiguredHeader, 0, len(headers))
	for _, h := range headers {
		configured = append(configured, proxy.ConfiguredHeader{
			Name:                   h.Name,
			StaticValue:            h.Value.String,
			ValueFromRequestHeader: h.ValueFromRequestHeader.String,
			IsRequired:             h.IsRequired,
		})
	}

	serverID := server.ID.String()

	// Per-request instance: the interceptor holds a single nilable start
	// timestamp set by the request side and consumed by the response side.
	// A fresh instance per Build makes that field's lifetime match the
	// proxy's, so a stale timestamp from a failure path (request fires,
	// response doesn't) is reclaimed when the proxy is dropped.
	clickHouseLogInterceptor := NewToolsCallClickHouseLogInterceptor(f.telemLogger, serverID, logger)

	// Counter records every attempted tools/call, including those later
	// rejected by limits or per-tool authz. This mirrors /mcp, where
	// RecordMCPToolCall fires before the per-tool RBAC check in
	// rpc_tools_call.go.
	//
	// Per-tool RBAC interceptors (ToolsCallAuthzInterceptor on the
	// request side; ToolsListMCPConnectFilterInterceptor on the response
	// side) are only attached for private-visibility servers. Public
	// servers bypass server-level RBAC by design, so per-tool RBAC is
	// also skipped — otherwise an unauthenticated public caller would
	// be unable to invoke any tool, and the tools/list filter would
	// have no grants to consult.
	//
	// The shadow-MCP interceptors are attached unconditionally — public
	// AND private — because they enforce a project-scoped risk policy,
	// not an identity-scoped grant. A project that enables tool-identity
	// capture wants the property injected and validated on every call
	// the proxy serves, regardless of whether the underlying transport
	// authenticated the caller. The pair self-gates via
	// shadowmcp.Client.IsEnabledForProject at intercept time; the lookup
	// is Redis-cached (15-minute TTL) so the hot-path cost when the
	// policy is disabled is a single cache GET.
	toolsCallReqInterceptors := []proxy.ToolsCallRequestInterceptor{
		NewToolsCallOTELCounterInterceptor(f.mcpMetrics, serverID, logger),
		f.toolsCallUsageLimitsInterceptor,
		NewToolsCallShadowMCPValidateAndStripInterceptor(f.shadowmcpClient, serverID, projectID, logger),
		clickHouseLogInterceptor,
	}
	if visibility == mcpservers.VisibilityPrivate {
		toolsCallReqInterceptors = append(toolsCallReqInterceptors,
			NewToolsCallAuthzInterceptor(f.authz, serverID, projectID, logger),
		)
	}

	// ToolsList response chain ordering: filter first (drop tools the
	// caller can't see), then inject (only mutate schemas of tools that
	// survive the filter — saves work and prevents leaking the
	// proxy-only x-gram-toolset-id property on tools the caller couldn't
	// invoke anyway).
	toolsListRespInterceptors := []proxy.ToolsListResponseInterceptor{}
	if visibility == mcpservers.VisibilityPrivate {
		toolsListRespInterceptors = append(toolsListRespInterceptors,
			NewToolsListMCPConnectFilterInterceptor(f.authz, serverID, projectID, logger),
		)
	}
	toolsListRespInterceptors = append(toolsListRespInterceptors,
		NewToolsListShadowMCPInjectInterceptor(f.shadowmcpClient, serverID, projectID, logger),
	)

	// Resources request chain: free-tier ToolCalls usage limits apply to
	// resources/read invocations alongside tools/call. Per-resource RBAC
	// and the resources/list RBAC filter are deferred to a follow-up —
	// the proxy interceptor surface is in place so they can attach later
	// without touching the proxy package again.
	return &proxy.Proxy{
		GuardianPolicy:          f.guardianPolicy,
		Logger:                  logger,
		Tracer:                  f.tracer,
		NonStreamingTimeout:     proxy.DefaultNonStreamingTimeout,
		StreamingTimeout:        proxy.DefaultStreamingTimeout,
		Metrics:                 f.proxyMetrics,
		MaxBufferedBodyBytes:    proxy.DefaultMaxBufferedBodyBytes,
		ServerID:                serverID,
		RemoteURL:               server.Url,
		Headers:                 configured,
		AuthorizationOverride:   upstreamAuth,
		UserRequestInterceptors: nil,
		InitializeRequestInterceptors: []proxy.InitializeRequestInterceptor{
			f.initializePostHogEventInterceptor,
		},
		RemoteMessageInterceptors:    nil,
		ToolsCallRequestInterceptors: toolsCallReqInterceptors,
		ToolsCallResponseInterceptors: []proxy.ToolsCallResponseInterceptor{
			f.toolsCallUsageTrackingInterceptor,
			clickHouseLogInterceptor,
		},
		ToolsListRequestInterceptors: []proxy.ToolsListRequestInterceptor{
			NewToolsListPostHogEventInterceptor(f.posthog, serverID, logger),
		},
		ToolsListResponseInterceptors: toolsListRespInterceptors,
		ResourcesReadRequestInterceptors: []proxy.ResourcesReadRequestInterceptor{
			f.resourcesReadUsageLimitsInterceptor,
		},
		ResourcesReadResponseInterceptors: []proxy.ResourcesReadResponseInterceptor{
			f.resourcesReadUsageTrackingInterceptor,
		},
		ResourcesListRequestInterceptors:  nil,
		ResourcesListResponseInterceptors: nil,
	}
}
