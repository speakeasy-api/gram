package risk_analysis

import (
	"context"
	"strings"
	"time"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
)

// llmPolicyEvaluationPublished is the policy_evaluations outcome recorded once
// the batch's analysis requests are acknowledged by the topic. Verdict outcomes
// (clean, matched, dead_letter) are recorded by the streams consumer, which is
// where the model reply is known.
const llmPolicyEvaluationPublished = "published"

// llmCoveredSources returns, in llmanalyzer.CoveredSources order, the policy
// sources the fine-tuned model stands in for on this batch.
func llmCoveredSources(sources sourceSet) []string {
	covered := make([]string, 0, len(llmanalyzer.CoveredSources))
	for _, source := range llmanalyzer.CoveredSources {
		if sources.Has(source) {
			covered = append(covered, source)
		}
	}
	return covered
}

// publishLLMScanRequests hands every message in the batch to the fine-tuned
// risk model's async lane: one LLMAnalysis per message, carrying the same
// provenance the legacy analysis requests carry so findings and usage
// readings attribute identically. The publish must succeed for the activity
// to succeed: this lane is the only engine evaluating the covered sources for
// the organization, so a dropped request is a silently unscanned message.
func (a *AnalyzeBatch) publishLLMScanRequests(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, orgSlug string, coveredSources []string) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(messages))
	requestID := batchScanRequestID(args, "standard")
	for _, msg := range messages {
		chatMessageID, contentPartID := msg.anchorIDStrings()
		provenance := batchRiskProvenance(args, msg, asyncStreamExecutionPath, requestID.String())
		body, toolCalls := llmMessageInput(msg)

		publishResults = append(publishResults, a.llmPub.Publish(ctx, riskv1.LLMAnalysis_builder{
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
			ExecutionPath:           new(asyncStreamExecutionPath),
			ToolCallId:              &provenance.ToolCallID,
			HookSource:              &msg.Source,
			PolicyLinkReason:        nil,
			ExternalConversationId:  nil,

			Content:          new(msg.scanSurface()),
			UserId:           &msg.UserID,
			MessageType:      new(msg.Type),
			Body:             &body,
			ToolName:         &provenance.ToolName,
			ToolCalls:        toolCalls,
			OrganizationSlug: &orgSlug,
			Sources:          coveredSources,
			// The streams consumer applies the analyzer's input caps and
			// reports truncation on its own span; the request carries the
			// full text.
			ContentTruncated: new(false),
		}.Build()))
	}
	if err := drainPublishAcks(ctx, "publish llm analysis requests", publishResults); err != nil {
		return err
	}
	a.metrics.RecordLLMPolicyEvaluation(ctx, args.OrganizationID, args.RiskPolicyID.String(), llmPolicyEvaluationPublished)
	return nil
}

// llmMessageInput renders the message the way the judge lane does: a tool
// request carries its calls as structured tool_calls with an empty body so the
// model sees the harness ids, names and arguments; every other message carries
// its text as the body. A tool request whose calls failed to parse falls back
// to the raw tool_calls JSON as body, matching batchJudgeMessage.
func llmMessageInput(msg batchMessage) (body string, toolCalls []*riskv1.LLMAnalysis_ToolCall) {
	if msg.Type != message.ToolRequest {
		return msg.Content, nil
	}
	calls := make([]*riskv1.LLMAnalysis_ToolCall, 0, len(msg.ToolCalls))
	for _, call := range msg.ToolCalls {
		if call.Function.Name == "" && strings.TrimSpace(call.Function.Arguments) == "" {
			continue
		}
		calls = append(calls, riskv1.LLMAnalysis_ToolCall_builder{
			Id:        new(call.ID),
			Name:      new(call.Function.Name),
			Arguments: new(call.Function.Arguments),
		}.Build())
	}
	if len(calls) == 0 {
		return string(msg.RawToolCalls), nil
	}
	return "", calls
}
