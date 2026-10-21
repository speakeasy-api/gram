package customruleanalyzer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	enforce "github.com/speakeasy-api/gram/server/internal/scanners/enforcement"
)

const (
	// DefaultEnforcementMaxRequestAge leaves transport slack around the
	// five-second dispatcher wait while staying below the ack deadline.
	DefaultEnforcementMaxRequestAge = 8 * time.Second
	// DefaultEnforcementMaxContentBytes matches the maximum published content
	// budget and protects direct-to-topic requests from unbounded evaluation.
	DefaultEnforcementMaxContentBytes = 1 * 1024 * 1024
)

type enforcementScanner interface {
	Scan(context.Context, ScanRequest) (scanners.Result, error)
}

// EnforceHandlerConfig controls realtime custom-rule request bounds.
type EnforceHandlerConfig struct {
	MaxRequestAge   time.Duration
	MaxContentBytes int
}

// EnforceHandler evaluates one custom-rules lane and writes its reply.
type EnforceHandler struct {
	logger          *slog.Logger
	scanner         enforcementScanner
	writer          requestreply.ReplyBroker[*riskv1.EnforcementReply]
	riskRecorder    *metering.RiskRecorder
	truncations     metric.Int64Counter
	consumerID      string
	maxRequestAge   time.Duration
	maxContentBytes int
}

// NewEnforceHandler builds the custom-rules enforcement handler.
func NewEnforceHandler(logger *slog.Logger, meterProvider metric.MeterProvider, scanner enforcementScanner, writer requestreply.ReplyBroker[*riskv1.EnforcementReply], riskRecorder *metering.RiskRecorder, cfg EnforceHandlerConfig) (*EnforceHandler, error) {
	if scanner == nil {
		return nil, errors.New("custom rules enforcement scanner is required")
	}
	if writer == nil {
		return nil, errors.New("custom rules enforcement reply writer is required")
	}
	if riskRecorder == nil {
		return nil, errors.New("custom rules enforcement risk recorder is required")
	}
	if cfg.MaxRequestAge <= 0 {
		cfg.MaxRequestAge = DefaultEnforcementMaxRequestAge
	}
	if cfg.MaxContentBytes <= 0 {
		cfg.MaxContentBytes = DefaultEnforcementMaxContentBytes
	}
	return &EnforceHandler{
		logger:          logger.With(attr.SlogComponent("custom-rules-enforcer")),
		scanner:         scanner,
		writer:          writer,
		truncations:     enforce.NewTruncationCounter(meterProvider),
		riskRecorder:    riskRecorder,
		consumerID:      uuid.NewString(),
		maxRequestAge:   cfg.MaxRequestAge,
		maxContentBytes: cfg.MaxContentBytes,
	}, nil
}

// Handle ACKs answered and stale requests. Malformed requests are retried.
func (h *EnforceHandler) Handle(ctx context.Context, m *riskv1.CustomRulesEnforcement, meta gcp.MessageMetadata) error {
	replyURN := enforce.ReplyURN(meta.Attributes)
	correlationID, stale, err := enforce.ValidateRequest(enforce.Request{
		CreatedAt: m.GetCreatedAt(), OrganizationID: m.GetOrganizationId(), ProjectID: m.GetProjectId(), ReplyURN: replyURN, MaxRequestAge: h.maxRequestAge,
	})
	if err != nil {
		return fmt.Errorf("validate custom rules enforcement request: %w", err)
	}
	projectID, err := uuid.Parse(m.GetProjectId())
	if err != nil {
		return errors.New("custom rules enforcement project id is invalid")
	}
	if stale {
		return nil
	}

	content := m.GetContent()
	toolCalls := make([]ScanToolCall, 0, len(m.GetToolCalls()))
	argumentFields := make([]string, len(m.GetToolCalls()))
	fieldPointers := make([]*string, 0, len(argumentFields)+1)
	fieldPointers = append(fieldPointers, &content)
	for i, call := range m.GetToolCalls() {
		argumentFields[i] = call.GetArguments()
		fieldPointers = append(fieldPointers, &argumentFields[i])
	}
	contentCut := enforce.TruncateFields(h.maxContentBytes, fieldPointers...)
	truncated := m.GetContentTruncated() || contentCut
	if contentCut {
		enforce.RecordTruncation(ctx, h.truncations, "custom_rules")
	}
	for i, call := range m.GetToolCalls() {
		toolCalls = append(toolCalls, ScanToolCall{Name: call.GetName(), Arguments: argumentFields[i]})
	}

	started := time.Now().UTC()
	result, scanErr := h.scanner.Scan(ctx, ScanRequest{
		ProjectID: projectID, CustomRuleIDs: m.GetCustomRuleIds(), Content: content, Kind: m.GetMessageType(), ToolCalls: toolCalls,
	})
	status := riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK
	reason := ""
	if scanErr != nil {
		status = riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR
		reason = "custom rules scan failed"
	}
	reply := riskv1.EnforcementReply_builder{
		CorrelationId: new(correlationID),
		Scanner:       new(riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_CUSTOM_RULES),
		Status:        new(status),
		Reason:        new(reason),
		Findings:      enforce.Findings(result.Findings),
		Diagnostics: riskv1.EnforcementDiagnostics_builder{
			ScanDurationMs:  new(time.Since(started).Milliseconds()),
			ConsumerId:      new(h.consumerID),
			DeliveryAttempt: new(conv.SafeInt32(conv.PtrValOr(meta.DeliveryAttempt, 0))),
		}.Build(),
		PolicyId:         new(""),
		ContentTruncated: new(truncated),
		Incomplete:       new(scanErr != nil || !result.Completed),
	}.Build()
	if replyErr := h.writer.Reply(ctx, replyURN, reply); replyErr != nil {
		h.logger.ErrorContext(ctx, "write custom rules enforcement reply; acknowledging request", attr.SlogError(replyErr))
	}

	if result.Completed {
		provenance, provenanceErr := scanners.ParseRiskProvenance(m, m.GetMessageType(), "realtime_streams")
		if provenanceErr != nil {
			h.logger.WarnContext(ctx, "skipping custom rules enforcement usage with invalid attribution", attr.SlogError(provenanceErr))
		} else if recordErr := h.riskRecorder.Record(ctx, metering.RiskCustomRules(), provenance, result.STokens, started); recordErr != nil {
			h.logger.ErrorContext(ctx, "record custom rules enforcement usage", attr.SlogError(recordErr))
		}
	}
	return nil
}
