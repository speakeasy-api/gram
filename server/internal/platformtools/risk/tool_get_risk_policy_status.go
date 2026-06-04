package risk

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type GetRiskPolicyStatus struct {
	risk RiskService
}

type getRiskPolicyStatusInput struct {
	ID string `json:"id" jsonschema:"The risk policy ID."`
}

func NewGetRiskPolicyStatusTool(riskSvc RiskService) *GetRiskPolicyStatus {
	return &GetRiskPolicyStatus{risk: riskSvc}
}

func (s *GetRiskPolicyStatus) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "risk",
		HandlerName: "get_risk_policy_status",
		Name:        "platform_get_risk_policy_status",
		Description: "Get the analysis status of a risk policy, including progress and workflow state.",
		InputSchema: core.BuildInputSchema[getRiskPolicyStatusInput](
			core.WithPropertyFormat("id", "uuid"),
		),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *GetRiskPolicyStatus) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.risk == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := getRiskPolicyStatusInput{ID: ""}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}
	if input.ID == "" {
		return fmt.Errorf("id is required")
	}

	result, err := s.risk.GetRiskPolicyStatus(ctx, &risk.GetRiskPolicyStatusPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               input.ID,
	})
	if err != nil {
		return fmt.Errorf("get risk policy status: %w", err)
	}

	return encodeToolResult(wr, result)
}
