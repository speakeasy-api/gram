package scanners

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

type FindingMetadata struct {
	RequestID         string
	ChatMessageID     string
	ProjectID         string
	OrganizationID    string
	RiskPolicyID      string
	RiskPolicyVersion int64
}

func PublishFindings(ctx context.Context, logger *slog.Logger, pub gcp.Publisher[*riskv1.Finding], meta FindingMetadata, findings []Finding, logPrefix string) (int, []string) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	results := make([]gcp.PublishResult, 0, len(findings))
	ruleIDs := make([]string, 0, len(findings))

	for _, finding := range findings {
		id, err := uuid.NewV7()
		if err != nil {
			logger.WarnContext(ctx, "failed to generate finding id", attr.SlogError(err))
			continue
		}

		startPos := conv.SafeInt32(finding.StartPos)
		endPos := conv.SafeInt32(finding.EndPos)
		msg := riskv1.Finding_builder{
			Id:                new(id.String()),
			RequestId:         &meta.RequestID,
			ChatMessageId:     &meta.ChatMessageID,
			ProjectId:         &meta.ProjectID,
			OrganizationId:    &meta.OrganizationID,
			RiskPolicyId:      &meta.RiskPolicyID,
			RiskPolicyVersion: &meta.RiskPolicyVersion,
			CreatedAt:         &createdAt,
			RuleId:            &finding.RuleID,
			Description:       &finding.Description,
			Match:             &finding.Match,
			StartPos:          &startPos,
			EndPos:            &endPos,
			Tags:              finding.Tags,
			Source:            &finding.Source,
			Confidence:        &finding.Confidence,
		}.Build()

		results = append(results, pub.Publish(ctx, msg))
		ruleIDs = append(ruleIDs, finding.RuleID)
	}

	published := 0
	for _, res := range results {
		if _, err := res.Get(ctx); err != nil {
			logger.WarnContext(ctx, "failed to publish "+logPrefix+" finding", attr.SlogError(err))
			continue
		}
		published++
	}

	return published, ruleIDs
}
