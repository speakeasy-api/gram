package gitleaks

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// Handler consumes GitleaksAnalysis scan requests, runs gitleaks over the
// message content, and publishes a Finding per match into the shared Finding
// topic. This is the shadow-mode counterpart to the in-process gitleaks scan
// in the risk_analysis activity: nothing consumes the findings yet — the flow
// exists to exercise the async pipeline end to end.
type Handler struct {
	logger      *slog.Logger
	findingsPub gcp.Publisher[*riskv1.Finding]
	scanner     *Scanner
}

// NewHandler builds a gitleaks subscription handler. Its Scanner reuses a warm
// detector across messages (the subscriber processes one message per Handle,
// so it materializes a single detector), avoiding per-message rule compilation.
func NewHandler(logger *slog.Logger, findingsPub gcp.Publisher[*riskv1.Finding]) *Handler {
	return &Handler{
		logger:      logger.With(attr.SlogComponent("gitleaks-analyzer")),
		findingsPub: findingsPub,
		scanner:     NewScanner(),
	}
}

// Handle scans the request content and publishes one Finding per match. A scan
// failure is returned to the subscriber, which nacks the message for redelivery
// and eventual dead-lettering, so the failure stays visible rather than being
// silently acked. Per-finding id-generation and publish failures are best-effort:
// they are logged and skipped so a partial batch still publishes what it can.
func (h *Handler) Handle(ctx context.Context, m *riskv1.GitleaksAnalysis, _ gcp.MessageMetadata) error {
	findings, err := h.scanner.Scan(ctx, m.GetContent())
	if err != nil {
		return fmt.Errorf("gitleaks scan failed: %w", err)
	}

	// One timestamp for the batch: when the findings were detected, distinct
	// from the request's created_at.
	createdAt := time.Now().UTC().Format(time.RFC3339)

	// Issue every publish first so the Pub/Sub client can batch them, then drain
	// the futures — mirrors the publish-then-drain pattern in analyze_batch.go.
	results := make([]gcp.PublishResult, 0, len(findings))
	ruleIDs := make([]string, 0, len(findings))
	for _, f := range findings {
		id, err := uuid.NewV7()
		if err != nil {
			h.logger.WarnContext(ctx, "failed to generate finding id", attr.SlogError(err))
			continue
		}

		startPos := conv.SafeInt32(f.StartPos)
		endPos := conv.SafeInt32(f.EndPos)
		finding := riskv1.Finding_builder{
			Id:                new(id.String()),
			RequestId:         new(m.GetRequestId()),
			ChatMessageId:     new(m.GetChatMessageId()),
			ProjectId:         new(m.GetProjectId()),
			OrganizationId:    new(m.GetOrganizationId()),
			RiskPolicyId:      new(m.GetRiskPolicyId()),
			RiskPolicyVersion: new(m.GetRiskPolicyVersion()),
			CreatedAt:         &createdAt,
			RuleId:            &f.RuleID,
			Description:       &f.Description,
			Match:             &f.Match,
			StartPos:          &startPos,
			EndPos:            &endPos,
			Tags:              f.Tags,
			Source:            new(Source),
			Confidence:        &f.Confidence,
		}.Build()

		results = append(results, h.findingsPub.Publish(ctx, finding))
		ruleIDs = append(ruleIDs, f.RuleID)
	}

	published := 0
	for _, res := range results {
		if _, err := res.Get(ctx); err != nil {
			h.logger.WarnContext(ctx, "failed to publish gitleaks finding", attr.SlogError(err))
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

	return nil
}
