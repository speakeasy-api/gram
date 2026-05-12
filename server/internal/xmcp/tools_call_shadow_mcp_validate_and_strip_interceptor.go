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
// interceptor scoped to a single Remote MCP Server. serverID is unused
// for validation today (the echoed UUID is resolved against the project
// directly) but is captured for parity with the paired inject
// interceptor and to support a future cross-check between the echoed
// scope and the routed server.
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

	// Decode the arguments as a JSON object so the validator can read
	// the echoed scope property. A non-object body fails validation via
	// the same path as a missing property — the validator returns a
	// stable "missing required property" detail string.
	var argsMap map[string]any
	if len(call.Params.Arguments) > 0 {
		if err := json.Unmarshal(call.Params.Arguments, &argsMap); err != nil {
			argsMap = nil
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
