package risk_analysis

import (
	"context"
	"fmt"
	"time"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// publishAccountIdentityScanRequests publishes one request per chat in the
// batch, anchored on the chat's earliest in-batch message. The handler mints
// one finding id per chat, rule and policy version, so findings converge
// whichever message anchors the request.
func (a *AnalyzeBatch) publishAccountIdentityScanRequests(ctx context.Context, args AnalyzeBatchArgs) error {
	rows, err := repo.New(a.db).GetBatchChatIdentities(ctx, repo.GetBatchChatIdentitiesParams{
		Ids:               args.MessageIDs,
		ProjectID:         args.ProjectID,
		RiskPolicyID:      args.RiskPolicyID,
		RiskPolicyVersion: args.PolicyVersion,
	})
	if err != nil {
		return fmt.Errorf("get batch chat identities: %w", err)
	}
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(rows))
	for _, row := range rows {
		publishResults = append(publishResults, a.accountIdentityPub.Publish(ctx, riskv1.AccountIdentityAnalysis_builder{
			RequestId:         new(batchScanRequestID(args, scanLaneStandard, false, row.EarliestMessageID).String()),
			ChatMessageId:     new(row.EarliestMessageID.String()),
			ChatId:            new(row.ChatID.String()),
			ProjectId:         new(args.ProjectID.String()),
			OrganizationId:    &args.OrganizationID,
			RiskPolicyId:      new(args.RiskPolicyID.String()),
			RiskPolicyVersion: &args.PolicyVersion,
			CreatedAt:         &createdAt,
			AccountType:       new(conv.FromPGTextOrEmpty[string](row.AccountType)),
			Email:             new(conv.FromPGTextOrEmpty[string](row.Email)),
			ApprovedDomains:   args.ApprovedEmailDomains,
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish account identity scan requests", publishResults)
}
