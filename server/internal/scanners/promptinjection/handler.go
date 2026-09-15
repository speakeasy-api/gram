package promptinjection

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

type Handler struct {
	logger       *slog.Logger
	findingsPub  gcp.Publisher[*riskv1.Finding]
	metrics      *scanners.AsyncScanHandlerMetrics
	realScanner  *Scanner
	stubScanner  *Scanner
	gate         *scanners.AsyncShadowGate
	riskRecorder *metering.RiskRecorder
}

func NewHandler(logger *slog.Logger, meterProvider metric.MeterProvider, realScanner, stubScanner *Scanner, findingsPub gcp.Publisher[*riskv1.Finding], gate *scanners.AsyncShadowGate, riskRecorder *metering.RiskRecorder) *Handler {
	if stubScanner == nil {
		stubScanner = NewScanner(logger, NoopClassifier)
	}
	if realScanner == nil {
		realScanner = stubScanner
	}
	return &Handler{
		logger:       logger.With(attr.SlogComponent("prompt-injection-analyzer")),
		findingsPub:  findingsPub,
		metrics:      scanners.NewAsyncScanHandlerMetrics(meterProvider, logger),
		realScanner:  realScanner,
		stubScanner:  stubScanner,
		gate:         gate,
		riskRecorder: riskRecorder,
	}
}

func (h *Handler) Handle(ctx context.Context, m *riskv1.PromptInjectionAnalysis, _ gcp.MessageMetadata) error {
	anchorID := m.GetChatMessageId()
	if anchorID == "" {
		anchorID = m.GetContentPartId()
	}
	gateReason := h.gate.Decide(ctx, m.GetProjectId(), anchorID)
	engine := gateReason.Engine()
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attr.RiskScanRequestID(m.GetRequestId()),
		attr.MessageID(anchorID),
		attr.AuthOrganizationID(m.GetOrganizationId()),
		attr.RiskScanEngine(engine),
		attr.RiskScanGateReason(gateReason),
	)
	// message.id carries whichever anchor resolved, so a part-anchored scan also
	// gets the dedicated key: without it those spans are unsearchable by part.
	if partID := m.GetContentPartId(); partID != "" {
		span.SetAttributes(attr.ChatContentPartID(partID))
	}

	scanner := h.stubScanner
	if engine == scanners.AsyncScanEngineReal {
		scanner = h.realScanner
	}

	startedAt := time.Now().UTC()
	result, verdict, err := scanner.ScanWithVerdict(ctx, m.GetContent(), m.GetOrganizationId(), m.GetProjectId(), m.GetUserId(), promptInjectionJudgeMessage(m), judgemessage.Trajectory{
		PriorUserRequest:       m.GetPriorUserRequest(),
		RecentUntrustedContent: m.GetRecentUntrustedContent(),
	})
	if err != nil {
		h.metrics.RecordHandled(ctx, m.GetOrganizationId(), Source, engine, scanners.AsyncScanOutcomeScanError, gateReason)
		return fmt.Errorf("scan prompt injection: %w", err)
	}
	findings := result.Findings

	// The scan proceeds on empty content as long as the judge message has
	// content (tool name/calls), but the finding it produces has an empty
	// match, no fingerprint, and nothing revealable — pure noise as a
	// persisted row. Skip publishing; the classification (judge telemetry)
	// and the handled metrics below are unaffected.
	if m.GetContent() == "" {
		findings = nil
	}

	_, _, err = scanners.PublishFindings(ctx, h.logger, h.findingsPub, scanners.FindingMetadata{
		RequestID:         m.GetRequestId(),
		ChatMessageID:     m.GetChatMessageId(),
		ContentPartID:     m.GetContentPartId(),
		ProjectID:         m.GetProjectId(),
		OrganizationID:    m.GetOrganizationId(),
		RiskPolicyID:      m.GetRiskPolicyId(),
		RiskPolicyVersion: m.GetRiskPolicyVersion(),
	}, findings, "prompt injection")
	if err != nil {
		err = fmt.Errorf("publish prompt injection findings: %w", err)
	}

	if result.Completed {
		provenance, provenanceErr := scanners.ParseRiskProvenance(m, m.GetMessageType(), "async")
		if provenanceErr != nil {
			h.logger.WarnContext(ctx, "skipping prompt injection usage with invalid attribution", attr.SlogError(provenanceErr))
		} else {
			provenance.Model = verdict.Model
			provenance.Provider = verdict.Provider
			if meterErr := h.riskRecorder.Record(ctx, metering.RiskPromptInjection(), provenance, result.STokens, startedAt); meterErr != nil {
				h.logger.ErrorContext(ctx, "record prompt injection usage", attr.SlogError(meterErr))
			}
		}
	}
	if err != nil {
		h.metrics.RecordHandled(ctx, m.GetOrganizationId(), Source, engine, scanners.AsyncScanOutcomePublishError, gateReason)
		return err
	}

	h.metrics.RecordHandled(ctx, m.GetOrganizationId(), Source, engine, scanners.AsyncScanOutcomeOK, gateReason)
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
