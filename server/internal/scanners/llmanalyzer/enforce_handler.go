package llmanalyzer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

const (
	// LaneSync is the Request.Lane of realtime enforcement requests.
	LaneSync = "sync"

	// DefaultMaxRequestAge is the maximum useful age of an inline scan request.
	// It matches the dispatcher's per-lane wait: an older request has already
	// been given up on, so analyzing it would only spend model budget.
	DefaultMaxRequestAge = 30 * time.Second

	// maxReplyReasonRunes bounds EnforcementReply.reason as the proto documents.
	maxReplyReasonRunes = 256

	// maxReplyDescriptionRunes bounds EnforcementFinding.description as the
	// proto documents. ParseVerdict already caps reasoning at the same length;
	// this guards the wire contract independently of the parser.
	maxReplyDescriptionRunes = 500
)

// EnforceHandlerOption customizes an EnforceHandler.
type EnforceHandlerOption func(*EnforceHandler)

// WithMaxRequestAge overrides the freshness window. Non-positive values keep
// DefaultMaxRequestAge.
func WithMaxRequestAge(maxRequestAge time.Duration) EnforceHandlerOption {
	return func(h *EnforceHandler) {
		if maxRequestAge > 0 {
			h.maxRequestAge = maxRequestAge
		}
	}
}

// WithRiskRecorder meters the content of completed analyses under
// metering.RiskLLMAnalyzer, as the gitleaks enforcer does for its lane. Without
// it the handler answers requests but records no usage.
func WithRiskRecorder(recorder *metering.RiskRecorder) EnforceHandlerOption {
	return func(h *EnforceHandler) {
		h.riskRecorder = recorder
	}
}

// EnforceHandler consumes the LLM enforcement lane: it analyzes one inline
// request with the fine-tuned risk model and writes a correlated
// EnforcementReply to the requester's Redis inbox.
//
// Failure semantics mirror the gitleaks enforcer. Model failures and reply
// write failures ACK the request because redelivery cannot rescue an inline
// scan whose requester has already given up; the reply carries an ERROR (or
// DEAD_LETTER when no model is configured) so the waiter returns immediately
// and fails closed instead of running out its budget. Only requests the
// handler cannot interpret return an error, so the forensic dead-letter topic
// retains them.
type EnforceHandler struct {
	logger        *slog.Logger
	tracer        trace.Tracer
	analyzer      *Analyzer
	writer        requestreply.ReplyBroker[*riskv1.EnforcementReply]
	metrics       enforceHandlerMetrics
	riskRecorder  *metering.RiskRecorder
	consumerID    string
	maxRequestAge time.Duration
}

// NewEnforceHandler builds the LLM enforcement subscription handler. The
// analyzer may be disabled (built without a completer); the handler then
// answers every request with a DEAD_LETTER reply.
func NewEnforceHandler(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	analyzer *Analyzer,
	writer requestreply.ReplyBroker[*riskv1.EnforcementReply],
	opts ...EnforceHandlerOption,
) *EnforceHandler {
	logger = logger.With(attr.SlogComponent("risk-llm-enforcer"))
	h := &EnforceHandler{
		logger:        logger,
		tracer:        tracerProvider.Tracer(tracerName),
		analyzer:      analyzer,
		writer:        writer,
		metrics:       newEnforceHandlerMetrics(meterProvider, logger),
		riskRecorder:  nil,
		consumerID:    uuid.NewString(),
		maxRequestAge: DefaultMaxRequestAge,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Handle ACKs answered and stale requests. Malformed requests return an error
// so the restricted forensic DLQ retains evidence that could not be
// interpreted.
func (h *EnforceHandler) Handle(ctx context.Context, m *riskv1.LLMEnforcement, meta gcp.MessageMetadata) error {
	createdAt, err := time.Parse(time.RFC3339Nano, m.GetCreatedAt())
	if err != nil {
		return fmt.Errorf("parse llm enforcement created_at: %w", err)
	}
	if m.GetOrganizationId() == "" {
		return errors.New("llm enforcement organization id is required")
	}
	if m.GetProjectId() == "" {
		return errors.New("llm enforcement project id is required")
	}
	replyURN := meta.Attributes[requestreply.ReplyURNAttribute]
	if replyURN == "" {
		return errors.New("llm enforcement reply urn attribute is required")
	}
	_, correlationID, err := enforcereply.ParseReplyURN(replyURN)
	if err != nil {
		return fmt.Errorf("parse llm enforcement reply urn: %w", err)
	}
	// Structural validation first: a stale AND malformed request belongs in the
	// forensic DLQ, not in a silent drop. Symmetric window: a far-future stamp
	// is as suspect as a stale one.
	if age := time.Since(createdAt); age > h.maxRequestAge || age < -h.maxRequestAge {
		h.metrics.recordStaleDropped(ctx)
		return nil
	}

	ctx, span := h.tracer.Start(ctx, "risk.llm.enforce", trace.WithAttributes(
		attr.OrganizationID(m.GetOrganizationId()),
		attr.OrganizationSlug(m.GetOrganizationSlug()),
		attr.ProjectID(m.GetProjectId()),
		attr.RiskLane(LaneSync),
	))
	defer span.End()

	started := time.Now().UTC()
	status := riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK
	reason := ""
	var findings []scanners.Finding
	var analysis Analysis
	if !h.analyzer.Enabled() {
		status = riskv1.EnforcementStatus_ENFORCEMENT_STATUS_DEAD_LETTER
		reason = ReasonDisabled
	} else {
		msg, toolCallIDs := enforcementMessage(m)
		analysis = h.analyzer.Analyze(ctx, Request{
			OrgID:       m.GetOrganizationId(),
			OrgSlug:     m.GetOrganizationSlug(),
			ProjectID:   m.GetProjectId(),
			Lane:        LaneSync,
			Message:     msg,
			ToolCallIDs: toolCallIDs,
		})
		if analysis.Err != nil {
			status = riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR
			reason = replyReason(analysis.Err)
		} else {
			findings = analysis.Result.Findings
		}
	}

	replyFindings := make([]*riskv1.EnforcementFinding, 0, len(findings))
	for _, finding := range findings {
		if IsDeadLetter(finding) {
			// Analyze reports failures through Err; a sentinel here would be a
			// contract break and must not masquerade as a positive finding.
			continue
		}
		category := ""
		if len(finding.Tags) > 0 {
			category = finding.Tags[0]
		}
		if category == "" {
			category = string(categories.Classify(finding.Source, finding.RuleID))
		}
		replyFindings = append(replyFindings, riskv1.EnforcementFinding_builder{
			RuleId:        new(finding.RuleID),
			Category:      new(category),
			Score:         new(finding.Confidence),
			StartPos:      new(int32(0)),
			EndPos:        new(int32(0)),
			Surface:       new(scanners.SurfaceNone),
			Field:         new(""),
			Path:          new(""),
			ToolCallId:    new(""),
			MaskedPreview: new(""),
			Fingerprint:   new(""),
			Description:   new(conv.TruncateString(finding.Description, maxReplyDescriptionRunes)),
		}.Build())
	}

	deliveryAttempt := int32(0)
	if meta.DeliveryAttempt != nil {
		deliveryAttempt = conv.SafeInt32(*meta.DeliveryAttempt)
	}
	reply := riskv1.EnforcementReply_builder{
		CorrelationId: new(correlationID),
		Scanner:       new(riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER),
		Status:        new(status),
		Reason:        new(conv.TruncateString(reason, maxReplyReasonRunes)),
		Findings:      replyFindings,
		Diagnostics: riskv1.EnforcementDiagnostics_builder{
			ScanDurationMs:  new(time.Since(started).Milliseconds()),
			ConsumerId:      new(h.consumerID),
			DeliveryAttempt: new(deliveryAttempt),
		}.Build(),
		PolicyId: new(""),
	}.Build()

	outcome := enforceOutcome(status)
	span.SetAttributes(
		attr.Outcome(outcome),
		attribute.Int("gram.risk.llm.finding_count", len(replyFindings)),
	)
	h.metrics.recordRequest(ctx, outcome)

	replyErr := h.writer.Reply(ctx, replyURN, reply)
	if replyErr != nil {
		h.metrics.recordReplyWriteError(ctx)
		h.logger.ErrorContext(ctx, "write llm enforcement reply; acknowledging request",
			attr.SlogError(replyErr),
			attr.SlogOrganizationID(m.GetOrganizationId()),
			attr.SlogProjectID(m.GetProjectId()),
		)
	}

	// The model was consulted whether or not the reply landed, so usage is
	// metered after the reply attempt rather than gated on it.
	if h.riskRecorder != nil && analysis.Result.Completed {
		provenance, provenanceErr := scanners.ParseRiskProvenance(m, m.GetMessageType(), "realtime_streams")
		if provenanceErr != nil {
			h.logger.WarnContext(ctx, "skipping llm enforcement usage with invalid attribution", attr.SlogError(provenanceErr))
		} else {
			// Separate realtime requests stay distinct even when linked to one
			// message, matching the gitleaks enforcer.
			policyID := ""
			if provenance.RiskPolicyVersion > 0 {
				policyID = provenance.RiskPolicyID.String()
			}
			provenance.OperationID = scanners.AsyncRiskOperationID(provenance.ExecutionPath, policyID, provenance.RiskPolicyVersion, "", "", m.GetRequestId())
			provenance.Model = analysis.Completion.Model
			provenance.Provider = Provider
			if meterErr := h.riskRecorder.Record(ctx, metering.RiskLLMAnalyzer(), provenance, analysis.Result.STokens, started); meterErr != nil {
				h.logger.ErrorContext(ctx, "record llm enforcement usage", attr.SlogError(meterErr))
			}
		}
	}
	if replyErr != nil {
		return nil
	}

	h.logger.DebugContext(ctx, "llm enforcement analysis complete", attr.SlogValueAny(map[string]any{
		"request_id": m.GetRequestId(),
		"findings":   len(replyFindings),
		"status":     status.String(),
	}))
	return nil
}

// enforcementMessage rebuilds the judge message the dispatcher flattened into
// the request, together with the harness tool call ids so the prompt shows
// the model the ids the agent actually used. Multi-call requests render every
// call; a single tool request renders the tool name and body as one call.
func enforcementMessage(m *riskv1.LLMEnforcement) (judgemessage.Message, []string) {
	if calls := m.GetToolCalls(); len(calls) > 0 {
		toolCalls := make([]judgemessage.ToolCall, 0, len(calls))
		ids := make([]string, 0, len(calls))
		for _, call := range calls {
			toolCalls = append(toolCalls, judgemessage.NewToolCall(call.GetName(), call.GetArguments()))
			ids = append(ids, call.GetId())
		}
		return judgemessage.NewForToolCalls(toolCalls), ids
	}

	msg := judgemessage.New(m.GetMessageType(), m.GetToolName(), m.GetBody())
	if msg.Type == message.ToolRequest && msg.ToolName != "" && m.GetToolCallId() != "" {
		return msg, []string{m.GetToolCallId()}
	}
	return msg, nil
}

// replyReason renders an analyzer failure for EnforcementReply.reason. The
// classification leads so the dispatcher can act on it without parsing free
// text; the error text follows for operators. An upstream body snippet is
// upstream-controlled and log-only, so it never travels in the reply.
func replyReason(err error) string {
	text := err.Error()
	if upstream, ok := errors.AsType[*UpstreamError](err); ok && upstream.Body != "" {
		text = (&UpstreamError{Status: upstream.Status, Body: ""}).Error()
	}
	return DeadLetterReason(err) + ": " + text
}

func enforceOutcome(status riskv1.EnforcementStatus) o11y.Outcome {
	switch status {
	case riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK:
		return EnforceOutcomeOK
	case riskv1.EnforcementStatus_ENFORCEMENT_STATUS_DEAD_LETTER:
		return EnforceOutcomeDeadLetter
	case riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_UNSPECIFIED:
		return EnforceOutcomeError
	default:
		return EnforceOutcomeError
	}
}
