package risk

import (
	"context"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
)

// RiskService is the subset of the risk management service that the managed
// assistant's risk tools call. The concrete risk service satisfies it; tools
// pass nil auth tokens because the assistant runtime supplies auth context out
// of band.
type RiskService interface {
	ListRiskPolicies(ctx context.Context, payload *risk.ListRiskPoliciesPayload) (*risk.ListRiskPoliciesResult, error)
	ListRiskResultsForAgent(ctx context.Context, payload *risk.ListRiskResultsForAgentPayload) (*risk.ListRiskResultsForAgentResult, error)
	ListRiskResultsByChat(ctx context.Context, payload *risk.ListRiskResultsByChatPayload) (*risk.ListRiskResultsByChatResult, error)
	GetRiskPolicyStatus(ctx context.Context, payload *risk.GetRiskPolicyStatusPayload) (*types.RiskPolicyStatus, error)
}
