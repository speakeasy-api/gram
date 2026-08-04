package gram

import (
	"log/slog"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	hookpolicies "github.com/speakeasy-api/gram/server/internal/hooks/policies"
)

// newHookPolicyRunner builds the ingest decision pipeline. The registration
// block below IS the run order — this is the single place the pipeline is
// defined — and the order replicates the old inline evaluateCanonicalHook
// exactly:
//
//   - the ActorResolution middleware makes the resolved actor available to
//     every gating stage.
//   - prompt.submitted: the spend gate (budget circuit), then the risk scan
//     (CEL policies).
//   - tool.requested: the spend gate, then the risk scan (MCP- or
//     tool-flavored, matching the request shape), then the shadow-MCP gate
//     (deny + bypass-request link for non-Gram MCP servers). The spend gate
//     and risk scan ran in that order inline too, so a spend deny wins over
//     a risk block, and a risk block wins over a shadow deny.
//   - tool.requested with permission_type (dispatched as permission.request):
//     the spend gate, the permission-flavored risk scan, then the same
//     MCP-or-tool risk scan the inline code fell through to (a deliberate
//     duplicate scan of the same request — see RiskScanPermissionToolGate),
//     then the shadow gate.
//
// The deps handed to the constructors are the Enforcer's method values: a
// method value binds the receiver pointer and reads its fields at call time,
// so swapping a field on the shared Enforcer after construction — as tests
// do with riskScanner and siteURL — is picked up on the next event. The Walk
// snapshot test next to this file pins the registration order.
//
// The hooks test setup mirrors this block (newTestPolicyRunner in
// internal/hooks/setup_test.go) because tests cannot import this package;
// keep the two in sync.
func newHookPolicyRunner(logger *slog.Logger, enforcer *hooks.Enforcer) *agenthooks.Runner {
	r := agenthooks.New(agenthooks.WithLogger(logger.With(attr.SlogComponent("hooks"))))

	r.Use(hookpolicies.ActorResolution)

	r.OnPromptSubmitted(
		hookpolicies.SpendGatePrompt(enforcer.CheckSpend),
		hookpolicies.RiskScanPromptGate(enforcer.ScanPrompt, enforcer.WarnAcknowledged, enforcer.WarnDenyReason),
	)
	r.OnToolPre(
		hookpolicies.SpendGateToolPre(enforcer.CheckSpend, enforcer.AppendBlockPageURL),
		hookpolicies.RiskScanToolPreGate(enforcer.ScanToolRequest, enforcer.ScanMCPToolRequest, enforcer.AppendBlockPageURL, enforcer.WarnAcknowledged, enforcer.WarnDenyReason),
		hookpolicies.ShadowMCPToolPreGate(enforcer.EvaluateShadowMCP),
	)
	r.OnPermission(
		hookpolicies.SpendGatePermission(enforcer.CheckSpend, enforcer.AppendBlockPageURL),
		hookpolicies.RiskScanPermissionGate(enforcer.ScanPermissionRequest, enforcer.AppendBlockPageURL, enforcer.WarnAcknowledged, enforcer.WarnDenyReason),
		hookpolicies.RiskScanPermissionToolGate(enforcer.ScanToolRequest, enforcer.ScanMCPToolRequest, enforcer.AppendBlockPageURL, enforcer.WarnAcknowledged, enforcer.WarnDenyReason),
		hookpolicies.ShadowMCPPermissionGate(enforcer.EvaluateShadowMCP),
	)

	return r
}
