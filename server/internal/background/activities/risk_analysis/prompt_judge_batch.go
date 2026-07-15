package risk_analysis

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
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

func (a *AnalyzeBatch) scanPromptPolicy(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy, messages []batchMessage, outOfPolicyScope []bool) [][]scanners.Finding {
	out := make([][]scanners.Finding, len(messages))
	cfg := promptpolicy.ParseConfig(policy.ModelConfig)
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
		// Fresh slice per index (not one shared slice) so setEventMatch below
		// stamps each finding with its own message rather than aliasing.
		for _, idx := range indices {
			findings := promptpolicy.FindingsFromEvaluation(cfg, nil, nil, true)
			setEventMatch(findings, messages[idx])
			out[idx] = findings
		}
		return out
	}

	judgeFanout(
		ctx, a.judge,
		args.OrganizationID, args.ProjectID.String(), policy.Prompt.String, cfg,
		messages, indices,
		func(_, idx int, verdict *promptpolicy.Verdict, err error, _ time.Duration) {
			findings := promptpolicy.FindingsFromEvaluation(cfg, verdict, err, false)
			setEventMatch(findings, messages[idx])
			out[idx] = findings
		},
		func(end int) { activity.RecordHeartbeat(ctx, promptpolicy.Source, end) },
	)
	return out
}

func (a *AnalyzeBatch) projectFlagEnabled(ctx context.Context, orgID string, projectID uuid.UUID, flag feature.Flag) bool {
	return policyflags.ProjectFlagEnabled(ctx, a.logger, repo.New(a.db), a.flags, orgID, projectID, flag)
}
