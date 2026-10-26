package risk_analysis

import (
	"context"
	"time"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// promptPolicyIndices lists the messages the prompt-policy detection scope admits.
func promptPolicyIndices(messages []batchMessage, masks CategoryScopeMasks) []int {
	indices := make([]int, 0, len(messages))
	for i := range messages {
		if masks.InScope(i, categories.CategoryPromptPolicy) {
			indices = append(indices, i)
		}
	}
	return indices
}

// publishPromptPolicyScanRequests publishes one request per indexed message
// for the handler to judge and publish its findings.
func (a *AnalyzeBatch) publishPromptPolicyScanRequests(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy, messages []batchMessage, indices []int) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(indices))
	executionPath := judgeHandlerStreamExecutionPath
	for _, idx := range indices {
		msg := messages[idx]
		requestID := messageScanRequestID(args, scanLanePromptPolicy, msg)
		provenance := batchRiskProvenance(args, msg, executionPath, requestID)
		chatMessageID, contentPartID := msg.anchorIDStrings()
		jm := batchJudgeMessage(msg)
		toolCalls := make([]*riskv1.PromptPolicyAnalysis_ToolCall, 0, len(jm.ToolCalls))
		for _, call := range jm.ToolCalls {
			toolCalls = append(toolCalls, riskv1.PromptPolicyAnalysis_ToolCall_builder{
				Name:      &call.ToolName,
				Arguments: &call.Arguments,
			}.Build())
		}

		publishResults = append(publishResults, a.promptPolicyPub.Publish(ctx, riskv1.PromptPolicyAnalysis_builder{
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

			Content:     new(msg.Content),
			UserId:      &msg.UserID,
			Prompt:      &policy.Prompt.String,
			ModelConfig: policy.ModelConfig,
			MessageType: new(jm.Type),
			Body:        &jm.Body,
			ToolName:    &jm.ToolName,
			ToolCalls:   toolCalls,
			RunJudge:    new(true),
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish prompt policy scan requests", publishResults)
}
