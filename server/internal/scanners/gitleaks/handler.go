package gitleaks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// Handler consumes GitleaksAnalysis scan requests, runs gitleaks over the
// message content, and publishes a Finding per match into the shared Finding
// topic. This is the shadow-mode counterpart to the in-process gitleaks scan
// in the risk_analysis activity: nothing consumes the findings yet — the flow
// exists to exercise the async pipeline end to end.
type Handler struct {
	logger       *slog.Logger
	findingsPub  gcp.Publisher[*riskv1.Finding]
	scanner      *Scanner
	riskRecorder *metering.RiskRecorder
}

// NewHandler builds a gitleaks subscription handler. Its Scanner reuses a warm
// detector across messages (the subscriber processes one message per Handle,
// so it materializes a single detector), avoiding per-message rule compilation.
func NewHandler(logger *slog.Logger, findingsPub gcp.Publisher[*riskv1.Finding], riskRecorder *metering.RiskRecorder) *Handler {
	return &Handler{
		logger:       logger.With(attr.SlogComponent("gitleaks-analyzer")),
		findingsPub:  findingsPub,
		scanner:      NewScanner(),
		riskRecorder: riskRecorder,
	}
}

// Handle scans the request content and publishes one Finding per match. A
// scan failure OR a publish failure is returned to the subscriber, which
// nacks the message for redelivery — an acked request must mean every finding
// it produced is durably on the topic, since that topic feeds the ClickHouse
// findings store. Publishing goes through
// StartPublishFindings so ids are deterministic — a redelivered message
// republishes under the same ids (the already-published subset included)
// instead of duplicating ClickHouse rows — and the reveal metadata (surface
// et al.) is stamped uniformly.
func (h *Handler) Handle(ctx context.Context, m *riskv1.GitleaksAnalysis, _ gcp.MessageMetadata) error {
	startedAt := time.Now().UTC()
	result, err := h.scanner.Scan(ctx, m.GetContent())
	if err != nil {
		return fmt.Errorf("gitleaks scan failed: %w", err)
	}
	findings := result.Findings

	// Issue every publish first so the Pub/Sub client can batch them, then drain
	// the futures — mirrors the publish-then-drain pattern in analyze_batch.go.
	results, ruleIDs := scanners.StartPublishFindings(ctx, h.findingsPub, scanners.FindingMetadata{
		RequestID:         m.GetRequestId(),
		ChatMessageID:     m.GetChatMessageId(),
		ContentPartID:     m.GetContentPartId(),
		ProjectID:         m.GetProjectId(),
		OrganizationID:    m.GetOrganizationId(),
		RiskPolicyID:      m.GetRiskPolicyId(),
		RiskPolicyVersion: m.GetRiskPolicyVersion(),
		Surface:           requestSurface(m),
	}, findings)

	published := 0
	var publishErr error
	for _, res := range results {
		if _, err := res.Get(ctx); err != nil {
			publishErr = errors.Join(publishErr, err)
			continue
		}
		published++
	}

	// Never log matched values — they carry the secret. Counts and rule ids only.
	h.logger.InfoContext(ctx, "gitleaks scan complete", attr.SlogValueAny(map[string]any{
		"request_id":      m.GetRequestId(),
		"chat_message_id": m.GetChatMessageId(),
		"detections":      len(findings),
		"published":       published,
		"rule_ids":        ruleIDs,
	}))

	if publishErr != nil {
		publishErr = fmt.Errorf("publish gitleaks findings: %w", publishErr)
	}

	if result.Completed {
		provenance, provenanceErr := scanners.ParseRiskProvenance(m, m.GetMessageType(), "async")
		if provenanceErr != nil {
			h.logger.WarnContext(ctx, "skipping gitleaks usage with invalid attribution", attr.SlogError(provenanceErr))
		} else if err := h.riskRecorder.Record(ctx, metering.RiskGitleaks(), provenance, result.STokens, startedAt); err != nil {
			h.logger.ErrorContext(ctx, "record gitleaks usage", attr.SlogError(err))
		}
	}
	return publishErr
}

// requestSurface is the text the request's offsets index. Producers before
// finding_surface existed already sent tool requests over the composed scan
// surface, so an unmarked tool request still resolves to it.
func requestSurface(m *riskv1.GitleaksAnalysis) string {
	if surface := m.GetFindingSurface(); surface != "" {
		return surface
	}
	if m.GetMessageType() == message.ToolRequest {
		return "scan_surface"
	}
	return ""
}
