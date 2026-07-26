package policies

import (
	"context"
	"time"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/server/internal/risk"
)

// PermissionScanner runs the permission-flavored enforcement risk scan over
// a pre-approval permission request.
type PermissionScanner interface {
	ScanPermissionRequest(ctx context.Context, req *Request, actor Actor, at time.Time) *risk.ScanResult
}

// RiskScanPermissionGate builds the policy that scans a pre-approval
// permission request. It is the permission.request sibling of
// RiskScanToolPreGate, with the permission-request scan and deny wording.
func RiskScanPermissionGate(scans PermissionScanner, links BlockPageLinker, warns WarnChallenger) func(context.Context, *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
	return func(ctx context.Context, ev *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
		req := RequestFromContext(ctx)
		if req == nil {
			return agenthooks.NoDecision(), nil
		}
		actor := ActorFromContext(ctx)
		scan := scans.ScanPermissionRequest(ctx, req, actor, ev.Time)
		return riskScanToolDecision(ctx, links, warns, req, actor, scan, "permission request", ev.Time)
	}
}
