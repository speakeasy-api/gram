package promptpolicy

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

type Handler struct {
	logger      *slog.Logger
	findingsPub gcp.Publisher[*riskv1.Finding]
	metrics     *scanners.AsyncScanHandlerMetrics
	realScanner *Scanner
	stubScanner *Scanner
	gate        *scanners.AsyncShadowGate
}

func NewHandler(logger *slog.Logger, meterProvider metric.MeterProvider, realScanner, stubScanner *Scanner, findingsPub gcp.Publisher[*riskv1.Finding], gate *scanners.AsyncShadowGate) *Handler {
	if stubScanner == nil {
		stubScanner = NewScanner(logger, NoopEvaluator)
	}
	if realScanner == nil {
		realScanner = stubScanner
	}
	return &Handler{
		logger:      logger.With(attr.SlogComponent("prompt-policy-analyzer")),
		findingsPub: findingsPub,
		metrics:     scanners.NewAsyncScanHandlerMetrics(meterProvider, logger),
		realScanner: realScanner,
		stubScanner: stubScanner,
		gate:        gate,
	}
}

func (h *Handler) Handle(ctx context.Context, m *riskv1.PromptPolicyAnalysis, _ gcp.MessageMetadata) error {
	cfg := ParseConfig(m.GetModelConfig())
	gateReason := h.gate.Decide(ctx, m.GetProjectId(), m.GetChatMessageId())
	engine := gateReason.Engine()
	trace.SpanFromContext(ctx).SetAttributes(
		attr.RiskScanRequestID(m.GetRequestId()),
		attr.MessageID(m.GetChatMessageId()),
		attr.AuthOrganizationID(m.GetOrganizationId()),
		attr.RiskScanEngine(engine),
		attr.RiskScanGateReason(gateReason),
	)

	scanner := h.stubScanner
	if engine == scanners.AsyncScanEngineReal {
		scanner = h.realScanner
	}

	findings := scanner.Scan(ctx, m.GetOrganizationId(), m.GetProjectId(), m.GetUserId(), m.GetPrompt(), cfg, promptPolicyJudgeMessage(m))

	_, _, err := scanners.PublishFindings(ctx, h.logger, h.findingsPub, scanners.FindingMetadata{
		RequestID:         m.GetRequestId(),
		ChatMessageID:     m.GetChatMessageId(),
		ProjectID:         m.GetProjectId(),
		OrganizationID:    m.GetOrganizationId(),
		RiskPolicyID:      m.GetRiskPolicyId(),
		RiskPolicyVersion: m.GetRiskPolicyVersion(),
	}, findings, "prompt policy")
	if err != nil {
		h.metrics.RecordHandled(ctx, m.GetOrganizationId(), Source, engine, scanners.AsyncScanOutcomePublishError, gateReason)
		return fmt.Errorf("publish prompt policy findings: %w", err)
	}

	h.metrics.RecordHandled(ctx, m.GetOrganizationId(), Source, engine, scanners.AsyncScanOutcomeOK, gateReason)
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
