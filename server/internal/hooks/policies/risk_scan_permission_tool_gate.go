package policies

import (
	"context"

	"github.com/speakeasy-api/agenthooks"
)

// RiskScanPermissionToolGate builds the policy that re-scans a pre-approval
// permission request as an MCP or plain tool request, after
// RiskScanPermissionGate already scanned it with the permission flavor. The
// duplicate scan is deliberate: the old inline evaluation's permission
// branch did not return on a clean scan — it fell through into the MCP/plain
// branch and scanned the same request again with byte-identical scanner
// arguments. Dropping the second scan would be a semantic change (observably
// so for any non-deterministic scanner), so it is preserved exactly.
func RiskScanPermissionToolGate(scans ToolScanner, links BlockPageLinker, warns WarnChallenger) func(context.Context, *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
	return func(ctx context.Context, ev *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
		return riskScanToolRequest(ctx, scans, links, warns, &ev.Event, ev.Tool)
	}
}
