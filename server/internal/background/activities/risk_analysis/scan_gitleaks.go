package risk_analysis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func (a *AnalyzeBatch) scanGitleaks(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage, contents []string) ([][]scanners.Finding, error) {
	if err := a.publishGitleaksScanRequests(ctx, args, requestID, messages); err != nil {
		return nil, err
	}

	findings, err := a.gitleaksScanner.ScanBatch(ctx, contents)
	if err != nil {
		return [][]scanners.Finding{}, fmt.Errorf("scan gitleaks batch: %w", err)
	}

	return findings, nil
}

func (a *AnalyzeBatch) publishGitleaksScanRequests(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(messages))
	for _, msg := range messages {
		chatMessageID, contentPartID := msg.anchorIDStrings()
		publishResults = append(publishResults, a.gitleaksPub.Publish(ctx, riskv1.GitleaksAnalysis_builder{
			RequestId:         new(requestID.String()),
			ChatMessageId:     chatMessageID,
			ContentPartId:     contentPartID,
			ProjectId:         new(args.ProjectID.String()),
			OrganizationId:    &args.OrganizationID,
			RiskPolicyId:      new(args.RiskPolicyID.String()),
			RiskPolicyVersion: &args.PolicyVersion,
			CreatedAt:         &createdAt,

			ReplyUrn: nil,
			Content:  new(msg.Content),
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish gitleaks scan requests", publishResults)
}
