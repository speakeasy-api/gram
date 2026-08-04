package policies

import (
	"context"

	"github.com/speakeasy-api/agenthooks"
)

// RiskScanPermissionGate builds the policy that scans a pre-approval
// permission request. It is the permission.request sibling of
// RiskScanToolPreGate, with the permission-flavored scan (the Enforcer's
// ScanPermissionRequest, same ToolScanFunc shape) and its deny wording.
func RiskScanPermissionGate(scanPermission ToolScanFunc, appendBlockURL BlockPageLinkFunc, warnAcknowledged WarnAckFunc, warnDenyReason WarnDenyFunc) func(context.Context, *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
	return func(ctx context.Context, ev *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
		req := RequestFromContext(ctx)
		if req == nil {
			return agenthooks.NoDecision(), nil
		}
		actor := ActorFromContext(ctx)
		scan := scanPermission(ctx, req, actor, ev.Tool.Name, toolInputOf(ev.Tool), ev.Time)
		return riskScanToolDecision(ctx, appendBlockURL, warnAcknowledged, warnDenyReason, req, actor, scan, ev.Tool.Name, "permission request", ev.Time)
	}
}
