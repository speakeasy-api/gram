package risk_analysis

import (
	"context"
	"time"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"

	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/scanners/destructivetool"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
)

func (a *AnalyzeBatch) publishToolCallScanRequests(
	ctx context.Context,
	args AnalyzeBatchArgs,
	messages []batchMessage,
	sources sourceSet,
	masks CategoryScopeMasks,
	destructiveEngineRuns bool,
	cliEngineRuns bool,
) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	results := make([]gcp.PublishResult, 0, len(messages))
	for i, msg := range messages {
		if len(msg.ToolCalls) == 0 {
			continue
		}

		var requestSources []string
		if sources.Has(shadowmcpscan.Source) && masks.AdmitsAny(i, sourceCategories[shadowmcpscan.Source]) {
			requestSources = append(requestSources, shadowmcpscan.Source)
		}
		if destructiveEngineRuns && sources.Has(destructivetool.Source) && masks.AdmitsAny(i, sourceCategories[destructivetool.Source]) {
			requestSources = append(requestSources, destructivetool.Source)
		}
		if cliEngineRuns && sources.Has(clidestructive.Source) && masks.AdmitsAny(i, sourceCategories[clidestructive.Source]) {
			requestSources = append(requestSources, clidestructive.Source)
		}
		if len(requestSources) == 0 {
			continue
		}

		toolCalls := make([]*riskv1.ToolCallAnalysis_ToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			callCreatedAt := msg.CreatedAt.Format(time.RFC3339Nano)
			toolCalls = append(toolCalls, riskv1.ToolCallAnalysis_ToolCall_builder{
				Id:        &call.ID,
				Name:      &call.Function.Name,
				Arguments: &call.Function.Arguments,
				CreatedAt: &callCreatedAt,
				Sender:    &msg.Source,
			}.Build())
		}

		requestID := messageScanRequestID(args, scanLaneStandard, msg)
		provenance := batchRiskProvenance(args, msg, shadowStreamExecutionPath, requestID)
		chatMessageID, contentPartID := msg.anchorIDStrings()
		results = append(results, a.toolCallPub.Publish(ctx, riskv1.ToolCallAnalysis_builder{
			RequestId:               &requestID,
			ChatMessageId:           chatMessageID,
			ContentPartId:           contentPartID,
			ProjectId:               new(args.ProjectID.String()),
			OrganizationId:          &args.OrganizationID,
			RiskPolicyId:            new(args.RiskPolicyID.String()),
			RiskPolicyVersion:       &args.PolicyVersion,
			CreatedAt:               &createdAt,
			ToolCalls:               toolCalls,
			Sources:                 requestSources,
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
			MessageType:             &msg.Type,
			PolicyLinkReason:        &provenance.PolicyLinkReason,
			ExternalConversationId:  &provenance.ExternalConversationID,
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish tool call scan requests", results)
}
