package risk_analysis

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

// dispatchPromptInjection hands the batch's prompt-injection scanning to the
// streams consumer. The activity holds no judge of its own: it publishes one
// PromptInjectionAnalysis per in-scope message and the consumer runs the
// classifier, publishes the findings onto the shared findings topic and meters
// the usage. See publishPromptInjectionScanRequests for why the publish must
// succeed for the batch to succeed.
func (a *AnalyzeBatch) dispatchPromptInjection(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage) error {
	if err := a.publishPromptInjectionScanRequests(ctx, args, messages); err != nil {
		return err
	}
	activity.RecordHeartbeat(ctx, SourcePromptInjection)
	return nil
}

// publishPromptInjectionScanRequests emits one analysis request per message.
// The publish is the whole scan for this source, so a failure fails the
// activity and Temporal redrives the batch: the request ids are deterministic
// over the batch identity and the consumer's finding ids derive from them, so
// a redrive republishes under the same ids instead of duplicating findings.
func (a *AnalyzeBatch) publishPromptInjectionScanRequests(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(messages))
	requestID := batchScanRequestID(args, "standard")
	for _, msg := range messages {
		provenance := batchRiskProvenance(args, msg, promptInjectionStreamExecutionPath, requestID.String())
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
			ExecutionPath:           new(promptInjectionStreamExecutionPath),
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
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish prompt injection scan requests", publishResults)
}
