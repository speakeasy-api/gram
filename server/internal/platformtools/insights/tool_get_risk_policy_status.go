package insights

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type GetRiskPolicyStatus struct {
	provider func() RiskService
}

type getRiskPolicyStatusInput struct {
	ID string `json:"id" jsonschema:"The risk policy ID to get analysis status for."`
}

func NewGetRiskPolicyStatusTool(provider func() RiskService) *GetRiskPolicyStatus {
	return &GetRiskPolicyStatus{provider: provider}
}

func (s *GetRiskPolicyStatus) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "insights",
		HandlerName: "get_risk_policy_status",
		Name:        "platform_get_risk_policy_status",
		Description: "Get the analysis progress (pending vs analyzed counts, workflow state) for a risk policy. Requires organization admin access.",
		InputSchema: core.BuildInputSchema[getRiskPolicyStatusInput](),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *GetRiskPolicyStatus) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	svc := s.provider()
	if svc == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := getRiskPolicyStatusInput{ID: ""}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}
	if input.ID == "" {
		return fmt.Errorf("id is required")
	}

	result, err := svc.GetRiskPolicyStatus(ctx, &risk.GetRiskPolicyStatusPayload{
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
