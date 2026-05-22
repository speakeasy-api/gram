package xmcp

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// ResourcesReadUsageTrackingInterceptor emits a [billing.ToolCallUsageEvent]
// for each resources/read response so Remote MCP Server resource reads feed
// the same Polar meter that gates free-tier usage on the existing /mcp
// endpoint. It mirrors [ToolsCallUsageTrackingInterceptor] — resources/read
// is accounted to the same `ToolCalls` counter as tools/call, with the
// resource URI carried on the `ResourceURI` field so downstream consumers
// can distinguish the two. It is a [proxy.ResourcesReadResponseInterceptor]:
// it runs after the generic [proxy.RemoteMessageInterceptor] chain has
// accepted the response and before the payload is relayed to the user.
//
// Tracking is fire-and-forget: events are emitted in a goroutine bound to a
// context derived via [context.WithoutCancel] so the call completes even if
// the inbound request context cancels mid-relay. Missing auth context is
// treated as a no-op and logged so operators can spot misconfiguration
// without taking down resource reads.
type ResourcesReadUsageTrackingInterceptor struct {
	tracker billing.Tracker
	logger  *slog.Logger
}

var _ proxy.ResourcesReadResponseInterceptor = (*ResourcesReadUsageTrackingInterceptor)(nil)

// NewResourcesReadUsageTrackingInterceptor constructs an interceptor bound to
// the given billing tracker. The same instance can be reused across requests.
func NewResourcesReadUsageTrackingInterceptor(tracker billing.Tracker, logger *slog.Logger) *ResourcesReadUsageTrackingInterceptor {
	return &ResourcesReadUsageTrackingInterceptor{
		tracker: tracker,
		logger:  logger,
	}
}

// Name implements [proxy.ResourcesReadResponseInterceptor].
func (i *ResourcesReadUsageTrackingInterceptor) Name() string {
	return "resources-read-usage-tracking"
}

// InterceptResourcesReadResponse implements [proxy.ResourcesReadResponseInterceptor].
// It emits a billing event for every observed resources/read response — paid
// tiers included — so Polar metering matches the existing /mcp surface.
// Always returns nil: tracking is best-effort and must not block the response
// from reaching the user.
//
// The event populates `ResourceURI` from the originating request params and
// leaves `ToolName` empty — the resource definition's display name is only
// available after a prior resources/list response is correlated through a
// per-session cache, which lives outside the proxy today.
func (i *ResourcesReadUsageTrackingInterceptor) InterceptResourcesReadResponse(ctx context.Context, read *proxy.ResourcesReadResponse) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		i.logger.WarnContext(ctx, "skipping resource read usage tracking: missing auth context",
			attr.SlogComponent("xmcp"))
		return nil
	}

	resourceURI := read.Request.Params.URI

	// Defensive: the proxy's typed-view constructor enforces a single
	// JSON-RPC request, but this interceptor is publicly constructible —
	// guard the index access so a caller handing us an empty slice gets a
	// zero RequestBytes instead of a panic.
	var requestBytes int64
	if msgs := read.Request.UserRequest.JSONRPCMessages; len(msgs) > 0 {
		if rpcReq, ok := msgs[0].(*jsonrpc.Request); ok {
			requestBytes = int64(len(rpcReq.Params))
		}
	}

	var outputBytes int64
	if rpcResp, ok := read.RemoteMessage.Message.(*jsonrpc.Response); ok {
		outputBytes = int64(len(rpcResp.Result))
	}

	var statusCode int
	if read.RemoteMessage.RemoteHTTPResponse != nil {
		statusCode = read.RemoteMessage.RemoteHTTPResponse.StatusCode
	}

	var sessionID *string
	if read.RemoteMessage.UserHTTPRequest != nil {
		sessionID = conv.PtrEmpty(read.RemoteMessage.UserHTTPRequest.Header.Get("Mcp-Session-Id"))
	}

	projectID := authCtx.ProjectID.String()
	event := billing.ToolCallUsageEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		RequestBytes:          requestBytes,
		OutputBytes:           outputBytes,
		ToolURN:               "",
		ToolName:              "",
		ResourceURI:           resourceURI,
		ProjectID:             projectID,
		ProjectSlug:           authCtx.ProjectSlug,
		OrganizationSlug:      conv.PtrEmpty(authCtx.OrganizationSlug),
		ToolsetSlug:           nil,
		ChatID:                nil,
		MCPURL:                nil,
		Type:                  billing.ToolCallTypeExternalMCP,
		ResponseStatusCode:    statusCode,
		ToolsetID:             nil,
		MCPSessionID:          sessionID,
		FunctionCPUUsage:      nil,
		FunctionMemUsage:      nil,
		FunctionExecutionTime: nil,
	}

	go i.tracker.TrackToolCallUsage(context.WithoutCancel(ctx), event)

	return nil
}
