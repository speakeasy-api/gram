package risk_analysis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

func (a *AnalyzeBatch) scanGitleaks(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage, contents []string) ([][]Finding, error) {
	a.publishGitleaksScanRequests(ctx, args, requestID, messages)

	results, err := a.scanner.ScanBatchParallel(contents)
	if err != nil {
		return [][]Finding{}, fmt.Errorf("scan batch parallel: %w", err)
	}

	findings := make([][]Finding, len(results))
	for i, detections := range results {
		findings[i] = fromDetections(detections)
	}
	return findings, nil
}

func (a *AnalyzeBatch) publishGitleaksScanRequests(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, len(messages))
	for i, msg := range messages {
		publishResults[i] = a.gitleaksPub.Publish(ctx, riskv1.GitleaksAnalysis_builder{
			RequestId:         new(requestID.String()),
			ChatMessageId:     new(msg.ID.String()),
			ProjectId:         new(args.ProjectID.String()),
			OrganizationId:    &args.OrganizationID,
			RiskPolicyId:      new(args.RiskPolicyID.String()),
			RiskPolicyVersion: &args.PolicyVersion,
			CreatedAt:         &createdAt,

			ReplyUrn: nil,
			Content:  new(msg.Content),
		}.Build())
	}
	drainPublishAcks(ctx, a.logger, "failed to publish gitleaks scan request", publishResults)
}
