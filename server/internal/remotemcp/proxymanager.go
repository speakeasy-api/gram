package remotemcp

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/interceptors"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/toolcallobserver"
)

// ProxyManager builds configured remote-MCP proxies wired up with the
// MCP-aware interceptor stack: usage limits and tracking, per-tool RBAC,
// argument scrubbing, ClickHouse logging, OTel counters, and PostHog event
// capture.
//
// One factory is constructed at server startup and reused across requests.
// The interceptors that hold no per-request state (usage limits/tracking)
// are constructed once on the factory; the rest are instantiated per-call in
// [ProxyManager.Build] so the closure over the per-server correlation ids
// stays request-scoped.
type ProxyManager struct {
	logger         *slog.Logger
	tracer         trace.Tracer
	guardianPolicy *guardian.Policy
	authz          *authz.Engine
	posthog        *posthog.Posthog
	telemLogger    *tm.Logger

	proxyMetrics     *proxy.Metrics
	mcpMetrics       *ProxyMetrics
	identityCoverage *mcptoolexecution.IdentityCoverageCheckpoint

	// requestOTELCounterInterceptor emits the shared per-request census
	// counter (mcp.request) for the remote- and tunnel-backed /x/mcp traffic,
	// which never reaches the mcp package's dispatch where the hosted and
	// platform surfaces emit it.
	requestOTELCounterInterceptor *RequestOTELCounterInterceptor

	toolDispositions ToolDispositionResolver

	toolsCallUsageLimitsInterceptor       *ToolsCallUsageLimitsInterceptor
	toolsCallUsageTrackingInterceptor     *ToolsCallUsageTrackingInterceptor
	resourcesReadUsageLimitsInterceptor   *ResourcesReadUsageLimitsInterceptor
	resourcesReadUsageTrackingInterceptor *ResourcesReadUsageTrackingInterceptor
	platformMCPSelectedUseRecorder        toolcallobserver.SuccessRecorder

	// witnessStore backs listing-witnessed live enforcement of live
	// annotation grants: the list interceptor records the rows each session
	// was shown, the call interceptor matches against them.
	witnessStore *toolfilter.SessionToolWitnessStore
}

// NewProxyManager wires the MCP-aware proxy stack with its dependencies.
// The factory is safe for reuse across requests; only the per-request
// interceptors materialised in [ProxyManager.Build] are instantiated on
// each call.
func NewProxyManager(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	guardianPolicy *guardian.Policy,
	authzEngine *authz.Engine,
	posthogClient *posthog.Posthog,
	telemLogger *tm.Logger,
	billingRepo billing.Repository,
	billingTracker billing.Tracker,
	toolDispositions ToolDispositionResolver,
	platformMCPSelectedUseRecorder toolcallobserver.SuccessRecorder,
	witnessStore *toolfilter.SessionToolWitnessStore,
) *ProxyManager {
	logger = logger.With(attr.SlogComponent("remotemcp"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/remotemcp")
	mcpMetrics := NewProxyMetrics(meter, logger)

	return &ProxyManager{
		logger:                                logger,
		tracer:                                tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/remotemcp"),
		guardianPolicy:                        guardianPolicy,
		authz:                                 authzEngine,
		posthog:                               posthogClient,
		telemLogger:                           telemLogger,
		proxyMetrics:                          proxy.NewMetrics(meter, logger),
		mcpMetrics:                            mcpMetrics,
		identityCoverage:                      mcptoolexecution.NewIdentityCoverageCheckpoint(db, mcpMetrics),
		requestOTELCounterInterceptor:         NewRequestOTELCounterInterceptor(mcpmetrics.NewRequestCounter(meter, logger)),
		toolDispositions:                      toolDispositions,
		toolsCallUsageLimitsInterceptor:       NewToolsCallUsageLimitsInterceptor(billingRepo, logger),
		toolsCallUsageTrackingInterceptor:     NewToolsCallUsageTrackingInterceptor(billingTracker, logger),
		resourcesReadUsageLimitsInterceptor:   NewResourcesReadUsageLimitsInterceptor(billingRepo, logger),
		resourcesReadUsageTrackingInterceptor: NewResourcesReadUsageTrackingInterceptor(billingTracker, logger),
		platformMCPSelectedUseRecorder:        platformMCPSelectedUseRecorder,
		witnessStore:                          witnessStore,
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
//
// mcpServerID is the mcp_servers row id (NOT the remote_mcp_servers id on
// server). It is the RBAC ResourceID for the per-tool `mcp:connect` checks
// so they resolve grants against the same mcp_servers row that the handler's
// upfront server-level `mcp:connect` check uses, keeping per-tool and
// server-level authorization consistent for the same caller. server.ID still
// drives telemetry/logging dimensions, which are keyed by remote_mcp_servers.
//
// selection is the caller's consent-screen session tool selection. Nil means
// all tools: no selection interceptors attach and strict typed handling stays
// off. Non-nil attaches exact-name tools/list filtering and tools/call
// rejection after any RBAC interceptors, and turns on the proxy's strict
// typed handling so malformed tools traffic fails closed instead of relaying.
func (f *ProxyManager) Build(
	logger *slog.Logger,
	server *remotemcprepo.RemoteMcpServer,
	mcpServerID string,
	headers []remotemcprepo.RemoteMcpServerHeader,
	visibility string,
	projectID string,
	upstreamAuth string,
	wwwAuthenticate string,
	selection *toolfilter.SessionSelection,
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

	return f.BuildTarget(logger, proxy.ServerIdentity{
		RemoteMCPServerID:   server.ID.String(),
		TunneledMCPServerID: "",
		McpServerID:         mcpServerID,
	}, server.Url, configured, visibility, projectID, upstreamAuth, wwwAuthenticate, selection)
}

func (f *ProxyManager) BuildTarget(
	logger *slog.Logger,
	identity proxy.ServerIdentity,
	upstreamURL string,
	headers []proxy.ConfiguredHeader,
	visibility string,
	projectID string,
	upstreamAuth string,
	wwwAuthenticate string,
	selection *toolfilter.SessionSelection,
) *proxy.Proxy {
	// Per-request instance: the interceptor holds a single nilable start
	// timestamp set by the request side and consumed by the response side.
	// A fresh instance per Build makes that field's lifetime match the
	// proxy's, so a stale timestamp from a failure path (request fires,
	// response doesn't) is reclaimed when the proxy is dropped.
	clickHouseLogInterceptor := NewToolsCallClickHouseLogInterceptor(f.telemLogger, identity, logger)

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
	// The x-gram-toolset-id strip is attached unconditionally — public AND
	// private — because the property is Gram's own envelope rather than
	// anything scoped to an identity or a risk policy. It is a no-op for
	// the arguments that don't carry it.
	toolsCallReqInterceptors := []proxy.ToolsCallRequestInterceptor{
		NewToolsCallOTELCounterInterceptor(f.mcpMetrics, f.identityCoverage, identity, logger),
		f.toolsCallUsageLimitsInterceptor,
		NewToolsCallStripToolsetIDInterceptor(logger),
		clickHouseLogInterceptor,
	}
	if visibility == mcpservers.VisibilityPrivate {
		toolsCallReqInterceptors = append(toolsCallReqInterceptors,
			NewToolsCallAuthzInterceptor(f.authz, f.toolDispositions, identity.McpServerID, projectID, logger),
		)
	}

	toolsListRespInterceptors := []proxy.ToolsListResponseInterceptor{}
	if visibility == mcpservers.VisibilityPrivate {
		toolsListRespInterceptors = append(toolsListRespInterceptors,
			NewToolsListMCPConnectFilterInterceptor(f.authz, f.toolDispositions, identity.McpServerID, projectID, logger),
		)
	}

	// Session-selection enforcement runs after the RBAC interceptors above so
	// the effective catalog is the intersection of RBAC and consent.
	if selection != nil {
		selectionInterceptor := NewSessionSelectionInterceptor(selection, f.witnessStore)
		toolsCallReqInterceptors = append(toolsCallReqInterceptors, selectionInterceptor)
		toolsListRespInterceptors = append(toolsListRespInterceptors, selectionInterceptor)
	}

	// Resources request chain: free-tier ToolCalls usage limits apply to
	// resources/read invocations alongside tools/call. Per-resource RBAC
	// and the resources/list RBAC filter are deferred to a follow-up —
	// the proxy interceptor surface is in place so they can attach later
	// without touching the proxy package again.
	toolsCallResponseInterceptors := []proxy.ToolsCallResponseInterceptor{
		f.toolsCallUsageTrackingInterceptor,
		clickHouseLogInterceptor,
	}
	if f.platformMCPSelectedUseRecorder != nil && identity.RemoteMCPServerID != "" {
		toolsCallResponseInterceptors = append(toolsCallResponseInterceptors, NewPlatformMCPSelectedUseInterceptor(f.platformMCPSelectedUseRecorder, identity))
	}

	return &proxy.Proxy{
		GuardianPolicy:              f.guardianPolicy,
		GuardianClientOptions:       nil,
		Logger:                      logger,
		Tracer:                      f.tracer,
		NonStreamingTimeout:         proxy.DefaultNonStreamingTimeout,
		StreamingTimeout:            proxy.DefaultStreamingTimeout,
		Metrics:                     f.proxyMetrics,
		MaxBufferedBodyBytes:        proxy.DefaultMaxBufferedBodyBytes,
		Identity:                    identity,
		RemoteURL:                   upstreamURL,
		Headers:                     headers,
		AuthorizationOverride:       upstreamAuth,
		UpstreamResponseRetryer:     nil,
		UpstreamResponseInterceptor: nil,
		DisableRedirects:            false,
		StrictToolSelection:         selection != nil,
		WWWAuthenticate:             wwwAuthenticate,
		// The census runs first so every parsed request is counted, including
		// those a later interceptor rejects — matching the hosted dispatch,
		// which records before the method switch can refuse a request.
		UserRequestInterceptors: []proxy.UserRequestInterceptor{
			f.requestOTELCounterInterceptor,
			interceptors.NewFigma(upstreamURL, logger),
		},
		InitializeRequestInterceptors: []proxy.InitializeRequestInterceptor{
			NewInitializePostHogEventInterceptor(f.posthog, identity, logger),
		},
		RemoteMessageInterceptors:     nil,
		ToolsCallRequestInterceptors:  toolsCallReqInterceptors,
		ToolsCallResponseInterceptors: toolsCallResponseInterceptors,
		ToolsListRequestInterceptors: []proxy.ToolsListRequestInterceptor{
			NewToolsListPostHogEventInterceptor(f.posthog, identity, logger),
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
