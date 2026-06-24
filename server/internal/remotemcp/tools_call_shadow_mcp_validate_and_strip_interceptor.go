package remotemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// ToolsCallShadowMCPValidateAndStripInterceptor validates remote MCP
// tool calls for projects with Shadow MCP enabled. Remote MCP routes
// already identify the target remote_mcp_server, so the interceptor uses
// that route identity when callers omit Gram's internal
// `x-gram-toolset-id` property.
//
// Stale clients may still echo `x-gram-toolset-id` from a previously
// mutated tools/list schema. When present, the value must be a non-empty
// string matching the routed server, and it is stripped before the
// request is forwarded upstream — the remote MCP server should see its
// declared argument shape, not Gram's envelope.
//
// Validation calls [shadowmcp.Client.ValidateRemoteMCPServerCall], which
// confirms the routed UUID resolves to a remote_mcp_server in the
// calling project. A failure surfaces as a [*proxy.RejectError] with
// [proxy.RejectCodeServerError] and the validator's detail string as
// the user-visible message; the upstream tool call is never issued.
//
// Strip-only mode (no validation) is intentionally not exposed: stale
// echoed UUIDs still need to be authentic, so the proxy must validate at
// this layer too rather than trusting whatever the caller sent.
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
// routed server's UUID used as the validation provenance. If a caller
// still echoes an `x-gram-toolset-id` value, it is cross-checked against
// serverID to guard against sibling-server echoes within the same
// project.
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
// When the project has shadow-MCP enabled, it validates the routed
// remote server against the calling project and strips any stale
// `x-gram-toolset-id` property from the arguments before the proxy
// forwards them upstream.
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
	if argsMap == nil {
		argsMap = map[string]any{}
	}

	// Remote MCP routes already identify the target remote_mcp_server. Use
	// that route identity as the provenance signal when a client omits the
	// internal toolset field. This keeps Shadow MCP compatible with clients
	// that never saw Gram-specific schema properties while still rejecting
	// forged stale echoes that point at a sibling server.
	echoedValue, hasEchoed := argsMap[shadowmcp.XGramToolsetIDField]
	echoedRaw, ok := echoedValue.(string)
	if hasEchoed && !ok {
		return &proxy.RejectError{
			Code:    proxy.RejectCodeServerError,
			Message: fmt.Sprintf("shadow-mcp: invalid %s value", shadowmcp.XGramToolsetIDField),
			Data:    nil,
		}
	}
	if hasEchoed && echoedRaw == "" {
		return &proxy.RejectError{
			Code:    proxy.RejectCodeServerError,
			Message: fmt.Sprintf("shadow-mcp: invalid %s value", shadowmcp.XGramToolsetIDField),
			Data:    nil,
		}
	}
	if hasEchoed && echoedRaw != "" && echoedRaw != i.serverID {
		return &proxy.RejectError{
			Code:    proxy.RejectCodeServerError,
			Message: fmt.Sprintf("shadow-mcp: echoed %s does not match the routed server", shadowmcp.XGramToolsetIDField),
			Data:    nil,
		}
	}
	validationInput := argsMap
	if !hasEchoed {
		validationInput = make(map[string]any, len(argsMap)+1)
		maps.Copy(validationInput, argsMap)
		validationInput[shadowmcp.XGramToolsetIDField] = i.serverID
	}

	detail, denied := i.shadowmcpClient.ValidateRemoteMCPServerCall(ctx, validationInput, call.Params.Name, i.projectID)
	if denied {
		return &proxy.RejectError{
			Code:    proxy.RejectCodeServerError,
			Message: fmt.Sprintf("shadow-mcp: %s", detail),
			Data:    nil,
		}
	}

	// Strip the proxy envelope so the upstream tool sees its declared
	// argument shape. Strip is a no-op when the property is absent.
	stripped, err := shadowmcp.StripToolsetIDProperty(call.Params.Arguments)
	if err != nil {
		return fmt.Errorf("strip x-gram-toolset-id from tool arguments: %w", err)
	}
	if err := call.SetArguments(stripped); err != nil {
		return fmt.Errorf("commit scrubbed tools/call arguments: %w", err)
	}
	return nil
}
