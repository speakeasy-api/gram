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
	scanner     *ReusableScanner
}

// NewHandler builds a gitleaks subscription handler. It pre-creates a reusable
// gitleaks scanner (one detector, reused and serialized across all messages).
func NewHandler(logger *slog.Logger, findingsPub gcp.Publisher[*riskv1.Finding]) (*Handler, error) {
	scanner, err := NewReusableScanner()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks scanner: %w", err)
	}

	return &Handler{
		logger:      logger.With(attr.SlogComponent("gitleaks-analyzer")),
		findingsPub: findingsPub,
		scanner:     scanner,
	}, nil
}

// Handle scans the request content and publishes one Finding per match. Errors
// are swallowed (logged, then the message is acked) so a single bad message
// cannot poison the subscription while we run in shadow mode.
func (h *Handler) Handle(ctx context.Context, m *riskv1.GitleaksAnalysis, _ gcp.MessageMetadata) error {
	detections := h.scanner.Scan(m.GetContent())

	// One timestamp for the batch: when the findings were detected, distinct
	// from the request's created_at.
	createdAt := time.Now().UTC().Format(time.RFC3339)

	// Issue every publish first so the Pub/Sub client can batch them, then drain
	// the futures — mirrors the publish-then-drain pattern in analyze_batch.go.
	results := make([]gcp.PublishResult, 0, len(detections))
	ruleIDs := make([]string, 0, len(detections))
	for _, d := range detections {
		id, err := uuid.NewV7()
		if err != nil {
			h.logger.WarnContext(ctx, "failed to generate finding id", attr.SlogError(err))
			continue
		}

		startPos := conv.SafeInt32(d.StartPos)
		endPos := conv.SafeInt32(d.EndPos)
		finding := riskv1.Finding_builder{
			Id:                new(id.String()),
			RequestId:         new(m.GetRequestId()),
			ChatMessageId:     new(m.GetChatMessageId()),
			ProjectId:         new(m.GetProjectId()),
			OrganizationId:    new(m.GetOrganizationId()),
			RiskPolicyId:      new(m.GetRiskPolicyId()),
			RiskPolicyVersion: new(m.GetRiskPolicyVersion()),
			CreatedAt:         &createdAt,
			RuleId:            &d.RuleID,
			Description:       &d.Description,
			Match:             &d.Match,
			StartPos:          &startPos,
			EndPos:            &endPos,
			Tags:              d.Tags,
			Source:            new(Source),
			Confidence:        &d.Confidence,
		}.Build()

		results = append(results, h.findingsPub.Publish(ctx, finding))
		ruleIDs = append(ruleIDs, d.RuleID)
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
		"detections":      len(detections),
		"published":       published,
		"rule_ids":        ruleIDs,
	}))

	return nil
}
