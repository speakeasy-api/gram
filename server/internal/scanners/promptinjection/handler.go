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

// Handler is the only prompt-injection engine for batch analysis: the
// AnalyzeBatch activity publishes a request per scanned message and does not
// call the judge itself, so every request must reach the real classifier.
// That is why this handler has no AsyncShadowGate — sampling here would drop
// findings outright rather than shrink a comparison lane (AIS-722).
type Handler struct {
	logger       *slog.Logger
	findingsPub  gcp.Publisher[*riskv1.Finding]
	metrics      *scanners.AsyncScanHandlerMetrics
	scanner      *Scanner
	riskRecorder *metering.RiskRecorder
}

// NewHandler builds the prompt-injection subscription handler. A nil scanner
// falls back to the no-op classifier, which reaches no verdict and publishes
// nothing — a deployment without a judge acks requests untouched instead of
// recording content as clean.
func NewHandler(logger *slog.Logger, meterProvider metric.MeterProvider, scanner *Scanner, findingsPub gcp.Publisher[*riskv1.Finding], riskRecorder *metering.RiskRecorder) *Handler {
	if scanner == nil {
		scanner = NewScanner(logger, NoopClassifier)
	}
	return &Handler{
		logger:       logger.With(attr.SlogComponent("prompt-injection-analyzer")),
		findingsPub:  findingsPub,
		metrics:      scanners.NewAsyncScanHandlerMetrics(meterProvider, logger),
		scanner:      scanner,
		riskRecorder: riskRecorder,
	}
}

func (h *Handler) Handle(ctx context.Context, m *riskv1.PromptInjectionAnalysis, _ gcp.MessageMetadata) error {
	anchorID := m.GetChatMessageId()
	if anchorID == "" {
		anchorID = m.GetContentPartId()
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attr.RiskScanRequestID(m.GetRequestId()),
		attr.MessageID(anchorID),
		attr.AuthOrganizationID(m.GetOrganizationId()),
		attr.RiskScanEngine(scanners.AsyncScanEngineReal),
		attr.RiskScanGateReason(scanners.AsyncShadowGateReasonNotGated),
	)
	// message.id carries whichever anchor resolved, so a part-anchored scan also
	// gets the dedicated key: without it those spans are unsearchable by part.
	if partID := m.GetContentPartId(); partID != "" {
		span.SetAttributes(attr.ChatContentPartID(partID))
	}

	judgeMessage := promptInjectionJudgeMessage(m)
	startedAt := time.Now().UTC()
	result, verdict, err := h.scanner.ScanWithVerdict(ctx, m.GetContent(), m.GetOrganizationId(), m.GetProjectId(), m.GetUserId(), judgeMessage, judgemessage.Trajectory{
		PriorUserRequest:       m.GetPriorUserRequest(),
		RecentUntrustedContent: m.GetRecentUntrustedContent(),
	})
	if err != nil {
		h.recordHandled(ctx, m.GetOrganizationId(), scanners.AsyncScanOutcomeScanError)
		return fmt.Errorf("scan prompt injection: %w", err)
	}
	findings := result.Findings

	// A judge that reached no verdict (throttled, timed out, provider down)
	// fails open: the scan acks with no findings, exactly as the inline scan
	// used to. Now that this consumer is the only prompt-injection engine for
	// batch analysis, that silence is a message nothing will ever judge, so it
	// is counted apart from a clean verdict.
	outcome := scanners.AsyncScanOutcomeOK
	if !result.Completed && (m.GetContent() != "" || judgeMessage.HasContent()) {
		outcome = scanners.AsyncScanOutcomeNoVerdict
	}

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
		Shadow:            false,
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
		h.recordHandled(ctx, m.GetOrganizationId(), scanners.AsyncScanOutcomePublishError)
		return err
	}

	h.recordHandled(ctx, m.GetOrganizationId(), outcome)
	return nil
}

func (h *Handler) recordHandled(ctx context.Context, orgID, outcome string) {
	h.metrics.RecordHandled(ctx, orgID, Source, scanners.AsyncScanEngineReal, outcome, scanners.AsyncShadowGateReasonNotGated)
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
