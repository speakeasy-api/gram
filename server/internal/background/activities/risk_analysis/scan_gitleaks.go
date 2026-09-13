package risk_analysis

import (
	"context"
	"fmt"
	"time"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func (a *AnalyzeBatch) scanGitleaks(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, contents []string) ([][]scanners.Finding, error) {
	if err := a.publishGitleaksScanRequests(ctx, args, messages); err != nil {
		return nil, err
	}

	startedAt := time.Now().UTC()
	results, err := a.gitleaksScanner.ScanBatch(ctx, contents)
	a.recordBatchResults(ctx, metering.RiskGitleaks(), args, messages, results, startedAt)
	if err != nil {
		return findingsFromResults(results), fmt.Errorf("scan gitleaks batch: %w", err)
	}
	return findingsFromResults(results), nil
}

func (a *AnalyzeBatch) publishGitleaksScanRequests(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(messages))
	requestID := batchScanRequestID(args, "standard")
	for _, msg := range messages {
		chatMessageID, contentPartID := msg.anchorIDStrings()
		provenance := batchRiskProvenance(args, msg, shadowStreamExecutionPath, requestID.String())
		content := msg.scanSurface()
		findingSurface := scanners.SurfaceContent
		if content != msg.Content {
			findingSurface = "scan_surface"
		}
		publishResults = append(publishResults, a.gitleaksPub.Publish(ctx, riskv1.GitleaksAnalysis_builder{
			RequestId:               new(requestID.String()),
			ChatMessageId:           chatMessageID,
			ContentPartId:           contentPartID,
			ProjectId:               new(args.ProjectID.String()),
			OrganizationId:          &args.OrganizationID,
			RiskPolicyId:            new(args.RiskPolicyID.String()),
			RiskPolicyVersion:       &args.PolicyVersion,
			CreatedAt:               &createdAt,
			ChatId:                  nilUUIDString(msg.ChatID),
			ParentChatMessageId:     nilUUIDString(msg.ParentChatMessageID),
			OriginRiskPolicyId:      new(args.RiskPolicyID.String()),
			OriginRiskPolicyVersion: &args.PolicyVersion,
			MessageLinkReason:       &provenance.MessageLinkReason,
			ExecutionPath:           new(shadowStreamExecutionPath),
			ToolCallId:              &provenance.ToolCallID,
			ToolName:                &provenance.ToolName,
			HookSource:              &msg.Source,
			UserId:                  &msg.UserID,
			MessageType:             new(msg.Type),
			FindingSurface:          &findingSurface,

			ReplyUrn: nil,
			Content:  &content,
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish gitleaks scan requests", publishResults)
}
