package risk_analysis

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const judgeConcurrency = 8

func (a *AnalyzeBatch) scanPromptJudge(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy, messages []repo.GetMessageContentBatchRow, scopeExcluded []bool) [][]Finding {
	out := make([][]Finding, len(messages))
	cfg := ParseJudgeConfig(policy.ModelConfig)
	if !a.projectFlagEnabled(ctx, args.OrganizationID, args.ProjectID, feature.FlagPromptPolicies) {
		return out
	}

	indices := make([]int, 0, len(messages))
	for i, msg := range messages {
		if scopeExcluded != nil && scopeExcluded[i] {
			continue
		}
		if _, ok := messageRowMessageType(msg); ok {
			indices = append(indices, i)
		}
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
					Message:   a.judgeMessageForRow(ctx, messages[idx]),
					Config:    cfg,
				})
				if verdict != nil {
					out[idx] = []Finding{JudgeFinding(*verdict)}
				}
			}(idx)
		}
		wg.Wait()
		activity.RecordHeartbeat(ctx, "llm_judge", end)
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

func (a *AnalyzeBatch) judgeMessageForRow(ctx context.Context, msg repo.GetMessageContentBatchRow) JudgeMessage {
	messageType, _ := messageRowMessageType(msg)
	if messageType == message.ToolRequest {
		calls := a.parseRecordedToolCalls(ctx, SourceLLMJudge, msg.ToolCalls)
		switch len(calls) {
		case 0:
			return NewJudgeMessage(messageType, "", string(msg.ToolCalls))
		case 1:
			return NewJudgeMessage(messageType, calls[0].Function.Name, calls[0].Function.Arguments)
		default:
			judgeCalls := make([]JudgeToolCall, 0, len(calls))
			for _, c := range calls {
				if c.Function.Name == "" && strings.TrimSpace(c.Function.Arguments) == "" {
					continue
				}
				judgeCalls = append(judgeCalls, NewJudgeToolCall(c.Function.Name, c.Function.Arguments))
			}
			if len(judgeCalls) == 0 {
				return NewJudgeMessage(messageType, "", string(msg.ToolCalls))
			}
			return NewJudgeMessageForToolCalls(judgeCalls)
		}
	}
	return NewJudgeMessage(messageType, "", msg.Content)
}

func (a *AnalyzeBatch) customRuleMessageView(ctx context.Context, msg repo.GetMessageContentBatchRow) MessageView {
	messageType, _ := messageRowMessageType(msg)
	view := MessageView{Content: msg.Content, Type: messageType, Tools: nil}
	if messageType != message.ToolRequest {
		return view
	}
	for _, c := range a.parseRecordedToolCalls(ctx, SourceCustom, msg.ToolCalls) {
		if c.Function.Name == "" && strings.TrimSpace(c.Function.Arguments) == "" {
			continue
		}
		view.Tools = append(view.Tools, NewToolView(c.Function.Name, c.Function.Arguments))
	}
	return view
}

func (a *AnalyzeBatch) customRulesForPolicy(ctx context.Context, projectID uuid.UUID, detectorIDs []string) ([]CompiledCELRule, error) {
	if len(detectorIDs) == 0 {
		return nil, nil
	}

	rules, err := repo.New(a.db).ListCustomDetectionRules(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list custom detection rules: %w", err)
	}

	detectors := make(map[string]struct{}, len(detectorIDs))
	for _, id := range detectorIDs {
		detectors[id] = struct{}{}
	}

	customRules := make([]CustomDetectionRule, 0, len(detectors))
	for _, rule := range rules {
		if _, ok := detectors[rule.RuleID]; !ok {
			continue
		}
		customRules = append(customRules, CustomDetectionRule{
			RuleID:        rule.RuleID,
			Title:         rule.Title,
			Description:   rule.Description,
			DetectionExpr: rule.DetectionExpr.String,
			Regex:         rule.Regex.String,
		})
	}

	eng := a.celEng
	compiled, err := CompileCELRules(eng, customRules)
	if err != nil {
		return nil, err
	}
	return compiled, nil
}
