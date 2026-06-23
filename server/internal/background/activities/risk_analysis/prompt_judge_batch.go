package risk_analysis

import (
	"context"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// judgeConcurrency bounds the number of in-flight judge calls per batch.
const judgeConcurrency = 8

func (a *AnalyzeBatch) scanPromptPolicy(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy, messages []batchMessage, outOfPolicyScope []bool) [][]Finding {
	out := make([][]Finding, len(messages))
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
			finding := JudgeFinding(JudgeVerdict{Confidence: 0, Rationale: "Policy judge was unavailable; flagged by fail-closed policy."})
			for _, idx := range indices {
				out[idx] = []Finding{finding}
			}
		}
		return out
	}

	for start := 0; start < len(indices); start += judgeConcurrency {
		end := min(start+judgeConcurrency, len(indices))
		var wg sync.WaitGroup
		for _, idx := range indices[start:end] {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				verdict := a.judge.Evaluate(ctx, JudgeInput{
					OrgID:     args.OrganizationID,
					ProjectID: args.ProjectID.String(),
					Prompt:    policy.Prompt.String,
					Message:   batchJudgeMessage(messages[idx]),
					Config:    cfg,
				})
				if verdict != nil {
					out[idx] = []Finding{JudgeFinding(*verdict)}
				}
			}(idx)
		}
		wg.Wait()
		activity.RecordHeartbeat(ctx, SourceLLMJudge, end)
	}
	return out
}

func (a *AnalyzeBatch) projectFlagEnabled(ctx context.Context, orgID string, projectID uuid.UUID, flag feature.Flag) bool {
	if a.flags == nil {
		return false
	}
	groups, err := repo.New(a.db).GetProjectFlagGroups(ctx, projectID)
	if err != nil {
		a.logger.WarnContext(ctx, "resolve project flag groups failed", attr.SlogError(err), attr.SlogOrganizationID(orgID), attr.SlogProjectID(projectID.String()))
		return false
	}
	on, err := a.flags.IsFlagEnabled(ctx, flag, orgID, feature.OrgProjectGroups(groups.OrganizationSlug, groups.ProjectSlug))
	if err != nil {
		a.logger.WarnContext(ctx, "project flag check failed", attr.SlogError(err), attr.SlogOrganizationID(orgID))
		return false
	}
	return on
}
