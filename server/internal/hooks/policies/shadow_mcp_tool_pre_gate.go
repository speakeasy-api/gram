package policies

import (
	"context"

	"github.com/speakeasy-api/agenthooks"
)

// ShadowMCPEvaluator runs the shadow-MCP enforcement primitive: the blocking
// shadow_mcp policy lookup, the Gram-hosted check, the bypass-grant check,
// and the access-request link plus block row minting. Empty reasons mean the
// call is not denied.
type ShadowMCPEvaluator interface {
	EvaluateShadowMCP(ctx context.Context, req *Request, actor Actor, toolName string, toolInput any) (auditReason, userReason string)
}

// ShadowMCPToolPreGate builds the policy that denies MCP tool calls
// targeting non-Gram-hosted servers under a blocking shadow_mcp policy,
// attaching the bypass-request link. The shared gate guards itself with the
// ingest path's own MCP predicate (Request.IsMCPToolRequest) — not the
// library's tool matcher, which parses Gemini-style mcp_server_tool names
// and server-only mcp__ prefixes the ingest path has never treated as MCP —
// so the gate fires for exactly the calls the inline evaluation gated and
// stays neutral (no policy lookup, no side effects) for everything else.
func ShadowMCPToolPreGate(shadow ShadowMCPEvaluator) func(context.Context, *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
	return func(ctx context.Context, _ *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
		return shadowMCPGate(ctx, shadow)
	}
}

// shadowMCPGate is the shared evaluation behind ShadowMCPToolPreGate and
// ShadowMCPPermissionGate.
func shadowMCPGate(ctx context.Context, shadow ShadowMCPEvaluator) (agenthooks.ToolPreDecision, error) {
	req := RequestFromContext(ctx)
	if req == nil || !req.IsMCPToolRequest {
		return agenthooks.NoDecision(), nil
	}
	auditReason, userReason := shadow.EvaluateShadowMCP(ctx, req, ActorFromContext(ctx), req.ToolName, req.ToolInput)
	if auditReason == "" {
		return agenthooks.NoDecision(), nil
	}
	return agenthooks.Deny(auditReason).WithSystemMessage(userReason), nil
}
