package promptinjection

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
		logger:      logger.With(attr.SlogComponent("prompt-injection-analyzer")),
		findingsPub: findingsPub,
		scanner:     scanner,
	}
}

func (h *Handler) Handle(ctx context.Context, m *riskv1.PromptInjectionAnalysis, _ gcp.MessageMetadata) error {
	findings, err := h.scanner.Scan(ctx, m.GetContent(), m.GetOrganizationId(), m.GetProjectId(), m.GetUserId(), promptInjectionJudgeMessage(m))
	if err != nil {
		return fmt.Errorf("scan prompt injection: %w", err)
	}

	published, ruleIDs := scanners.PublishFindings(ctx, h.logger, h.findingsPub, scanners.FindingMetadata{
		RequestID:         m.GetRequestId(),
		ChatMessageID:     m.GetChatMessageId(),
		ProjectID:         m.GetProjectId(),
		OrganizationID:    m.GetOrganizationId(),
		RiskPolicyID:      m.GetRiskPolicyId(),
		RiskPolicyVersion: m.GetRiskPolicyVersion(),
	}, findings, "prompt injection")

	h.logger.InfoContext(ctx, "prompt injection scan complete", attr.SlogValueAny(map[string]any{
		"request_id":      m.GetRequestId(),
		"chat_message_id": m.GetChatMessageId(),
		"detections":      len(findings),
		"published":       published,
		"rule_ids":        ruleIDs,
	}))

	return nil
}

func promptInjectionJudgeMessage(m *riskv1.PromptInjectionAnalysis) judgemessage.Message {
	if len(m.GetToolCalls()) == 0 {
		return judgemessage.New(m.GetMessageType(), m.GetToolName(), m.GetBody())
	}

	calls := make([]judgemessage.ToolCall, 0, len(m.GetToolCalls()))
	for _, call := range m.GetToolCalls() {
		calls = append(calls, judgemessage.NewToolCall(call.GetName(), call.GetArguments()))
	}
	return judgemessage.NewForToolCalls(calls)
}
