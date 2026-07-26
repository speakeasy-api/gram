// Package policies is the router-backed ingest decision layer: one file per
// policy, each exporting a constructor that takes the enforcement primitives
// it needs and returns the agenthooks handler, plus the actor-resolution
// middleware and the NewRunner builder that registers them in run order.
//
// The stages are thin adapters over the enforcement primitives the hooks
// service implements (risk scans, shadow-MCP evaluation, block-page URL
// minting) — they translate the primitives' outcomes into agenthooks
// decisions and nothing else, so the enforcement behavior is exactly the
// inline evaluation Ingest ran before. The boundary that maps the winning
// decision back onto the wire response lives in the hooks service's
// evaluateCanonicalHook.
package policies

import (
	"log/slog"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// Deps bundles every enforcement primitive the registered policies call. The
// hooks service's enforcer implements it; tests substitute narrow fakes. The
// per-policy constructors take only the slices of this interface they use,
// so a policy's true dependencies stay visible at its definition.
type Deps interface {
	SpendChecker
	PromptScanner
	ToolScanner
	PermissionScanner
	BlockPageLinker
	ShadowMCPEvaluator
	WarnChallenger
}

// NewRunner builds the ingest decision pipeline. The registration block
// below IS the run order, and the order replicates the old inline
// evaluateCanonicalHook exactly:
//
//   - the ActorResolution middleware makes the resolved actor available to
//     every gating stage.
//   - the spend gate runs first on every gated kind: inline it ran before
//     any risk-policy evaluation, so an over-budget actor is denied before
//     any policy scan.
//   - prompt.submitted: the spend gate, then the risk scan (CEL policies).
//   - tool.requested: the spend gate, then the risk scan (MCP- or
//     tool-flavored, matching the request shape), then the shadow-MCP gate
//     (deny + bypass-request link for non-Gram MCP servers). The risk scan
//     ran first inline too, so a risk block wins over a shadow deny and the
//     risk_scanned metric dimension is set even when the shadow gate would
//     deny.
//   - tool.requested with permission_type (dispatched as permission.request):
//     the spend gate (with the permission framing), the permission-flavored
//     risk scan, then the same MCP-or-tool risk scan the inline code fell
//     through to (a deliberate duplicate scan of the same request — see
//     RiskScanPermissionToolGate), then the shadow gate.
//
// Stages read their dependencies through deps at call time, so swapping what
// deps resolves to — as tests do with the enforcer's fields — is picked up
// on the next event. Walk reports the constructors' reflected names; the
// snapshot test pins them.
func NewRunner(logger *slog.Logger, deps Deps) *agenthooks.Runner {
	r := agenthooks.New(agenthooks.WithLogger(logger.With(attr.SlogComponent("hooks"))))

	r.Use(ActorResolution)

	r.OnPromptSubmitted(
		SpendGatePrompt(deps),
		RiskScanPromptGate(deps, deps),
	)
	r.OnToolPre(
		SpendGateToolPre(deps, deps),
		RiskScanToolPreGate(deps, deps, deps),
		ShadowMCPToolPreGate(deps),
	)
	r.OnPermission(
		SpendGatePermission(deps, deps),
		RiskScanPermissionGate(deps, deps, deps),
		RiskScanPermissionToolGate(deps, deps, deps),
		ShadowMCPPermissionGate(deps),
	)

	return r
}
