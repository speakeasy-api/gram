package risk

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type UnmarkRiskResultsFalsePositive struct {
	risk RiskService
}

type unmarkRiskResultsFalsePositiveInput struct {
	ResultIDs []string `json:"result_ids" jsonschema:"IDs of the dismissed risk results to restore. Max 500 per call. IDs that don't exist or belong to another project are skipped without error."`
}

func NewUnmarkRiskResultsFalsePositiveTool(riskSvc RiskService) *UnmarkRiskResultsFalsePositive {
	return &UnmarkRiskResultsFalsePositive{risk: riskSvc}
}

func (s *UnmarkRiskResultsFalsePositive) Descriptor() core.ToolDescriptor {
	destructive := false
	idempotent := true

	return core.ToolDescriptor{
		SourceSlug:  "risk",
		HandlerName: "unmark_risk_results_false_positive",
		Name:        "platform_unmark_risk_false_positive",
		Description: "Undo a false-positive dismissal, returning the findings to the active results list.",
		InputSchema: core.BuildInputSchema[unmarkRiskResultsFalsePositiveInput](
			core.WithPropertyItemsRange("result_ids", 1, maxFalsePositiveBatch),
		),
		Variables:   nil,
		Annotations: writeAnnotations(destructive, idempotent),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *UnmarkRiskResultsFalsePositive) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.risk == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := unmarkRiskResultsFalsePositiveInput{ResultIDs: nil}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if len(input.ResultIDs) == 0 {
		return fmt.Errorf("result_ids is required")
	}

	if err := s.risk.UnmarkRiskResultsFalsePositive(ctx, &risk.UnmarkRiskResultsFalsePositivePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ResultIds:        input.ResultIDs,
	}); err != nil {
		return fmt.Errorf("unmark risk results false positive: %w", err)
	}

	return core.EncodeResult(wr, falsePositiveResult{RequestedResultIDs: input.ResultIDs})
}
