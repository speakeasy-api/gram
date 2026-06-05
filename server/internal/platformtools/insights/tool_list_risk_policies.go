package insights

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ListRiskPolicies struct {
	provider func() RiskService
}

type listRiskPoliciesInput struct{}

func NewListRiskPoliciesTool(provider func() RiskService) *ListRiskPolicies {
	return &ListRiskPolicies{provider: provider}
}

func (s *ListRiskPolicies) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "insights",
		HandlerName: "list_risk_policies",
		Name:        "platform_list_risk_policies",
		Description: "List the risk/safety policies configured for the current project. Requires organization admin access.",
		InputSchema: core.BuildInputSchema[listRiskPoliciesInput](),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListRiskPolicies) Call(ctx context.Context, _ toolconfig.ToolCallEnv, _ io.Reader, wr io.Writer) error {
	svc := s.provider()
	if svc == nil {
		return fmt.Errorf("risk service not configured")
	}

	result, err := svc.ListRiskPolicies(ctx, &risk.ListRiskPoliciesPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return fmt.Errorf("list risk policies: %w", err)
	}

	return encodeToolResult(wr, result)
}
