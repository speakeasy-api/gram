package risk_analysis

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/policyflags"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
)

// setEventMatch stamps each llm_judge finding with the full event the judge saw
// (rendered from the source message) as its Match. Judge findings carry no
// literal offending substring, so the "match" surfaced in the Risk Events UI is
// the entire flagged event.
func setEventMatch(findings []scanners.Finding, msg batchMessage) {
	if len(findings) == 0 {
		return
	}
	scanners.SetEventMatch(findings, judgemessage.Render(batchJudgeMessage(msg)))
}

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

func (a *AnalyzeBatch) scanPromptPolicy(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy, messages []batchMessage, masks CategoryScopeMasks) ([][]scanners.Finding, error) {
	out := make([]scanners.Result, len(messages))
	models := make([]string, len(messages))
	providers := make([]string, len(messages))
	cfg := promptpolicy.ParseConfig(policy.ModelConfig)
	if !a.projectFlagEnabled(ctx, args.OrganizationID, args.ProjectID, feature.FlagPromptPolicies) {
		return findingsFromResults(out), nil
	}

	indices := promptPolicyIndices(messages, masks)
	if len(indices) == 0 {
		return findingsFromResults(out), nil
	}

	if !hasPrompt(policy) || a.judge == nil {
		// Fresh slice per index (not one shared slice) so setEventMatch below
		// stamps each finding with its own message rather than aliasing.
		for _, idx := range indices {
			out[idx].Findings = promptpolicy.FindingsFromEvaluation(cfg, nil, nil, true)
			setEventMatch(out[idx].Findings, messages[idx])
		}
		return findingsFromResults(out), nil
	}

	if err := a.publishPromptPolicyScanRequests(ctx, args, policy, messages, indices, false); err != nil {
		return nil, err
	}

	startedAt := time.Now().UTC()
	judgeFanout(
		ctx, a.judge,
		args.OrganizationID, args.ProjectID.String(), policy.Prompt.String, cfg,
		messages, indices,
		func(_, idx int, verdict *promptpolicy.Verdict, err error, _ time.Duration) {
			out[idx].Findings = promptpolicy.FindingsFromEvaluation(cfg, verdict, err, false)
			if err == nil && verdict != nil && verdict.Completed {
				out[idx].STokens = verdict.STokens
				out[idx].Completed = true
				models[idx] = verdict.Model
				providers[idx] = verdict.Provider
			}
			setEventMatch(out[idx].Findings, messages[idx])
		},
		func(end int) { activity.RecordHeartbeat(ctx, promptpolicy.Source, end) },
	)
	for i := range out {
		if !out[i].Completed {
			continue
		}
		provenance := batchRiskProvenance(args, messages[i], inlineBatchExecutionPath, messageScanRequestID(args, "prompt_policy", messages[i]))
		provenance.Model = models[i]
		provenance.Provider = providers[i]
		if err := a.riskRecorder.Record(ctx, metering.RiskPromptPolicy(), provenance, out[i].STokens, startedAt); err != nil {
			a.logger.ErrorContext(ctx, "record prompt policy usage", attr.SlogError(err))
		}
	}
	return findingsFromResults(out), nil
}

func (a *AnalyzeBatch) projectFlagEnabled(ctx context.Context, orgID string, projectID uuid.UUID, flag feature.Flag) bool {
	return policyflags.ProjectFlagEnabled(ctx, a.logger, repo.New(a.db), a.flags, orgID, projectID, flag)
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

func hasPrompt(policy repo.RiskPolicy) bool {
	return policy.Prompt.Valid && strings.TrimSpace(policy.Prompt.String) != ""
}

// publishPromptPolicyScanRequests publishes one request per indexed message.
// runJudge has the handler run the judge and publish its findings; without it
// the handler only records the comparison while the inline judge enforces.
func (a *AnalyzeBatch) publishPromptPolicyScanRequests(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy, messages []batchMessage, indices []int, runJudge bool) error {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(indices))
	executionPath := shadowStreamExecutionPath
	if runJudge {
		executionPath = judgeHandlerStreamExecutionPath
	}
	for _, idx := range indices {
		msg := messages[idx]
		requestID := messageScanRequestID(args, "prompt_policy", msg)
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
			RunJudge:    &runJudge,
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish prompt policy scan requests", publishResults)
}
