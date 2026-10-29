package risk_analysis

import (
	"context"
	"time"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

// publishCustomRulesScanRequests mirrors publishGitleaksScanRequests: it emits a
// CustomRulesAnalysis per message onto the async analyzer topic, carrying the
// full CEL input (content, kind, tool calls) and the selected rule ids so the
// subscriber can re-load and evaluate the same rules against the same message.
func (a *AnalyzeBatch) publishCustomRulesScanRequests(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, customRuleIDs []string) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(messages))
	for _, msg := range messages {
		requestID := messageScanRequestID(args, scanLaneStandard, msg)
		provenance := batchRiskProvenance(args, msg, shadowStreamExecutionPath, requestID)
		chatMessageID, contentPartID := msg.anchorIDStrings()
		view := batchMessageView(msg)
		toolCalls := make([]*riskv1.CustomRulesAnalysis_ToolCall, 0, len(view.Tools))
		for _, t := range view.Tools {
			toolCalls = append(toolCalls, riskv1.CustomRulesAnalysis_ToolCall_builder{
				Name:      new(t.Name),
				Arguments: new(t.Arguments),
			}.Build())
		}

		publishResults = append(publishResults, a.customRulesPub.Publish(ctx, riskv1.CustomRulesAnalysis_builder{
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
			ExecutionPath:           new(shadowStreamExecutionPath),
			ToolCallId:              &provenance.ToolCallID,
			ToolName:                &provenance.ToolName,
			HookSource:              &msg.Source,
			UserId:                  &msg.UserID,

			Content:       new(msg.Content),
			Kind:          new(msg.Type),
			ToolCalls:     toolCalls,
			CustomRuleIds: customRuleIDs,
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish custom rules scan requests", publishResults)
}
