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
	"github.com/speakeasy-api/gram/server/internal/scanners/llmjudge"
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
	judge llmjudge.Evaluator,
	orgID, projectID, prompt string,
	cfg llmjudge.Config,
	msgs []batchMessage,
	indices []int,
	apply func(pos, idx int, verdict *llmjudge.Verdict, err error, latency time.Duration),
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
				verdict, err := judge.Evaluate(ctx, llmjudge.Input{
					OrgID:     orgID,
					ProjectID: projectID,
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

func (a *AnalyzeBatch) scanPromptPolicy(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy, messages []batchMessage, outOfPolicyScope []bool) [][]scanners.Finding {
	out := make([][]scanners.Finding, len(messages))
	cfg := llmjudge.ParseConfig(policy.ModelConfig)
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
			finding := llmjudge.NewFinding(llmjudge.FailClosedVerdict(nil))
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
		func(_, idx int, verdict *llmjudge.Verdict, err error, _ time.Duration) {
			if err != nil {
				if !cfg.FailOpen {
					out[idx] = []scanners.Finding{llmjudge.NewFinding(llmjudge.FailClosedVerdict(err))}
				}
			} else if verdict != nil && verdict.Matched {
				out[idx] = []scanners.Finding{llmjudge.NewFinding(*verdict)}
			}
		},
		func(end int) { activity.RecordHeartbeat(ctx, llmjudge.Source, end) },
	)
	return out
}

func (a *AnalyzeBatch) projectFlagEnabled(ctx context.Context, orgID string, projectID uuid.UUID, flag feature.Flag) bool {
	return policyflags.ProjectFlagEnabled(ctx, a.logger, repo.New(a.db), a.flags, orgID, projectID, flag)
}
