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
	ev := judgemessage.Render(batchJudgeMessage(msg))
	for j := range findings {
		findings[j].Match = ev
		findings[j].EndPos = len(ev)
	}
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

	indices := make([]int, 0, len(messages))
	for i := range messages {
		if !masks.InScope(i, categories.CategoryPromptPolicy) {
			continue
		}
		indices = append(indices, i)
	}
	if len(indices) == 0 {
		return findingsFromResults(out), nil
	}

	if a.judge == nil || !policy.Prompt.Valid || strings.TrimSpace(policy.Prompt.String) == "" {
		// Fresh slice per index (not one shared slice) so setEventMatch below
		// stamps each finding with its own message rather than aliasing.
		for _, idx := range indices {
			out[idx].Findings = promptpolicy.FindingsFromEvaluation(cfg, nil, nil, true)
			setEventMatch(out[idx].Findings, messages[idx])
		}
		return findingsFromResults(out), nil
	}

	if err := a.publishPromptPolicyScanRequests(ctx, args, policy, messages, indices); err != nil {
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
	requestID := batchScanRequestID(args, "prompt_policy").String()

	for i := range out {
		if !out[i].Completed {
			continue
		}
		provenance := batchRiskProvenance(args, messages[i], inlineBatchExecutionPath, requestID)
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

func (a *AnalyzeBatch) publishPromptPolicyScanRequests(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy, messages []batchMessage, indices []int) error {

	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(indices))
	requestID := batchScanRequestID(args, "prompt_policy")
	for _, idx := range indices {
		msg := messages[idx]
		provenance := batchRiskProvenance(args, msg, shadowStreamExecutionPath, requestID.String())
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
			HookSource:              &msg.Source,

			Content:     new(msg.Content),
			UserId:      &msg.UserID,
			Prompt:      &policy.Prompt.String,
			ModelConfig: policy.ModelConfig,
			MessageType: new(jm.Type),
			Body:        &jm.Body,
			ToolName:    &jm.ToolName,
			ToolCalls:   toolCalls,
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish prompt policy scan requests", publishResults)
}
