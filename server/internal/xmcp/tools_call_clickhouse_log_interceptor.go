package xmcp

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
)

// DurationMissingKey marks rows where the request-side start timestamp was
// not observed (cancellation, panic, transport oddity). The materialized
// duration is set to zero in that case, so the sentinel lets queries
// distinguish "zero ms call" from "duration unknown" instead of silently
// dragging the latency histogram toward zero.
var DurationMissingKey = attribute.Key("gram.telemetry.duration_missing")

// ToolsCallClickHouseLogInterceptor emits one structured ClickHouse
// telemetry_logs row per Remote MCP tools/call. The attribute shape matches
// the dashboard's getObservabilityOverview query path (gram_urn starts with
// "tools:", per-tool failure derived from http.response.status_code, latency
// from http.server.request.duration).
//
// One instance implements both [proxy.ToolsCallRequestInterceptor] and
// [proxy.ToolsCallResponseInterceptor]. xmcp constructs a fresh interceptor
// per HTTP request inside [Service.buildProxy], and [proxy.Proxy.Post] fires
// the request and response chains sequentially on a single goroutine for at
// most one tools/call per request — so a single nilable start timestamp is
// enough state to compute duration, no map or mutex required. If a future
// refactor makes [proxy.Proxy] (or this interceptor) long-lived across
// requests, this needs to revert to per-call keying.
//
// Emission is fire-and-forget on a goroutine bound to
// [context.WithoutCancel] so ClickHouse latency never appears in the user's
// tool-call tail latency.
type ToolsCallClickHouseLogInterceptor struct {
	telemLogger *tm.Logger
	serverID    string
	logger      *slog.Logger

	// start is set by the request interceptor and consumed by the
	// response interceptor. Nil means the request side never fired, in
	// which case the response path emits the duration-missing sentinel.
	start *time.Time
}

var (
	_ proxy.ToolsCallRequestInterceptor  = (*ToolsCallClickHouseLogInterceptor)(nil)
	_ proxy.ToolsCallResponseInterceptor = (*ToolsCallClickHouseLogInterceptor)(nil)
)

// NewToolsCallClickHouseLogInterceptor constructs an interceptor bound to the
// given telemetry logger and Remote MCP Server id. Construct one per request
// (or per buildProxy call) so the server-id attribute is closed over without
// re-deriving it per emission.
func NewToolsCallClickHouseLogInterceptor(telemLogger *tm.Logger, serverID string, logger *slog.Logger) *ToolsCallClickHouseLogInterceptor {
	return &ToolsCallClickHouseLogInterceptor{
		telemLogger: telemLogger,
		serverID:    serverID,
		logger:      logger,
		start:       nil,
	}
}

// Name implements [proxy.ToolsCallRequestInterceptor] and
// [proxy.ToolsCallResponseInterceptor].
func (i *ToolsCallClickHouseLogInterceptor) Name() string {
	return "tools-call-clickhouse-log"
}

// InterceptToolsCallRequest stashes the call's start time. Always returns
// nil — emission is best-effort and must not block tool invocation.
func (i *ToolsCallClickHouseLogInterceptor) InterceptToolsCallRequest(_ context.Context, _ *proxy.ToolsCallRequest) error {
	i.start = new(time.Now())
	return nil
}

// InterceptToolsCallResponse builds a [tm.LogParams] from the response, the
// stashed start time (or a duration-missing sentinel), and the request auth
// context, and emits it asynchronously to ClickHouse. Always returns nil.
func (i *ToolsCallClickHouseLogInterceptor) InterceptToolsCallResponse(ctx context.Context, call *proxy.ToolsCallResponse) error {
	if call == nil || call.Request == nil || call.Request.Params == nil {
		return nil
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		i.logger.WarnContext(ctx, "skipping tools/call clickhouse log: missing auth context",
			attr.SlogComponent("xmcp"))
		return nil
	}

	end := time.Now()
	var durationSec float64
	durationMissing := i.start == nil
	if i.start != nil {
		durationSec = end.Sub(*i.start).Seconds()
	}

	toolName := call.Request.Params.Name
	requestBytes := int64(len(call.Request.Params.Arguments))

	var outputBytes int64
	if call.RemoteMessage != nil {
		if rpcResp, ok := call.RemoteMessage.Message.(*jsonrpc.Response); ok && rpcResp != nil {
			outputBytes = int64(len(rpcResp.Result))
		}
	}

	statusCode := upstreamStatusCode(call)

	// Synthetic URN: the dashboard's tool-call queries filter on
	// startsWith(gram_urn, 'tools:'), so the prefix is required. Use the
	// remote MCP server's UUID (stable) rather than its slug (mutable) in
	// the source segment so historical URNs remain queryable across renames.
	// urn.ParseTool will reject names with characters outside SlugPatternRE
	// (uppercase, dots, etc.), which means the materialized tool_source
	// column may be empty for those tool names — acceptable; gram_urn
	// (used for grouping) and tool_name (separate materialized column from
	// gram.tool.name attribute) are still populated correctly.
	toolURN := "tools:externalmcp:" + i.serverID + ":" + toolName

	logAttrs := tm.HTTPLogAttributes{
		attr.EventSourceKey:       string(tm.EventSourceToolCall),
		attr.RemoteMCPServerIDKey: i.serverID,
	}
	logAttrs.RecordDuration(durationSec)
	logAttrs.RecordStatusCode(statusCode)
	logAttrs.RecordRequestBody(requestBytes)
	logAttrs.RecordResponseBody(outputBytes)
	logAttrs.RecordTraceContext(ctx)
	if durationMissing {
		logAttrs[DurationMissingKey] = true
	}
	if authCtx.UserID != "" {
		logAttrs[attr.UserIDKey] = authCtx.UserID
	}
	if authCtx.ExternalUserID != "" {
		logAttrs[attr.ExternalUserIDKey] = authCtx.ExternalUserID
	}
	if authCtx.APIKeyID != "" {
		logAttrs[attr.APIKeyIDKey] = authCtx.APIKeyID
	}
	if authCtx.Email != nil && *authCtx.Email != "" {
		logAttrs[attr.UserEmailKey] = *authCtx.Email
	}

	params := tm.LogParams{
		Timestamp: end,
		ToolInfo: tm.ToolInfo{
			ID:             "",
			URN:            toolURN,
			Name:           toolName,
			ProjectID:      authCtx.ProjectID.String(),
			DeploymentID:   "",
			OrganizationID: authCtx.ActiveOrganizationID,
			FunctionID:     nil,
		},
		Attributes: logAttrs,
	}

	// Fire-and-forget so ClickHouse latency does not show up in the
	// user-observable tool-call tail. WithoutCancel preserves trace
	// context but detaches from request cancellation, and the logger
	// itself runs the insert against its own shutdown context.
	go i.telemLogger.Log(context.WithoutCancel(ctx), params)

	return nil
}

// upstreamStatusCode returns the upstream HTTP status code for a tools/call
// response. JSON-RPC errors map to 500 since they signal upstream-side
// failure even when the HTTP layer succeeded; tool-level errors (Result
// with IsError=true) keep the upstream HTTP status — failure is signaled by
// the response writer's status code, not the tool's inner error flag.
func upstreamStatusCode(call *proxy.ToolsCallResponse) int {
	if call.RemoteMessage != nil && call.RemoteMessage.RemoteHTTPResponse != nil {
		code := call.RemoteMessage.RemoteHTTPResponse.StatusCode
		if call.Error != nil && code < 400 {
			return 500
		}
		return code
	}
	if call.Error != nil {
		return 500
	}
	return 200
}
