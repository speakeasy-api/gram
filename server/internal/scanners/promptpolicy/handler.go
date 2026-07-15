package promptpolicy

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
)

type Handler struct {
	logger      *slog.Logger
	findingsPub gcp.Publisher[*riskv1.Finding]
	scanner     *Scanner
}

func NewHandler(logger *slog.Logger, scanner *Scanner, findingsPub gcp.Publisher[*riskv1.Finding]) *Handler {
	return &Handler{
		logger:      logger.With(attr.SlogComponent("prompt-policy-analyzer")),
		findingsPub: findingsPub,
		scanner:     scanner,
	}
}

func (h *Handler) Handle(ctx context.Context, m *riskv1.PromptPolicyAnalysis, _ gcp.MessageMetadata) error {
	cfg := ParseConfig(m.GetModelConfig())
	findings := h.scanner.Scan(ctx, m.GetOrganizationId(), m.GetProjectId(), m.GetUserId(), m.GetPrompt(), cfg, promptPolicyJudgeMessage(m))

	createdAt := time.Now().UTC().Format(time.RFC3339)
	results := make([]gcp.PublishResult, 0, len(findings))
	ruleIDs := make([]string, 0, len(findings))
	for _, f := range findings {
		id, err := uuid.NewV7()
		if err != nil {
			h.logger.WarnContext(ctx, "failed to generate finding id", attr.SlogError(err))
			continue
		}

		startPos := conv.SafeInt32(f.StartPos)
		endPos := conv.SafeInt32(f.EndPos)
		finding := riskv1.Finding_builder{
			Id:                new(id.String()),
			RequestId:         new(m.GetRequestId()),
			ChatMessageId:     new(m.GetChatMessageId()),
			ProjectId:         new(m.GetProjectId()),
			OrganizationId:    new(m.GetOrganizationId()),
			RiskPolicyId:      new(m.GetRiskPolicyId()),
			RiskPolicyVersion: new(m.GetRiskPolicyVersion()),
			CreatedAt:         &createdAt,
			RuleId:            &f.RuleID,
			Description:       &f.Description,
			Match:             &f.Match,
			StartPos:          &startPos,
			EndPos:            &endPos,
			Tags:              f.Tags,
			Source:            &f.Source,
			Confidence:        &f.Confidence,
		}.Build()

		results = append(results, h.findingsPub.Publish(ctx, finding))
		ruleIDs = append(ruleIDs, f.RuleID)
	}

	published := 0
	for _, res := range results {
		if _, err := res.Get(ctx); err != nil {
			h.logger.WarnContext(ctx, "failed to publish prompt policy finding", attr.SlogError(err))
			continue
		}
		published++
	}

	h.logger.InfoContext(ctx, "prompt policy scan complete", attr.SlogValueAny(map[string]any{
		"request_id":      m.GetRequestId(),
		"chat_message_id": m.GetChatMessageId(),
		"detections":      len(findings),
		"published":       published,
		"rule_ids":        ruleIDs,
	}))

	return nil
}

func promptPolicyJudgeMessage(m *riskv1.PromptPolicyAnalysis) judgemessage.Message {
	if len(m.GetToolCalls()) == 0 {
		return judgemessage.New(m.GetMessageType(), m.GetToolName(), m.GetBody())
	}

	calls := make([]judgemessage.ToolCall, 0, len(m.GetToolCalls()))
	for _, call := range m.GetToolCalls() {
		calls = append(calls, judgemessage.NewToolCall(call.GetToolName(), call.GetArguments()))
	}
	return judgemessage.NewForToolCalls(calls)
}
