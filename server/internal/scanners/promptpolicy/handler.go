package promptpolicy

import (
	"context"
	"fmt"
	"log/slog"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
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

	published, ruleIDs, err := scanners.PublishFindings(ctx, h.logger, h.findingsPub, scanners.FindingMetadata{
		RequestID:         m.GetRequestId(),
		ChatMessageID:     m.GetChatMessageId(),
		ProjectID:         m.GetProjectId(),
		OrganizationID:    m.GetOrganizationId(),
		RiskPolicyID:      m.GetRiskPolicyId(),
		RiskPolicyVersion: m.GetRiskPolicyVersion(),
	}, findings, "prompt policy")
	if err != nil {
		return fmt.Errorf("publish prompt policy findings: %w", err)
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
		calls = append(calls, judgemessage.NewToolCall(call.GetName(), call.GetArguments()))
	}
	return judgemessage.NewForToolCalls(calls)
}
