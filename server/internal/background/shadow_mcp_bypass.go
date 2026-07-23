package background

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
)

// shadowMCPPolicyBypassChecker adapts the concrete risk evaluator to the
// scanner-facing interface without introducing a risk_analysis import cycle.
type shadowMCPPolicyBypassChecker struct {
	evaluator policyBypassBatchEvaluator
}

type policyBypassBatchEvaluator interface {
	CanBypassBatch(ctx context.Context, inputs []risk.PolicyBypassEvaluation) map[risk.PolicyBypassEvaluation]bool
}

func (c *shadowMCPPolicyBypassChecker) CanBypassShadowMCP(
	ctx context.Context,
	organizationID string,
	policyID uuid.UUID,
	requests []shadowmcpscan.BypassRequest,
) map[shadowmcpscan.BypassRequest]bool {
	results := make(map[shadowmcpscan.BypassRequest]bool, len(requests))
	evaluationRequests := make(map[risk.PolicyBypassEvaluation][]shadowmcpscan.BypassRequest, len(requests))
	evaluations := make([]risk.PolicyBypassEvaluation, 0, len(requests))
	for _, request := range requests {
		var target *risk.PolicyBypassTarget
		if request.Resolved {
			target = risk.ShadowMCPPolicyBypassTarget(request.Evidence, request.ToolName)
			if target == nil {
				continue
			}
		}
		evaluation := risk.PolicyBypassEvaluation{
			OrganizationID: organizationID,
			UserID:         request.UserID,
			PolicyID:       policyID.String(),
			Target:         target,
		}
		evaluations = append(evaluations, evaluation)
		evaluationRequests[evaluation] = append(evaluationRequests[evaluation], request)
	}

	for evaluation, allowed := range c.evaluator.CanBypassBatch(ctx, evaluations) {
		if allowed {
			for _, request := range evaluationRequests[evaluation] {
				results[request] = true
			}
		}
	}
	return results
}
