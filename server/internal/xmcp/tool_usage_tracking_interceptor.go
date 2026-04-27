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

// ToolUsageTrackingInterceptor emits a [billing.ToolCallUsageEvent] for each
// tools/call response so Remote MCP Server invocations feed the same Polar
// meter that gates free-tier usage on the existing /mcp endpoint. It is a
// [proxy.ToolsCallResponseInterceptor]: it runs after the generic
// [proxy.RemoteMessageInterceptor] chain has accepted the response and before
// the payload is relayed to the user.
//
// Tracking is fire-and-forget: events are emitted in a goroutine bound to a
// context derived via [context.WithoutCancel] so the call completes even if
// the inbound request context cancels mid-relay. Missing auth context is
// treated as a no-op and logged so operators can spot misconfiguration
// without taking down tool invocation.
type ToolUsageTrackingInterceptor struct {
	tracker billing.Tracker
	logger  *slog.Logger
}

var _ proxy.ToolsCallResponseInterceptor = (*ToolUsageTrackingInterceptor)(nil)

// NewToolUsageTrackingInterceptor constructs an interceptor bound to the
// given billing tracker. The same instance can be reused across requests.
func NewToolUsageTrackingInterceptor(tracker billing.Tracker, logger *slog.Logger) *ToolUsageTrackingInterceptor {
	return &ToolUsageTrackingInterceptor{
		tracker: tracker,
		logger:  logger,
	}
}

// Name implements [proxy.ToolsCallResponseInterceptor].
func (i *ToolUsageTrackingInterceptor) Name() string {
	return "tool-usage-tracking"
}

// InterceptToolsCallResponse implements [proxy.ToolsCallResponseInterceptor].
// It emits a billing event for every observed tools/call response — paid
// tiers included — so Polar metering matches the existing /mcp surface.
// Always returns nil: tracking is best-effort and must not block the response
// from reaching the user.
func (i *ToolUsageTrackingInterceptor) InterceptToolsCallResponse(ctx context.Context, call *proxy.ToolsCallResponse) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		i.logger.WarnContext(ctx, "skipping tool call usage tracking: missing auth context",
			attr.SlogComponent("xmcp"))
		return nil
	}

	toolName := call.Request.Params.Name
	requestBytes := int64(len(call.Request.Params.Arguments))

	var outputBytes int64
	if rpcResp, ok := call.RemoteMessage.Message.(*jsonrpc.Response); ok {
		outputBytes = int64(len(rpcResp.Result))
	}

	var statusCode int
	if call.RemoteMessage.RemoteHTTPResponse != nil {
		statusCode = call.RemoteMessage.RemoteHTTPResponse.StatusCode
	}

	var sessionID *string
	if call.RemoteMessage.UserHTTPRequest != nil {
		sessionID = conv.PtrEmpty(call.RemoteMessage.UserHTTPRequest.Header.Get("Mcp-Session-Id"))
	}

	projectID := authCtx.ProjectID.String()
	event := billing.ToolCallUsageEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		RequestBytes:          requestBytes,
		OutputBytes:           outputBytes,
		ToolURN:               "",
		ToolName:              toolName,
		ResourceURI:           "",
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
