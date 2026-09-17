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

// llmPolicyEvaluationFallbackLegacy is the policy_evaluations outcome recorded
// when the organization is on the LLM analyzer flag but the worker has no
// analyzer configured, so the batch ran the legacy engines instead. It is
// counted once per batch: no request reaches the consumer, so there is no
// per-message outcome to line up with.
const llmPolicyEvaluationFallbackLegacy = "fallback_legacy"

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

// llmMessageSources keeps the covered sources whose detection scope admits
// message i. It is the per-source prefilter the legacy engines apply through
// CategoryScopeMasks.Subset, folded into the request: the consumer only
// publishes findings for the sources the request names.
func llmMessageSources(masks CategoryScopeMasks, i int, coveredSources []string) []string {
	kept := make([]string, 0, len(coveredSources))
	for _, source := range coveredSources {
		if masks.AdmitsAny(i, sourceCategories[source]) {
			kept = append(kept, source)
		}
	}
	return kept
}

// publishLLMScanRequests hands the batch to the fine-tuned risk model's async
// lane: one LLMAnalysis per message in scope for at least one covered
// source, carrying the same provenance the legacy analysis requests carry so
// findings and usage readings attribute identically. Messages every covered
// source's detection scope excludes are not published, just as the legacy
// engines never scan them. The publish must succeed for the activity to
// succeed: this lane is the only engine evaluating the covered sources for
// the organization, so a dropped request is a silently unscanned message.
func (a *AnalyzeBatch) publishLLMScanRequests(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, orgSlug string, coveredSources []string, masks CategoryScopeMasks) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(messages))
	requestID := batchScanRequestID(args, "standard")
	for i, msg := range messages {
		sources := llmMessageSources(masks, i, coveredSources)
		if len(sources) == 0 {
			continue
		}
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
			Sources:          sources,
			// The streams consumer applies the analyzer's input caps and
			// reports truncation on its own span; the request carries the
			// full text.
			ContentTruncated: new(false),
		}.Build()))
	}
	if err := drainPublishAcks(ctx, "publish llm analysis requests", publishResults); err != nil {
		return err
	}
	a.metrics.RecordLLMPolicyEvaluation(ctx, args.OrganizationID, args.RiskPolicyID.String(), llmPolicyEvaluationPublished, len(publishResults))
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
