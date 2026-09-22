package llmanalyzer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
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

// Provider is the model provider recorded on analyzer usage readings. The
// fine-tuned model is served by a single Baseten deployment shared across
// environments.
const Provider = "baseten"

// asyncExecutionPath is the metering execution path assumed for batch
// requests that carry none.
const asyncExecutionPath = "async"

// Handler consumes LLMAnalysis requests from the batch flag lane, evaluates
// each message with the fine-tuned risk model and publishes one Finding per
// positive risk into the shared Finding topic. It bypasses the async shadow
// gate: requests only reach this lane for organizations whose
// feature.FlagRiskLLMAnalyzer mode is llm, where the model is the real
// engine, or shadow, where the legacy engines enforce the same batch and the
// model's verdict is recorded for comparison only.
//
// Findings are ClickHouse-only: nothing writes them to the Postgres
// risk_results table. Shadow requests are evaluated and metered like every
// other request but their findings are withheld from the Finding topic until
// the findings store can mark shadow rows and hide them by default.
type Handler struct {
	logger       *slog.Logger
	findingsPub  gcp.Publisher[*riskv1.Finding]
	metrics      *scanners.AsyncScanHandlerMetrics
	analyzer     *Analyzer
	riskRecorder *metering.RiskRecorder
	disabledOnce sync.Once
}

// NewHandler builds the async lane handler over analyzer. A disabled analyzer
// (no model URL configured) makes the handler ack every message untouched.
func NewHandler(logger *slog.Logger, meterProvider metric.MeterProvider, analyzer *Analyzer, findingsPub gcp.Publisher[*riskv1.Finding], riskRecorder *metering.RiskRecorder) *Handler {
	return &Handler{
		logger:       logger.With(attr.SlogComponent("llm-analyzer")),
		findingsPub:  findingsPub,
		metrics:      scanners.NewAsyncScanHandlerMetrics(meterProvider, logger),
		analyzer:     analyzer,
		riskRecorder: riskRecorder,
		disabledOnce: sync.Once{},
	}
}

// Handle evaluates one message and publishes its findings. The return value
// is the ack signal: only a publish failure nacks the message, because an
// acked request must mean every finding it produced is durably on the topic.
// Analyzer failures (timeout, upstream error, unparsable reply) are acked
// with nothing published, mirroring the prompt policy lane: redelivery would
// re-run the same model call, and the Finding topic carries no dead-letter
// marker on this lane.
func (h *Handler) Handle(ctx context.Context, m *riskv1.LLMAnalysis, _ gcp.MessageMetadata) error {
	anchorID := m.GetChatMessageId()
	if anchorID == "" {
		anchorID = m.GetContentPartId()
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attr.RiskScanRequestID(m.GetRequestId()),
		attr.MessageID(anchorID),
		attr.AuthOrganizationID(m.GetOrganizationId()),
		attr.OrganizationSlug(m.GetOrganizationSlug()),
		attr.RiskScanMode(ScanModeAsync),
		attr.RiskScanEngine(scanners.AsyncScanEngineReal),
	)
	// message.id carries whichever anchor resolved, so a part-anchored scan also
	// gets the dedicated key: without it those spans are unsearchable by part.
	if partID := m.GetContentPartId(); partID != "" {
		span.SetAttributes(attr.ChatContentPartID(partID))
	}

	if !h.analyzer.Enabled() {
		h.disabledOnce.Do(func() {
			h.logger.WarnContext(ctx, "LLM analyzer disabled: GRAM_RISK_LLM_URL empty; acking analysis requests without findings; batch routing falls back to the legacy engines under this config, so this ack is only a safety net for in-flight requests")
		})
		h.metrics.RecordHandled(ctx, m.GetOrganizationId(), Source, scanners.AsyncScanEngineReal, scanners.AsyncScanOutcomeDisabled, scanners.AsyncShadowGateReasonNotGated)
		return nil
	}

	msg, toolCallIDs := judgeMessage(m)
	startedAt := time.Now().UTC()
	analysis := h.analyzer.Analyze(ctx, Request{
		OrgID:       m.GetOrganizationId(),
		OrgSlug:     m.GetOrganizationSlug(),
		ProjectID:   m.GetProjectId(),
		ScanMode:    ScanModeAsync,
		Message:     msg,
		ToolCallIDs: toolCallIDs,
	})
	if analysis.Err != nil {
		h.logger.WarnContext(ctx, "llm analyzer scan failed; acking without findings",
			attr.SlogError(analysis.Err),
			attr.SlogRiskScanRequestID(m.GetRequestId()),
			attr.SlogOrganizationID(m.GetOrganizationId()),
		)
		h.metrics.RecordHandled(ctx, m.GetOrganizationId(), Source, scanners.AsyncScanEngineReal, scanners.AsyncScanOutcomeScanError, scanners.AsyncShadowGateReasonNotGated)
		return nil
	}
	result := analysis.Result
	// The envelope's sources are the policy sources this request stands in
	// for; only the risks covering them may become findings of that policy.
	if sources := m.GetSources(); len(sources) > 0 {
		result.Findings = FindingsForSources(result.Findings, sources)
	}

	var err error
	if m.GetShadow() {
		h.logger.DebugContext(ctx, "llm analyzer shadow findings withheld from the finding topic",
			attr.SlogRiskScanRequestID(m.GetRequestId()),
			attr.SlogOrganizationID(m.GetOrganizationId()),
			attr.SlogRiskPolicyID(m.GetRiskPolicyId()),
			attr.SlogRiskLLMFindingCount(len(result.Findings)),
		)
	} else {
		_, _, err = scanners.PublishFindings(ctx, h.logger, h.findingsPub, scanners.FindingMetadata{
			RequestID:         m.GetRequestId(),
			ChatMessageID:     m.GetChatMessageId(),
			ContentPartID:     m.GetContentPartId(),
			ProjectID:         m.GetProjectId(),
			OrganizationID:    m.GetOrganizationId(),
			RiskPolicyID:      m.GetRiskPolicyId(),
			RiskPolicyVersion: m.GetRiskPolicyVersion(),
		}, result.Findings, "llm analyzer")
		if err != nil {
			err = fmt.Errorf("publish llm analyzer findings: %w", err)
		}
	}

	if result.Completed {
		provenance, provenanceErr := scanners.ParseRiskProvenance(m, m.GetMessageType(), asyncExecutionPath)
		if provenanceErr != nil {
			h.logger.WarnContext(ctx, "skipping llm analyzer usage with invalid attribution", attr.SlogError(provenanceErr))
		} else {
			provenance.Model = analysis.Completion.Model
			provenance.Provider = Provider
			if meterErr := h.riskRecorder.Record(ctx, metering.RiskLLMAnalyzer(), provenance, result.STokens, startedAt); meterErr != nil {
				h.logger.ErrorContext(ctx, "record llm analyzer usage", attr.SlogError(meterErr))
			}
		}
	}
	if err != nil {
		h.metrics.RecordHandled(ctx, m.GetOrganizationId(), Source, scanners.AsyncScanEngineReal, scanners.AsyncScanOutcomePublishError, scanners.AsyncShadowGateReasonNotGated)
		return err
	}

	outcome := scanners.AsyncScanOutcomeOK
	if m.GetShadow() {
		outcome = scanners.AsyncScanOutcomeShadowUnpublished
	}
	h.metrics.RecordHandled(ctx, m.GetOrganizationId(), Source, scanners.AsyncScanEngineReal, outcome, scanners.AsyncShadowGateReasonNotGated)
	return nil
}

// judgeMessage maps the request onto the judge message the analyzer renders,
// returning the harness tool call ids alongside so the prompt shows the real
// ids: one per tool call for multi-call messages, or the request's tool call
// id for a single tool request carried in tool_name and body.
func judgeMessage(m *riskv1.LLMAnalysis) (judgemessage.Message, []string) {
	calls := m.GetToolCalls()
	if len(calls) == 0 {
		msg := judgemessage.New(m.GetMessageType(), m.GetToolName(), m.GetBody())
		if id := m.GetToolCallId(); id != "" {
			return msg, []string{id}
		}
		return msg, nil
	}

	judgeCalls := make([]judgemessage.ToolCall, 0, len(calls))
	ids := make([]string, 0, len(calls))
	for _, call := range calls {
		judgeCalls = append(judgeCalls, judgemessage.NewToolCall(call.GetName(), call.GetArguments()))
		ids = append(ids, call.GetId())
	}
	return judgemessage.NewForToolCalls(judgeCalls), ids
}
