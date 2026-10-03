package risk

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
)

// ShadowMCPBypassChecker answers the shadow MCP scanner's bypass checks with
// the policy bypass grants PolicyBypassEvaluator resolves.
type ShadowMCPBypassChecker struct {
	evaluator policyBypassBatchEvaluator
}

func NewShadowMCPBypassChecker(evaluator *PolicyBypassEvaluator) *ShadowMCPBypassChecker {
	return &ShadowMCPBypassChecker{evaluator: evaluator}
}

type policyBypassBatchEvaluator interface {
	CanBypassBatch(ctx context.Context, inputs []PolicyBypassEvaluation) map[PolicyBypassEvaluation]bool
}

func (c *ShadowMCPBypassChecker) CanBypassShadowMCP(
	ctx context.Context,
	organizationID string,
	policyID uuid.UUID,
	requests []shadowmcpscan.BypassRequest,
) map[shadowmcpscan.BypassRequest]bool {
	results := make(map[shadowmcpscan.BypassRequest]bool, len(requests))
	evaluationRequests := make(map[PolicyBypassEvaluation][]shadowmcpscan.BypassRequest, len(requests))
	evaluations := make([]PolicyBypassEvaluation, 0, len(requests))
	for _, request := range requests {
		var target *PolicyBypassTarget
		if request.Resolved {
			target = ShadowMCPPolicyBypassTarget(request.Evidence, request.ToolName)
			if target == nil {
				continue
			}
		}
		evaluation := PolicyBypassEvaluation{
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
