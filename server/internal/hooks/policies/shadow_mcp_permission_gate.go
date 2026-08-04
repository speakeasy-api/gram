package policies

import (
	"context"

	"github.com/speakeasy-api/agenthooks"
)

// ShadowMCPPermissionGate builds the permission.request sibling of
// ShadowMCPToolPreGate: the same shadow-MCP evaluation over a pre-approval
// permission request.
func ShadowMCPPermissionGate(evaluate ShadowMCPEvaluateFunc) func(context.Context, *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
	return func(ctx context.Context, ev *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
		return shadowMCPGate(ctx, evaluate, &ev.Event, ev.Tool)
	}
}
