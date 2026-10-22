package remotemcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
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
// One instance implements [proxy.ToolsCallRequestInterceptor],
// [proxy.ToolsCallResponseInterceptor] and [proxy.ToolsCallFailureInterceptor].
// The proxy constructs a fresh interceptor per HTTP request inside
// [ProxyManager.BuildTarget], and [proxy.Proxy.Post] fires the request chain
// and then either the response or the failure chain sequentially on a single
// goroutine for at most one tools/call per request — so a single nilable
// start timestamp is enough state to compute duration, no map or mutex
// required. If a future refactor makes [proxy.Proxy] (or this interceptor)
// long-lived across requests, this needs to revert to per-call keying.
//
// Emission is fire-and-forget on a goroutine bound to
// [context.WithoutCancel] so ClickHouse latency never appears in the user's
// tool-call tail latency.
type ToolsCallClickHouseLogInterceptor struct {
	telemLogger *tm.Logger
	identity    proxy.ServerIdentity
	logger      *slog.Logger

	// start is set by the request interceptor and consumed by the
	// response or failure interceptor. Nil means the request side never
	// fired, in which case the row carries the duration-missing sentinel.
	start *time.Time
}

var (
	_ proxy.ToolsCallRequestInterceptor  = (*ToolsCallClickHouseLogInterceptor)(nil)
	_ proxy.ToolsCallResponseInterceptor = (*ToolsCallClickHouseLogInterceptor)(nil)
	_ proxy.ToolsCallFailureInterceptor  = (*ToolsCallClickHouseLogInterceptor)(nil)
)

// NewToolsCallClickHouseLogInterceptor constructs an interceptor bound to the
// given telemetry logger and server correlation ids. Construct one per request
// (or per buildProxy call) so the id attributes are closed over without
// re-deriving them per emission.
func NewToolsCallClickHouseLogInterceptor(telemLogger *tm.Logger, identity proxy.ServerIdentity, logger *slog.Logger) *ToolsCallClickHouseLogInterceptor {
	return &ToolsCallClickHouseLogInterceptor{
		telemLogger: telemLogger,
		identity:    identity,
		logger:      logger,
		start:       nil,
	}
}

// Name implements [proxy.ToolsCallRequestInterceptor],
// [proxy.ToolsCallResponseInterceptor] and [proxy.ToolsCallFailureInterceptor].
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
// stashed start time (or a duration-missing sentinel), and the server
// context, and emits it asynchronously to ClickHouse. Always returns nil.
func (i *ToolsCallClickHouseLogInterceptor) InterceptToolsCallResponse(ctx context.Context, call *proxy.ToolsCallResponse) error {
	if call == nil || call.Request == nil || call.Request.Params == nil {
		return nil
	}

	var outputBytes int64
	var outputContent []byte
	if call.RemoteMessage != nil {
		if rpcResp, ok := call.RemoteMessage.Message.(*jsonrpc.Response); ok && rpcResp != nil {
			outputBytes = int64(len(rpcResp.Result))
			outputContent = rpcResp.Result
		}
	}
	// Failed calls record the marshaled JSON-RPC error as the output payload.
	// This deliberately changes outputBytes semantics for errors (previously 0):
	// the recorded size describes the stored error document, not wire bytes, so
	// error rows carry inspectable content in the tool-I/O views.
	if len(outputContent) == 0 && call.Error != nil {
		if encodedError, err := json.Marshal(call.Error); err == nil {
			outputContent = encodedError
			outputBytes = int64(len(encodedError))
		}
	}

	i.emit(ctx, call.Request, upstreamStatusCode(call), call.Result != nil && call.Result.IsError, outputBytes, outputContent)
	return nil
}

// InterceptToolsCallFailure emits the row for a forwarded call whose reply
// never arrived, so an unreachable or failing upstream reads as a failed call
// rather than leaving no trace. There is no reply to record as output.
func (i *ToolsCallClickHouseLogInterceptor) InterceptToolsCallFailure(ctx context.Context, failure *proxy.ToolsCallFailure) {
	if failure == nil || failure.Request == nil || failure.Request.Params == nil {
		return
	}
	i.emit(ctx, failure.Request, failedCallStatusCode(ctx, failure), false, 0, nil)
}

// emit builds the telemetry_logs row for one tools/call and writes it
// asynchronously to ClickHouse.
func (i *ToolsCallClickHouseLogInterceptor) emit(ctx context.Context, request *proxy.ToolsCallRequest, statusCode int, resultIsError bool, outputBytes int64, outputContent []byte) {
	serverCtx, ok := GetServerContext(ctx)
	if !ok || serverCtx.OrganizationID == "" || serverCtx.ProjectID == uuid.Nil {
		i.logger.WarnContext(ctx, "skipping tools/call clickhouse log: missing server context",
			attr.SlogComponent("xmcp"))
		return
	}

	end := time.Now()
	var durationSec float64
	durationMissing := i.start == nil
	if i.start != nil {
		durationSec = end.Sub(*i.start).Seconds()
	}

	toolName := request.Params.Name
	requestBytes := int64(len(request.Params.Arguments))

	if resultIsError {
		// Warn, not error: an isError result is the upstream tool's verdict, not a
		// Speakeasy fault. The body stays out: it is upstream-controlled content.
		fields := append([]slog.Attr{
			attr.SlogComponent("xmcp"),
			attr.SlogToolName(toolName),
			attr.SlogOrganizationID(serverCtx.OrganizationID),
			attr.SlogProjectID(serverCtx.ProjectID.String()),
		}, i.identity.SlogAttrs()...)
		i.logger.LogAttrs(ctx, slog.LevelWarn, "remote MCP tool returned an error result", fields...)
	}

	// Synthetic URN: the dashboard's tool-call queries filter on
	// startsWith(gram_urn, 'tools:'), so the prefix is required. Use the
	// backend UUID (stable) rather than a slug (mutable) in the source
	// segment so historical URNs remain queryable across renames.
	// urn.ParseTool will reject names with characters outside SlugPatternRE
	// (uppercase, dots, etc.), which means the materialized tool_source
	// column may be empty for those tool names — acceptable; gram_urn
	// (used for grouping) and tool_name (separate materialized column from
	// gram.tool.name attribute) are still populated correctly.
	toolURN := "tools:" + i.identity.ToolURNKind() + ":" + i.identity.SourceID() + ":" + toolName

	logAttrs := tm.HTTPLogAttributes{
		attr.EventSourceKey: string(tm.EventSourceToolCall),
	}
	if i.identity.RemoteMCPServerID != "" {
		logAttrs[attr.RemoteMCPServerIDKey] = i.identity.RemoteMCPServerID
	}
	if i.identity.TunneledMCPServerID != "" {
		logAttrs[attr.TunneledMCPServerIDKey] = i.identity.TunneledMCPServerID
	}
	if i.identity.McpServerID != "" {
		logAttrs[attr.McpServerIDKey] = i.identity.McpServerID
	}
	if i.identity.MetaMCPServerID != "" {
		logAttrs[attr.MetaMcpServerIDKey] = i.identity.MetaMCPServerID
	}
	logAttrs.RecordDuration(durationSec)
	logAttrs.RecordStatusCode(statusCode)
	logAttrs[attr.OutcomeKey] = dialect.ToolCallOutcome(statusCode, resultIsError)
	logAttrs.RecordRequestBody(requestBytes)
	logAttrs.RecordResponseBody(outputBytes)
	logAttrs.RecordRequestBodyContent(request.Params.Arguments)
	logAttrs.RecordResponseBodyContent(outputContent)
	logAttrs.RecordTraceContext(ctx)
	ensureTraceContext(logAttrs)
	if durationMissing {
		logAttrs[DurationMissingKey] = true
	}
	userInfo := tm.UserInfoByID("")
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ActiveOrganizationID == serverCtx.OrganizationID {
		logAttrs.RecordAuthenticatedActor(ctx)
		userInfo = tm.UserInfoByID(authCtx.UserID)
		if authCtx.APIKeyID != "" {
			logAttrs[attr.APIKeyIDKey] = authCtx.APIKeyID
		}
		if authCtx.ExternalUserID != "" {
			logAttrs[attr.ExternalUserIDKey] = authCtx.ExternalUserID
		}
	}
	// The gateway resolved the caller before dialing this member; without it
	// proxied dispatches would be the one tool-call flavour with no client.
	if client, ok := contextvalues.GetMCPClientInfo(ctx); ok {
		logAttrs.RecordMCPClient(client.Name, client.Version)
	}

	params := tm.LogParams{
		Timestamp: end,
		ToolInfo: tm.ToolInfo{
			ID:             "",
			URN:            toolURN,
			Name:           toolName,
			ProjectID:      serverCtx.ProjectID.String(),
			DeploymentID:   "",
			OrganizationID: serverCtx.OrganizationID,
			FunctionID:     nil,
		},
		UserInfo:   userInfo,
		Attributes: logAttrs,
	}

	// Fire-and-forget so ClickHouse latency does not show up in the
	// user-observable tool-call tail. WithoutCancel preserves trace
	// context but detaches from request cancellation, and the logger
	// itself runs the insert against its own shutdown context.
	go i.telemLogger.Log(context.WithoutCancel(ctx), params)
}

// ensureTraceContext fills trace/span ids when this telemetry row has no
// active OTel span context. That is expected when tracing is disabled or when
// tests invoke the interceptor directly: the row is still valid, but
// trace_summaries_mv drops rows without trace_id and the tool-log dashboards
// read from that summary path. The generated ids are therefore a local
// ClickHouse grouping key, not distributed trace context.
//
// Hosted/toolset-backed MCP rows do the equivalent earlier in
// gateway.ToolProxy.Do by recording the active gateway.toolCall span onto the
// shared log attributes.
func ensureTraceContext(logAttrs tm.HTTPLogAttributes) {
	if _, ok := logAttrs[attr.TraceIDKey]; !ok {
		traceID := strings.ReplaceAll(uuid.NewString(), "-", "")
		logAttrs[attr.TraceIDKey] = traceID
	}
	if _, ok := logAttrs[attr.SpanIDKey]; !ok {
		traceID, _ := logAttrs[attr.TraceIDKey].(string)
		if len(traceID) >= 16 {
			logAttrs[attr.SpanIDKey] = traceID[:16]
			return
		}
		logAttrs[attr.SpanIDKey] = strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	}
}

// upstreamStatusCode returns the upstream HTTP status code for a tools/call
// response. JSON-RPC errors map to 500 since they signal upstream-side
// failure even when the HTTP layer succeeded; tool-level errors (Result
// with IsError=true) keep the upstream HTTP status — failure is signaled by
// the response writer's status code, not the tool's inner error flag, which
// is recorded separately as gram.outcome.
func upstreamStatusCode(call *proxy.ToolsCallResponse) int {
	failed := call.Error != nil
	if call.RemoteMessage != nil && call.RemoteMessage.RemoteHTTPResponse != nil {
		code := call.RemoteMessage.RemoteHTTPResponse.StatusCode
		if failed && code < 400 {
			return http.StatusInternalServerError
		}
		return code
	}
	if failed {
		return http.StatusInternalServerError
	}
	return http.StatusOK
}

// failedCallStatusCode returns the status code recorded for a call whose
// reply never arrived: the upstream's error status when it sent one, else
// the status of the error the proxy answered with, else 502 for an upstream
// that was unreachable or replied with something other than a tools/call
// result.
func failedCallStatusCode(ctx context.Context, failure *proxy.ToolsCallFailure) int {
	if failure.StatusCode >= 400 {
		return failure.StatusCode
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](failure.Err); ok {
		if code := shareable.HTTPStatus(ctx); code >= 400 {
			return code
		}
	}
	return http.StatusBadGateway
}
