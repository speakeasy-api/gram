package risk

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type GetRiskRuleBreakdown struct {
	risk RiskService
}

type getRiskRuleBreakdownInput struct {
	Category string  `json:"category" jsonschema:"Category key to break down by rule_id."`
	From     *string `json:"from,omitempty" jsonschema:"Inclusive start of the window as an ISO 8601 timestamp. Defaults to the same 7-day window as the risk overview."`
	To       *string `json:"to,omitempty" jsonschema:"Exclusive end of the window as an ISO 8601 timestamp. Defaults to now."`
}

func NewGetRiskRuleBreakdownTool(riskSvc RiskService) *GetRiskRuleBreakdown {
	return &GetRiskRuleBreakdown{risk: riskSvc}
}

// categoryKeys enumerates the category registry into the input schema so the
// model picks a real key instead of guessing one and getting an empty result.
func categoryKeys() []any {
	defs := categories.All()
	keys := make([]any, 0, len(defs))
	for _, def := range defs {
		keys = append(keys, string(def.Category))
	}
	return keys
}

func (s *GetRiskRuleBreakdown) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "risk",
		HandlerName: "get_risk_rule_breakdown",
		Name:        "platform_get_risk_rule_breakdown",
		Description: "Get per-rule_id finding counts for one category over a time window. Prefer this over paginating platform_list_risk_results_for_agent when the question is about volume or which rules fire most — it answers in one small call instead of many large pages.",
		InputSchema: core.BuildInputSchema[getRiskRuleBreakdownInput](
			core.WithPropertyEnum("category", categoryKeys()...),
			core.WithPropertyFormat("from", "date-time"),
			core.WithPropertyFormat("to", "date-time"),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *GetRiskRuleBreakdown) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.risk == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := getRiskRuleBreakdownInput{Category: "", From: nil, To: nil}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.Category == "" {
		return fmt.Errorf("category is required")
	}

	result, err := s.risk.GetRiskRuleBreakdown(ctx, &risk.GetRiskRuleBreakdownPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Category:         input.Category,
		From:             input.From,
		To:               input.To,
	})
	if err != nil {
		return fmt.Errorf("get risk rule breakdown: %w", err)
	}

	return core.EncodeResult(wr, result)
}
