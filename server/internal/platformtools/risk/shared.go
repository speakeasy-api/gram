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
	GetRiskRuleBreakdown(ctx context.Context, payload *risk.GetRiskRuleBreakdownPayload) (*risk.RiskRuleBreakdownResult, error)
	ListRiskExclusions(ctx context.Context, payload *risk.ListRiskExclusionsPayload) (*risk.ListRiskExclusionsResult, error)
	CreateRiskExclusion(ctx context.Context, payload *risk.CreateRiskExclusionPayload) (*types.RiskExclusion, error)
	MarkRiskResultsFalsePositive(ctx context.Context, payload *risk.MarkRiskResultsFalsePositivePayload) error
	UnmarkRiskResultsFalsePositive(ctx context.Context, payload *risk.UnmarkRiskResultsFalsePositivePayload) error
}

// writeAnnotations describes a tool that mutates project state. Kept separate
// from core.ReadOnlyAnnotations so MCP clients can prompt before the call.
func writeAnnotations(destructive, idempotent bool) *types.ToolAnnotations {
	readOnly := false
	openWorld := false
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}
