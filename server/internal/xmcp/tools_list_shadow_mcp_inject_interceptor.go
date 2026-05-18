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

// ToolsListShadowMCPInjectInterceptor injects the shadow-MCP
// `x-gram-toolset-id` property into each tool's input schema on a
// `tools/list` response, fixed via "const" to the Remote MCP Server's
// UUID. Tool callers must echo the value back so the paired
// [ToolsCallShadowMCPValidateAndStripInterceptor] (and downstream
// hook-layer validators) can prove the call originated from a
// Gram-managed scope.
//
// Gated on [shadowmcp.Client.IsEnabledForProject] at intercept time: when
// the calling project has no enabled tool-identity risk policy, the
// interceptor is a no-op against the response. The lookup is Redis-cached
// (15 minute TTL) so the runtime cost on the hot path is a Redis GET when
// the policy is disabled.
//
// The injected schema is a structural mutation: tools whose
// re-marshaling fails are logged and pass through unchanged rather than
// failing the whole `tools/list`. If any tool was successfully mutated,
// [proxy.ToolsListResponse.SetTools] commits the updated slice; a
// failure on that call surfaces as a [*proxy.MutationError] which the
// proxy renders as a 5xx (not a JSON-RPC rejection envelope).
type ToolsListShadowMCPInjectInterceptor struct {
	shadowmcpClient *shadowmcp.Client
	serverID        string
	projectID       string
	logger          *slog.Logger
}

var _ proxy.ToolsListResponseInterceptor = (*ToolsListShadowMCPInjectInterceptor)(nil)

// NewToolsListShadowMCPInjectInterceptor constructs an interceptor
// scoped to a single Remote MCP Server. The serverID is what gets
// injected as the `x-gram-toolset-id` const; projectID is the gating
// scope for [shadowmcp.Client.IsEnabledForProject].
func NewToolsListShadowMCPInjectInterceptor(shadowmcpClient *shadowmcp.Client, serverID, projectID string, logger *slog.Logger) *ToolsListShadowMCPInjectInterceptor {
	return &ToolsListShadowMCPInjectInterceptor{
		shadowmcpClient: shadowmcpClient,
		serverID:        serverID,
		projectID:       projectID,
		logger:          logger,
	}
}

// Name implements [proxy.ToolsListResponseInterceptor].
func (i *ToolsListShadowMCPInjectInterceptor) Name() string {
	return "tools-list-shadow-mcp-inject"
}

// InterceptToolsListResponse implements [proxy.ToolsListResponseInterceptor].
// When the project has shadow-MCP enabled, it injects the
// `x-gram-toolset-id` const into each tool's input schema. Tools whose
// schema fails to marshal are passed through unchanged.
func (i *ToolsListShadowMCPInjectInterceptor) InterceptToolsListResponse(ctx context.Context, list *proxy.ToolsListResponse) error {
	if list == nil || list.Result == nil || len(list.Result.Tools) == 0 {
		return nil
	}

	projectUUID, err := uuid.Parse(i.projectID)
	if err != nil {
		i.logger.WarnContext(ctx, "invalid project id; skipping shadow_mcp schema injection",
			attr.SlogError(err),
			attr.SlogComponent("xmcp"))
		return nil
	}

	if !i.shadowmcpClient.IsEnabledForProject(ctx, projectUUID) {
		return nil
	}

	tools := list.Result.Tools
	mutated := false
	for _, t := range tools {
		// The SDK types InputSchema as `any` since upstream MCP servers
		// can produce any JSON-marshalable shape; marshal it back to
		// bytes so InjectToolsetIDConstant (which operates on
		// json.RawMessage) can parse, mutate, and re-emit. The
		// per-tool marshal cost is bounded by the tool count.
		raw, err := json.Marshal(t.InputSchema)
		if err != nil {
			i.logger.WarnContext(ctx, "failed to encode tool input schema for shadow_mcp injection; skipping",
				attr.SlogError(err),
				attr.SlogToolName(t.Name),
				attr.SlogComponent("xmcp"))
			continue
		}
		injected, err := shadowmcp.InjectToolsetIDConstant(raw, i.serverID)
		if err != nil {
			i.logger.WarnContext(ctx, "failed to inject toolset id constant into tool input schema; skipping",
				attr.SlogError(err),
				attr.SlogToolName(t.Name),
				attr.SlogComponent("xmcp"))
			continue
		}
		// Assign the mutated bytes back so downstream marshal of the
		// response emits the injected schema verbatim.
		t.InputSchema = injected
		mutated = true
	}

	// Skip the setter (and the dirty-flag flip + chain-end re-marshal
	// it triggers) when no tool was successfully mutated — every
	// schema either failed to encode or was relayed unchanged.
	if !mutated {
		return nil
	}
	if err := list.SetTools(tools); err != nil {
		return fmt.Errorf("commit injected tools/list result: %w", err)
	}
	return nil
}
