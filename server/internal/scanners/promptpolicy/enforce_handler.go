package promptpolicy

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
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	enforce "github.com/speakeasy-api/gram/server/internal/scanners/enforcement"
)

const (
	// DefaultEnforcementMaxRequestAge matches the dispatcher's lane wait so
	// requests cannot be scanned after the caller stops waiting.
	DefaultEnforcementMaxRequestAge = 15 * time.Second
	// DefaultEnforcementMaxContentBytes matches the published content budget;
	// only Presidio keeps a smaller engine limit.
	DefaultEnforcementMaxContentBytes = 1 * 1024 * 1024
)

type enforcementScanner interface {
	ScanWithVerdict(context.Context, string, string, string, string, Config, judgemessage.Message) (scanners.Result, *Verdict)
}

// EnforceHandlerConfig controls realtime prompt-policy request bounds.
type EnforceHandlerConfig struct {
	MaxRequestAge   time.Duration
	MaxContentBytes int
}

// EnforceHandler evaluates one policy-keyed judge lane and writes its reply.
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

// NewEnforceHandler builds the prompt-policy enforcement handler.
func NewEnforceHandler(logger *slog.Logger, meterProvider metric.MeterProvider, scanner enforcementScanner, writer requestreply.ReplyBroker[*riskv1.EnforcementReply], riskRecorder *metering.RiskRecorder, cfg EnforceHandlerConfig) (*EnforceHandler, error) {
	if scanner == nil {
		return nil, errors.New("prompt policy enforcement scanner is required")
	}
	if writer == nil {
		return nil, errors.New("prompt policy enforcement reply writer is required")
	}
	if riskRecorder == nil {
		return nil, errors.New("prompt policy enforcement risk recorder is required")
	}
	truncations, err := enforce.NewTruncationCounter(meterProvider)
	if err != nil {
		return nil, fmt.Errorf("create prompt policy enforcement truncation counter: %w", err)
	}
	if cfg.MaxRequestAge <= 0 {
		cfg.MaxRequestAge = DefaultEnforcementMaxRequestAge
	}
	if cfg.MaxContentBytes <= 0 {
		cfg.MaxContentBytes = DefaultEnforcementMaxContentBytes
	}
	return &EnforceHandler{
		logger:          logger.With(attr.SlogComponent("prompt-policy-enforcer")),
		scanner:         scanner,
		writer:          writer,
		truncations:     truncations,
		riskRecorder:    riskRecorder,
		consumerID:      uuid.NewString(),
		maxRequestAge:   cfg.MaxRequestAge,
		maxContentBytes: cfg.MaxContentBytes,
	}, nil
}

// Handle ACKs answered and stale requests. Malformed requests are retried.
func (h *EnforceHandler) Handle(ctx context.Context, m *riskv1.PromptPolicyEnforcement, meta gcp.MessageMetadata) error {
	replyURN := enforce.ReplyURN(meta.Attributes)
	correlationID, stale, err := enforce.ValidateRequest(enforce.Request{
		CreatedAt: m.GetCreatedAt(), OrganizationID: m.GetOrganizationId(), ProjectID: m.GetProjectId(), ReplyURN: replyURN, MaxRequestAge: h.maxRequestAge,
	})
	if err != nil {
		return fmt.Errorf("validate prompt policy enforcement request: %w", err)
	}
	if m.GetRiskPolicyId() == "" {
		return errors.New("prompt policy enforcement policy id is required")
	}
	if stale {
		return nil
	}

	body := conv.Default(m.GetBody(), m.GetContent())
	argumentFields := make([]string, len(m.GetToolCalls()))
	fields := make([]*string, 0, len(argumentFields)+1)
	if len(argumentFields) == 0 {
		fields = append(fields, &body)
	} else {
		for i, call := range m.GetToolCalls() {
			argumentFields[i] = call.GetArguments()
			fields = append(fields, &argumentFields[i])
		}
	}
	messageCut := enforce.TruncateFields(h.maxContentBytes, fields...)
	truncated := m.GetContentTruncated() || messageCut
	if messageCut {
		enforce.RecordTruncation(ctx, h.truncations, "judge")
	}
	var judgeMessage judgemessage.Message
	switch len(argumentFields) {
	case 0:
		judgeMessage = judgemessage.New(m.GetMessageType(), m.GetToolName(), body)
	case 1:
		judgeMessage = judgemessage.New(message.ToolRequest, m.GetToolCalls()[0].GetName(), argumentFields[0])
	default:
		calls := make([]judgemessage.ToolCall, 0, len(argumentFields))
		for i, call := range m.GetToolCalls() {
			calls = append(calls, judgemessage.NewToolCall(call.GetName(), argumentFields[i]))
		}
		judgeMessage = judgemessage.NewForToolCalls(calls)
	}

	started := time.Now().UTC()
	result, verdict := h.scanner.ScanWithVerdict(ctx, m.GetOrganizationId(), m.GetProjectId(), m.GetUserId(), m.GetPrompt(), ParseConfig(m.GetModelConfig()), judgeMessage)
	reply := riskv1.EnforcementReply_builder{
		CorrelationId: new(correlationID),
		Scanner:       new(riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_JUDGE),
		Status:        new(riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK),
		Reason:        new(""),
		Findings:      enforce.Findings(result.Findings),
		Diagnostics: riskv1.EnforcementDiagnostics_builder{
			ScanDurationMs:  new(time.Since(started).Milliseconds()),
			ConsumerId:      new(h.consumerID),
			DeliveryAttempt: new(conv.SafeInt32(conv.PtrValOr(meta.DeliveryAttempt, 0))),
		}.Build(),
		PolicyId:         new(m.GetRiskPolicyId()),
		ContentTruncated: new(truncated),
		Incomplete:       new(!result.Completed),
	}.Build()
	if replyErr := h.writer.Reply(ctx, replyURN, reply); replyErr != nil {
		h.logger.ErrorContext(ctx, "write prompt policy enforcement reply; acknowledging request", attr.SlogError(replyErr))
	}

	if result.Completed {
		provenance, provenanceErr := scanners.ParseRiskProvenance(m, m.GetMessageType(), "realtime_streams")
		if provenanceErr != nil {
			h.logger.WarnContext(ctx, "skipping prompt policy enforcement usage with invalid attribution", attr.SlogError(provenanceErr))
		} else {
			if verdict != nil {
				provenance.Model = verdict.Model
				provenance.Provider = verdict.Provider
			}
			if recordErr := h.riskRecorder.Record(ctx, metering.RiskPromptPolicy(), provenance, result.STokens, started); recordErr != nil {
				h.logger.ErrorContext(ctx, "record prompt policy enforcement usage", attr.SlogError(recordErr))
			}
		}
	}
	return nil
}
