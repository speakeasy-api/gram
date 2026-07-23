package background

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// shadowMCPPolicyBypassChecker adapts the concrete risk evaluator to the
// scanner-facing interface without introducing a risk_analysis import cycle.
type shadowMCPPolicyBypassChecker struct {
	evaluator *risk.PolicyBypassEvaluator
}

func (c *shadowMCPPolicyBypassChecker) CanBypassShadowMCP(
	ctx context.Context,
	organizationID string,
	userID string,
	policyID uuid.UUID,
	evidence shadowmcp.AccessEvidence,
	toolName string,
) bool {
	target := risk.ShadowMCPPolicyBypassTarget(evidence, toolName)
	if target == nil {
		return false
	}
	return c.evaluator.CanBypass(ctx, risk.PolicyBypassEvaluation{
		OrganizationID: organizationID,
		UserID:         userID,
		PolicyID:       policyID.String(),
		Target:         target,
	})
}
