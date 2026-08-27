package gitleaks

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
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/risk/maskdisplay"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

const (
	// DefaultMaxRequestAge is the maximum useful age of an inline scan request.
	DefaultMaxRequestAge = 30 * time.Second
	maxReplyReasonRunes  = 256
)

// FingerprintFinding returns a canonically encoded, tenant-scoped fingerprint.
// Production callers must adapt risk.Fingerprinter.TenantedHS256 and
// risk.EncodeFingerprint here; the callback avoids a package import cycle.
type FingerprintFinding func(tenantID string, message []byte) (string, error)

// EnforceHandlerConfig controls inline request freshness.
type EnforceHandlerConfig struct {
	// MaxRequestAge drops requests that can no longer satisfy an inline caller.
	MaxRequestAge time.Duration
}

// EnforceHandler scans one inline request and writes a safe correlated reply.
type EnforceHandler struct {
	logger        *slog.Logger
	writer        requestreply.ReplyBroker[*riskv1.EnforcementReply]
	scanner       *Scanner
	fingerprint   FingerprintFinding
	metrics       enforceHandlerMetrics
	consumerID    string
	maxRequestAge time.Duration
}

// NewEnforceHandler builds the gitleaks enforcement subscription handler.
func NewEnforceHandler(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	writer requestreply.ReplyBroker[*riskv1.EnforcementReply],
	fingerprint FingerprintFinding,
	cfg EnforceHandlerConfig,
) (*EnforceHandler, error) {
	if writer == nil {
		return nil, errors.New("gitleaks enforcement reply writer is required")
	}
	if fingerprint == nil {
		return nil, errors.New("gitleaks enforcement fingerprint function is required")
	}
	if cfg.MaxRequestAge <= 0 {
		cfg.MaxRequestAge = DefaultMaxRequestAge
	}
	metrics, err := newEnforceHandlerMetrics(meterProvider)
	if err != nil {
		return nil, err
	}
	return &EnforceHandler{
		logger:        logger.With(attr.SlogComponent("gitleaks-enforcer")),
		writer:        writer,
		scanner:       NewScanner(),
		fingerprint:   fingerprint,
		metrics:       metrics,
		consumerID:    uuid.NewString(),
		maxRequestAge: cfg.MaxRequestAge,
	}, nil
}

// Handle ACKs completed and stale scans. Malformed requests return an error so
// the restricted forensic DLQ retains evidence that could not be interpreted.
func (h *EnforceHandler) Handle(ctx context.Context, m *riskv1.GitleaksEnforcement, meta gcp.MessageMetadata) error {
	createdAt, err := time.Parse(time.RFC3339Nano, m.GetCreatedAt())
	if err != nil {
		return fmt.Errorf("parse enforcement created_at: %w", err)
	}
	if time.Since(createdAt) > h.maxRequestAge {
		h.metrics.staleDropped.Add(ctx, 1)
		return nil
	}
	if m.GetOrganizationId() == "" {
		return errors.New("enforcement organization id is required")
	}
	if m.GetProjectId() == "" {
		return errors.New("enforcement project id is required")
	}
	replyURN := meta.Attributes[requestreply.ReplyURNAttribute]
	if replyURN == "" {
		return errors.New("enforcement reply urn attribute is required")
	}
	_, correlationID, err := enforcereply.ParseReplyURN(replyURN)
	if err != nil {
		return fmt.Errorf("parse enforcement reply urn: %w", err)
	}

	started := time.Now()
	var findings []scanners.Finding
	status := riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK
	reason := ""
	if len(m.GetContent()) > enforcereply.MaxContentBytes {
		// Dispatcher enforces this budget too; a request that bypasses it must
		// not buy an unbounded scan.
		status = riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR
		reason = fmt.Sprintf("enforcement content is %d bytes; maximum is %d bytes", len(m.GetContent()), enforcereply.MaxContentBytes)
	} else {
		var scanErr error
		findings, scanErr = h.scanner.Scan(ctx, m.GetContent())
		if scanErr != nil {
			status = riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR
			reason = scanErr.Error()
		}
	}

	replyFindings := make([]*riskv1.EnforcementFinding, 0, len(findings))
	for _, finding := range findings {
		fingerprint, fingerprintErr := h.fingerprint(m.GetOrganizationId(), []byte(finding.Match))
		if fingerprintErr != nil {
			h.logger.ErrorContext(ctx, "fingerprint gitleaks enforcement finding", attr.SlogError(fingerprintErr))
			status = riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR
			reason = "fingerprint enforcement finding"
			replyFindings = nil
			break
		}
		replyFindings = append(replyFindings, riskv1.EnforcementFinding_builder{
			RuleId:        new(finding.RuleID),
			Category:      new(string(categories.Classify(finding.Source, finding.RuleID))),
			Score:         new(finding.Confidence),
			StartPos:      new(conv.SafeInt32(finding.StartPos)),
			EndPos:        new(conv.SafeInt32(finding.EndPos)),
			Surface:       new(scanners.FindingSurface(finding.Source, finding.Field, finding.Path)),
			Field:         new(finding.Field),
			Path:          new(finding.Path),
			ToolCallId:    new(finding.McpLookupToolCallID),
			MaskedPreview: new(maskdisplay.Display(finding.Source, finding.RuleID, finding.Match)),
			Fingerprint:   new(fingerprint),
		}.Build())
	}

	deliveryAttempt := int32(0)
	if meta.DeliveryAttempt != nil {
		deliveryAttempt = conv.SafeInt32(*meta.DeliveryAttempt)
	}
	reply := riskv1.EnforcementReply_builder{
		CorrelationId: new(correlationID),
		Scanner:       new(riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS),
		Status:        new(status),
		Reason:        new(conv.TruncateString(reason, maxReplyReasonRunes)),
		Findings:      replyFindings,
		Diagnostics: riskv1.EnforcementDiagnostics_builder{
			ScanDurationMs:  new(time.Since(started).Milliseconds()),
			ConsumerId:      new(h.consumerID),
			DeliveryAttempt: new(deliveryAttempt),
		}.Build(),
	}.Build()
	if err := h.writer.Reply(ctx, replyURN, reply); err != nil {
		h.metrics.replyWriteErrors.Add(ctx, 1)
		h.logger.ErrorContext(ctx, "write gitleaks enforcement reply; acknowledging request", attr.SlogError(err))
		return nil
	}

	h.logger.DebugContext(ctx, "gitleaks enforcement scan complete", attr.SlogValueAny(map[string]any{
		"request_id": m.GetRequestId(),
		"detections": len(findings),
		"status":     status.String(),
	}))
	return nil
}
