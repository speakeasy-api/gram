package risk

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// falsePositiveResult is what the mark/unmark tools echo back: the service
// methods return only an error, so the model needs an explicit acknowledgement
// of what was acted on.
type falsePositiveResult struct {
	ResultIDs []string `json:"result_ids"`
}

type MarkRiskResultsFalsePositive struct {
	risk RiskService
}

type markRiskResultsFalsePositiveInput struct {
	ResultIDs []string `json:"result_ids" jsonschema:"IDs of the risk results to dismiss, as returned by platform_list_risk_results_for_agent. Max 500."`
	Reason    *string  `json:"reason,omitempty" jsonschema:"Optional free-text reason for the dismissal."`
}

func NewMarkRiskResultsFalsePositiveTool(riskSvc RiskService) *MarkRiskResultsFalsePositive {
	return &MarkRiskResultsFalsePositive{risk: riskSvc}
}

func (s *MarkRiskResultsFalsePositive) Descriptor() core.ToolDescriptor {
	destructive := false
	idempotent := true

	return core.ToolDescriptor{
		SourceSlug:  "risk",
		HandlerName: "mark_risk_results_false_positive",
		Name:        "platform_mark_risk_false_positive",
		Description: "Mark specific risk findings as manually-reviewed false positives, moving them to the Dismissed tab. This only suppresses the findings picked, not future findings that match the same rule — use platform_create_risk_exclusion for that. Reversible with platform_unmark_risk_false_positive.",
		InputSchema: core.BuildInputSchema[markRiskResultsFalsePositiveInput](),
		Variables:   nil,
		Annotations: writeAnnotations(destructive, idempotent),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *MarkRiskResultsFalsePositive) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.risk == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := markRiskResultsFalsePositiveInput{ResultIDs: nil, Reason: nil}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if len(input.ResultIDs) == 0 {
		return fmt.Errorf("result_ids is required")
	}

	if err := s.risk.MarkRiskResultsFalsePositive(ctx, &risk.MarkRiskResultsFalsePositivePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ResultIds:        input.ResultIDs,
		Reason:           input.Reason,
	}); err != nil {
		return fmt.Errorf("mark risk results false positive: %w", err)
	}

	return core.EncodeResult(wr, falsePositiveResult{ResultIDs: input.ResultIDs})
}
