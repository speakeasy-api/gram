package risk_analysis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

func (a *AnalyzeBatch) scanPresidio(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage, contents []string) ([][]Finding, error) {
	scoreThreshold := resolvePresidioScoreThreshold(args.PresidioScoreThreshold)
	a.publishPresidioScanRequests(ctx, args, requestID, messages, scoreThreshold)

	results, err := a.piiScanner.AnalyzeBatch(ctx, contents, args.PresidioEntities, scoreThreshold, func() {
		activity.RecordHeartbeat(ctx, SourcePresidio)
	})
	if results == nil {
		results = make([][]Finding, len(messages))
	}
	if err != nil {
		a.logger.WarnContext(ctx, "presidio scan returned errors, using partial results", attr.SlogError(err))
		if a.metrics.presidioScanSkipped != nil {
			a.metrics.presidioScanSkipped.Add(ctx, 1)
		}
		err = fmt.Errorf("analyze batch: %w", err)
	}
	return results, err
}

func (a *AnalyzeBatch) publishPresidioScanRequests(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage, scoreThreshold float64) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, len(messages))
	for i, msg := range messages {
		publishResults[i] = a.presidioPub.Publish(ctx, riskv1.PresidioAnalysis_builder{
			RequestId:         new(requestID.String()),
			ChatMessageId:     new(msg.ID.String()),
			ProjectId:         new(args.ProjectID.String()),
			OrganizationId:    &args.OrganizationID,
			RiskPolicyId:      new(args.RiskPolicyID.String()),
			RiskPolicyVersion: &args.PolicyVersion,
			CreatedAt:         &createdAt,

			ReplyUrn:       nil,
			Content:        new(msg.Content),
			Entities:       args.PresidioEntities,
			ScoreThreshold: &scoreThreshold,
		}.Build())
	}
	drainPublishAcks(ctx, a.logger, "failed to publish presidio scan request", publishResults)
}
