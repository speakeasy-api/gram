package xmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// ToolsCallShadowMCPValidateAndStripInterceptor is the request-side pair
// of [ToolsListShadowMCPInjectInterceptor]. It validates the
// `x-gram-toolset-id` const the caller echoed back from the tool's input
// schema, then strips the property from the arguments before the request
// is forwarded upstream — the remote MCP server should see its declared
// argument shape, not Gram's envelope.
//
// Validation calls [shadowmcp.Client.ValidateRemoteMCPServerCall], which
// confirms the echoed UUID resolves to a remote_mcp_server in the
// calling project. A failure surfaces as a [*proxy.RejectError] with
// [proxy.RejectCodeServerError] and the validator's detail string as
// the user-visible message; the upstream tool call is never issued.
//
// Strip-only mode (no validation) is intentionally not exposed: the
// downstream client-side hook layer relies on echoed UUIDs being
// authentic, so the proxy must validate at this layer too rather than
// trusting whatever the caller sent.
//
// Gated on [shadowmcp.Client.IsEnabledForProject] at intercept time — a
// no-op when the project has no enabled tool-identity risk policy.
type ToolsCallShadowMCPValidateAndStripInterceptor struct {
	shadowmcpClient *shadowmcp.Client
	serverID        string
	projectID       string
	logger          *slog.Logger
}

var _ proxy.ToolsCallRequestInterceptor = (*ToolsCallShadowMCPValidateAndStripInterceptor)(nil)

// NewToolsCallShadowMCPValidateAndStripInterceptor constructs an
// interceptor scoped to a single Remote MCP Server. serverID is the
// routed server's UUID and is cross-checked against the caller's
// echoed scope after the validator confirms the echoed UUID resolves
// in the project — guarding against sibling-server echoes within the
// same project, where a caller could otherwise satisfy validation by
// echoing any project-scoped server's UUID rather than the one the
// route actually targets.
func NewToolsCallShadowMCPValidateAndStripInterceptor(shadowmcpClient *shadowmcp.Client, serverID, projectID string, logger *slog.Logger) *ToolsCallShadowMCPValidateAndStripInterceptor {
	return &ToolsCallShadowMCPValidateAndStripInterceptor{
		shadowmcpClient: shadowmcpClient,
		serverID:        serverID,
		projectID:       projectID,
		logger:          logger,
	}
}

// Name implements [proxy.ToolsCallRequestInterceptor].
func (i *ToolsCallShadowMCPValidateAndStripInterceptor) Name() string {
	return "tools-call-shadow-mcp-validate-and-strip"
}

// InterceptToolsCallRequest implements [proxy.ToolsCallRequestInterceptor].
// When the project has shadow-MCP enabled, it validates the echoed
// `x-gram-toolset-id` against the calling project and strips the
// property from the arguments before the proxy forwards them upstream.
func (i *ToolsCallShadowMCPValidateAndStripInterceptor) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	if call == nil || call.Params == nil {
		return nil
	}

	projectUUID, err := uuid.Parse(i.projectID)
	if err != nil {
		i.logger.WarnContext(ctx, "invalid project id; skipping shadow_mcp validation",
			attr.SlogError(err),
			attr.SlogComponent("xmcp"))
		return nil
	}

	if !i.shadowmcpClient.IsEnabledForProject(ctx, projectUUID) {
		return nil
	}

	// Decode the arguments as a JSON object. Non-object payloads
	// (arrays, scalars, malformed JSON) get a distinct rejection
	// message rather than being passed through to the validator as nil
	// and surfacing the misleading "missing required property" detail
	// — the caller's real problem is a malformed body, not just an
	// absent field.
	var argsMap map[string]any
	if len(call.Params.Arguments) > 0 {
		if err := json.Unmarshal(call.Params.Arguments, &argsMap); err != nil {
			i.logger.DebugContext(ctx, "tools/call arguments are not a JSON object; shadow-MCP rejecting",
				attr.SlogError(err),
				attr.SlogComponent("xmcp"))
			return &proxy.RejectError{
				Code:    proxy.RejectCodeServerError,
				Message: "shadow-mcp: tools/call arguments must be a JSON object",
				Data:    nil,
			}
		}
	}

	detail, denied := i.shadowmcpClient.ValidateRemoteMCPServerCall(ctx, argsMap, call.Params.Name, i.projectID)
	if denied {
		return &proxy.RejectError{
			Code:    proxy.RejectCodeServerError,
			Message: fmt.Sprintf("shadow-mcp: %s", detail),
			Data:    nil,
		}
	}

	// Defense in depth: after the validator confirms the echoed UUID
	// resolves to a real remote_mcp_server in the project, ensure the
	// caller didn't echo a sibling server's UUID within the same
	// project. The route's serverID is the source of truth for which
	// server the call targets, so an echo that matches the project
	// scope but not the route shape is a forged/replayed provenance
	// signal and must not satisfy validation.
	echoedRaw, _ := argsMap[shadowmcp.XGramToolsetIDField].(string)
	if echoedRaw != i.serverID {
		return &proxy.RejectError{
			Code:    proxy.RejectCodeServerError,
			Message: fmt.Sprintf("shadow-mcp: echoed %s does not match the routed server", shadowmcp.XGramToolsetIDField),
			Data:    nil,
		}
	}

	// Strip the proxy envelope so the upstream tool sees its declared
	// argument shape. Strip is a no-op when the property is absent; the
	// validator already confirmed it is present and well-formed.
	stripped, err := shadowmcp.StripToolsetIDProperty(call.Params.Arguments)
	if err != nil {
		return fmt.Errorf("strip x-gram-toolset-id from tool arguments: %w", err)
	}
	if err := call.SetArguments(stripped); err != nil {
		return fmt.Errorf("commit scrubbed tools/call arguments: %w", err)
	}
	return nil
}
