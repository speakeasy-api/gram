package risk_analysis

import (
	"context"
	"sync"
	"time"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
)

// judgeConcurrency bounds the number of in-flight judge calls per batch.
const judgeConcurrency = 8

// judgeFanout evaluates the given message indices against a guardrail prompt
// with at most judgeConcurrency calls in flight, invoking apply for each result
// (pos is the position within indices, idx the original message index, verdict
// nil when the judge skipped, err non-nil when it degraded, latency the
// wall-clock call time). onChunk, when non-nil, runs after each chunk with the
// exclusive end position so callers can record progress heartbeats. Shared by
// the batch analyzer and the workbench replay so both drive the judge
// identically.
func judgeFanout(
	ctx context.Context,
	judge promptpolicy.Evaluator,
	orgID, projectID, prompt string,
	cfg promptpolicy.Config,
	msgs []batchMessage,
	indices []int,
	apply func(pos, idx int, verdict *promptpolicy.Verdict, err error, latency time.Duration),
	onChunk func(end int),
) {
	for start := 0; start < len(indices); start += judgeConcurrency {
		if err := ctx.Err(); err != nil {
			for pos := start; pos < len(indices); pos++ {
				idx := indices[pos]
				apply(pos, idx, nil, err, 0)
			}
			return
		}

		end := min(start+judgeConcurrency, len(indices))
		var wg sync.WaitGroup
		for pos := start; pos < end; pos++ {
			wg.Add(1)
			go func(pos int) {
				defer wg.Done()
				idx := indices[pos]
				started := time.Now()
				verdict, err := judge(ctx, promptpolicy.Input{
					OrgID:     orgID,
					ProjectID: projectID,
					UserID:    msgs[idx].UserID,
					Prompt:    prompt,
					Message:   batchJudgeMessage(msgs[idx]),
					Config:    cfg,
				})
				apply(pos, idx, verdict, err, time.Since(started))
			}(pos)
		}
		wg.Wait()
		if onChunk != nil {
			onChunk(end)
		}
	}
}

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
