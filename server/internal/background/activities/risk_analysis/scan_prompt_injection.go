package risk_analysis

import (
	"context"
	"time"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

// publishPromptInjectionScanRequests publishes one request per message for
// the handler to run the prompt-injection cascade and publish its findings.
func (a *AnalyzeBatch) publishPromptInjectionScanRequests(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(messages))
	executionPath := judgeHandlerStreamExecutionPath
	for _, msg := range messages {
		requestID := messageScanRequestID(args, scanLaneStandard, msg)
		provenance := batchRiskProvenance(args, msg, executionPath, requestID)
		chatMessageID, contentPartID := msg.anchorIDStrings()
		jm := batchJudgeMessage(msg)
		toolCalls := make([]*riskv1.PromptInjectionAnalysis_ToolCall, 0, len(jm.ToolCalls))
		for _, call := range jm.ToolCalls {
			toolCalls = append(toolCalls, riskv1.PromptInjectionAnalysis_ToolCall_builder{
				Name:      &call.ToolName,
				Arguments: &call.Arguments,
			}.Build())
		}

		publishResults = append(publishResults, a.promptInjectionPub.Publish(ctx, riskv1.PromptInjectionAnalysis_builder{
			RequestId:               &requestID,
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
			ExecutionPath:           &executionPath,
			ToolCallId:              &provenance.ToolCallID,
			HookSource:              &msg.Source,

			Content:                new(msg.Content),
			UserId:                 &msg.UserID,
			L1Enabled:              new(true),
			MessageType:            new(jm.Type),
			Body:                   &jm.Body,
			ToolName:               &jm.ToolName,
			ToolCalls:              toolCalls,
			PriorUserRequest:       &msg.PriorUserRequest,
			RecentUntrustedContent: &msg.RecentUntrustedContent,
			RunJudge:               new(true),
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish prompt injection scan requests", publishResults)
}
