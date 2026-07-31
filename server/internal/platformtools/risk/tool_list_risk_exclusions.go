package risk

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ListRiskExclusions struct {
	risk RiskService
}

type listRiskExclusionsInput struct {
	RiskPolicyID *string `json:"risk_policy_id,omitempty" jsonschema:"Only return exclusions bound to this policy ID. Omit to return every exclusion in the project (global plus policy-bound)."`
}

func NewListRiskExclusionsTool(riskSvc RiskService) *ListRiskExclusions {
	return &ListRiskExclusions{risk: riskSvc}
}

func (s *ListRiskExclusions) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "risk",
		HandlerName: "list_risk_exclusions",
		Name:        "platform_list_risk_exclusions",
		Description: "List the risk exclusions configured for the current project. Call this before creating an exclusion to check whether an equivalent one already exists.",
		InputSchema: core.BuildInputSchema[listRiskExclusionsInput](
			core.WithPropertyFormat("risk_policy_id", "uuid"),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListRiskExclusions) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.risk == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := listRiskExclusionsInput{RiskPolicyID: nil}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}

	result, err := s.risk.ListRiskExclusions(ctx, &risk.ListRiskExclusionsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		RiskPolicyID:     input.RiskPolicyID,
	})
	if err != nil {
		return fmt.Errorf("list risk exclusions: %w", err)
	}

	return core.EncodeResult(wr, result)
}
