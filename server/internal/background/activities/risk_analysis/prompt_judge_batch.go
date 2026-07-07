package risk_analysis

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/policyflags"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// judgeConcurrency bounds the number of in-flight judge calls per batch.
const judgeConcurrency = 8

// judgeFanout evaluates the given message indices against a guardrail prompt
// with at most judgeConcurrency calls in flight, invoking apply for each result
// (pos is the position within indices, idx the original message index, verdict
// nil when the judge returned none, latency the wall-clock call time). onChunk,
// when non-nil, runs after each chunk with the exclusive end position so callers
// can record progress heartbeats. Shared by the batch analyzer and the
// workbench replay so both drive the judge identically.
func judgeFanout(
	ctx context.Context,
	judge PromptJudge,
	orgID, projectID, prompt string,
	cfg JudgeConfig,
	msgs []batchMessage,
	indices []int,
	apply func(pos, idx int, verdict *JudgeVerdict, latency time.Duration),
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
				verdict := judge.Evaluate(ctx, JudgeInput{
					OrgID:     orgID,
					ProjectID: projectID,
					Prompt:    prompt,
					Message:   batchJudgeMessage(msgs[idx]),
					Config:    cfg,
				})
				apply(pos, idx, verdict, time.Since(started))
			}(pos)
		}
		wg.Wait()
		if onChunk != nil {
			onChunk(end)
		}
	}
}

func (a *AnalyzeBatch) scanPromptPolicy(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy, messages []batchMessage, outOfPolicyScope []bool) [][]scanners.Finding {
	out := make([][]scanners.Finding, len(messages))
	cfg := ParseJudgeConfig(policy.ModelConfig)
	if !a.projectFlagEnabled(ctx, args.OrganizationID, args.ProjectID, feature.FlagPromptPolicies) {
		return out
	}

	indices := make([]int, 0, len(messages))
	for i := range messages {
		if len(outOfPolicyScope) > 0 && outOfPolicyScope[i] {
			continue
		}
		indices = append(indices, i)
	}
	if len(indices) == 0 {
		return out
	}

	if a.judge == nil || !policy.Prompt.Valid || strings.TrimSpace(policy.Prompt.String) == "" {
		if !cfg.FailOpen {
			finding := JudgeFinding(JudgeVerdict{
				Matched:          true,
				Confidence:       0,
				Rationale:        "Policy judge was unavailable; flagged by fail-closed policy.",
				CostUSD:          0,
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
			})
			for _, idx := range indices {
				out[idx] = []scanners.Finding{finding}
			}
		}
		return out
	}

	judgeFanout(
		ctx, a.judge,
		args.OrganizationID, args.ProjectID.String(), policy.Prompt.String, cfg,
		messages, indices,
		func(_, idx int, verdict *JudgeVerdict, _ time.Duration) {
			if verdict != nil && verdict.Matched {
				out[idx] = []scanners.Finding{JudgeFinding(*verdict)}
			}
		},
		func(end int) { activity.RecordHeartbeat(ctx, SourceLLMJudge, end) },
	)
	return out
}

func (a *AnalyzeBatch) projectFlagEnabled(ctx context.Context, orgID string, projectID uuid.UUID, flag feature.Flag) bool {
	return policyflags.ProjectFlagEnabled(ctx, a.logger, repo.New(a.db), a.flags, orgID, projectID, flag)
}
