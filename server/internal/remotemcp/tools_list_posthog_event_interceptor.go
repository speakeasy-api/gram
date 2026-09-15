package remotemcp

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

// eventMCPServerToolsList is the PostHog event name emitted for every observed
// `tools/list` request. Renamed from `/mcp`'s `mcp_server_count` because the
// property schema differs — see the type doc below for context. AGE-1902
// tracks unifying the two runtimes onto this single event name.
const eventMCPServerToolsList = "mcp_server_tools_list"

// ToolsListPostHogEventInterceptor emits the [eventMCPServerToolsList] PostHog
// event for every JSON-RPC `tools/list` request observed by `/x/mcp`. It is a
// [proxy.ToolsListRequestInterceptor]: emitting on the request side mirrors
// `/mcp`'s placement of the equivalent event before any per-tool filtering or
// upstream call, so the event records "tools/list was attempted on this
// server" regardless of upstream success.
//
// The event is renamed from `/mcp`'s `mcp_server_count` because the property
// schema differs — the toolset-shaped fields (`toolset_id`, `toolset_slug`,
// etc.) are replaced with `remote_mcp_server_id` since `/x/mcp` proxies a
// Remote MCP Server rather than wrapping a Gram-managed toolset. AGE-1902
// tracks unifying the two runtimes onto this single event name.
type ToolsListPostHogEventInterceptor struct {
	posthog  *posthog.Posthog
	identity proxy.ServerIdentity
	logger   *slog.Logger
}

var _ proxy.ToolsListRequestInterceptor = (*ToolsListPostHogEventInterceptor)(nil)

// NewToolsListPostHogEventInterceptor constructs an interceptor scoped to a
// single Remote MCP Server. Callers build a fresh instance per request so the
// `remote_mcp_server_id` and `mcp_server_id` properties are captured without
// re-parsing routing state.
func NewToolsListPostHogEventInterceptor(posthogClient *posthog.Posthog, identity proxy.ServerIdentity, logger *slog.Logger) *ToolsListPostHogEventInterceptor {
	return &ToolsListPostHogEventInterceptor{
		posthog:  posthogClient,
		identity: identity,
		logger:   logger,
	}
}

// Name implements [proxy.ToolsListRequestInterceptor].
func (i *ToolsListPostHogEventInterceptor) Name() string {
	return "tools-list-posthog-event"
}

// InterceptToolsListRequest implements [proxy.ToolsListRequestInterceptor].
// Always returns nil — analytics emission is best-effort and must not block
// the request from reaching the remote server.
func (i *ToolsListPostHogEventInterceptor) InterceptToolsListRequest(ctx context.Context, list *proxy.ToolsListRequest) error {
	if i.posthog == nil || list == nil || list.UserRequest == nil {
		return nil
	}

	requestContext, _ := contextvalues.GetRequestContext(ctx)
	if requestContext == nil {
		return nil
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	authenticated := authCtx != nil && authCtx.ProjectID != nil

	var projectID string
	if authenticated {
		projectID = authCtx.ProjectID.String()
	}

	sessionID := ""
	if list.UserRequest.UserHTTPRequest != nil {
		sessionID = list.UserRequest.UserHTTPRequest.Header.Get("Mcp-Session-Id")
	}
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	// Client identity and protocol version come from per-request metadata
	// only: the header, and the `_meta` the 2026-07-28 revision attaches to
	// every request. This path has no initialize-time store to fall back to
	// (the proxy relays the handshake without recording it), so for
	// handshake-era clients these properties stay empty by design.
	var reqMeta mcprequests.SanitizedMeta
	if msgs := list.UserRequest.JSONRPCMessages; len(msgs) == 1 {
		if rpcReq, ok := msgs[0].(*jsonrpc.Request); ok {
			reqMeta = mcprequests.ParseMeta(rpcReq.Params)
		}
	}
	var headerVersion string
	if list.UserRequest.UserHTTPRequest != nil {
		headerVersion = list.UserRequest.UserHTTPRequest.Header.Get(mcpversions.HTTPHeader)
	}
	protocolVersion := reqMeta.DeclaredProtocolVersion(headerVersion)
	var clientName, clientVersion string
	if reqMeta.ClientInfo != nil {
		clientName = reqMeta.ClientInfo.Name
		clientVersion = reqMeta.ClientInfo.Version
	}

	if err := i.posthog.CaptureEvent(ctx, eventMCPServerToolsList, sessionID, map[string]any{
		"project_id":             projectID,
		"authenticated":          authenticated,
		"remote_mcp_server_id":   i.identity.RemoteMCPServerID,
		"tunneled_mcp_server_id": i.identity.TunneledMCPServerID,
		"mcp_server_id":          i.identity.McpServerID,
		"mcp_domain":             requestContext.Host,
		"mcp_url":                requestContext.Host + requestContext.ReqURL,
		"disable_notification":   true,
		"mcp_session_id":         sessionID,
		"protocol_version":       protocolVersion,
		"client_name":            conv.PtrEmpty(clientName),
		"client_version":         conv.PtrEmpty(clientVersion),
		"capabilities":           reqMeta.CapabilityKeys,
	}); err != nil {
		i.logger.ErrorContext(ctx, "failed to capture "+eventMCPServerToolsList+" event", attr.SlogError(err))
	}

	return nil
}
