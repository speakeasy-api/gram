package risk

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ListRiskPolicies struct {
	risk RiskService
}

type listRiskPoliciesInput struct{}

func NewListRiskPoliciesTool(riskSvc RiskService) *ListRiskPolicies {
	return &ListRiskPolicies{risk: riskSvc}
}

func (s *ListRiskPolicies) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "risk",
		HandlerName: "list_risk_policies",
		Name:        "platform_list_risk_policies",
		Description: "List the risk analysis policies configured for the current project.",
		InputSchema: core.BuildInputSchema[listRiskPoliciesInput](),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListRiskPolicies) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.risk == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := listRiskPoliciesInput{}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}

	result, err := s.risk.ListRiskPolicies(ctx, &risk.ListRiskPoliciesPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return fmt.Errorf("list risk policies: %w", err)
	}

	return encodeToolResult(wr, result)
}
