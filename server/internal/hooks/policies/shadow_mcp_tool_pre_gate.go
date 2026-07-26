package policies

import (
	"context"

	"github.com/speakeasy-api/agenthooks"
)

// ShadowMCPEvaluateFunc runs the shadow-MCP enforcement primitive: the
// blocking shadow_mcp policy lookup, the Gram-hosted check, the bypass-grant
// check, and the access-request link plus block row minting. Empty reasons
// mean the call is not denied. The hooks Enforcer's EvaluateShadowMCP has
// this shape.
type ShadowMCPEvaluateFunc func(ctx context.Context, req *Request, actor Actor, toolName string, toolInput any) (auditReason, userReason string)

// ShadowMCPToolPreGate builds the policy that denies MCP tool calls
// targeting non-Gram-hosted servers under a blocking shadow_mcp policy,
// attaching the bypass-request link. The shared gate guards itself with the
// ingest path's own MCP predicate stamped on the event (mcpToolRequest) —
// not the library's tool matcher, which parses Gemini-style mcp_server_tool
// names and server-only mcp__ prefixes the ingest path has never treated as
// MCP — so the gate fires for exactly the calls the inline evaluation gated
// and stays neutral (no policy lookup, no side effects) for everything else.
func ShadowMCPToolPreGate(evaluate ShadowMCPEvaluateFunc) func(context.Context, *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
	return func(ctx context.Context, ev *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
		return shadowMCPGate(ctx, evaluate, &ev.Event, ev.Tool)
	}
}

// shadowMCPGate is the shared evaluation behind ShadowMCPToolPreGate and
// ShadowMCPPermissionGate.
func shadowMCPGate(ctx context.Context, evaluate ShadowMCPEvaluateFunc, env *agenthooks.Event, tool agenthooks.ToolCall) (agenthooks.ToolPreDecision, error) {
	req := RequestFromContext(ctx)
	if req == nil || !mcpToolRequest(env) {
		return agenthooks.NoDecision(), nil
	}
	auditReason, userReason := evaluate(ctx, req, ActorFromContext(ctx), tool.Name, toolInputOf(tool))
	if auditReason == "" {
		return agenthooks.NoDecision(), nil
	}
	return agenthooks.Deny(auditReason).WithSystemMessage(userReason), nil
}
