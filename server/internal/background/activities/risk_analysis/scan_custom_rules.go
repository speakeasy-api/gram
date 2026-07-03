package risk_analysis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/risk/customrules"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func (a *AnalyzeBatch) scanCustomRules(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage, customRules []CompiledCELRule) ([][]Finding, error) {
	a.publishCustomRulesScanRequests(ctx, args, requestID, messages, customRules)

	out := make([][]Finding, len(messages))
	for i, msg := range messages {
		findings, err := ScanCELRules(a.celEng, batchMessageView(msg), customRules)
		if err != nil {
			return [][]Finding{}, err
		}
		out[i] = findings
	}
	activity.RecordHeartbeat(ctx, SourceCustom)
	return out, nil
}

// publishCustomRulesScanRequests mirrors publishGitleaksScanRequests: it emits a
// CustomRulesAnalysis per message onto the async analyzer topic, carrying the
// full CEL input (content, kind, tool calls) and the selected rule ids so the
// subscriber can re-load and evaluate the same rules against the same message.
func (a *AnalyzeBatch) publishCustomRulesScanRequests(ctx context.Context, args AnalyzeBatchArgs, requestID uuid.UUID, messages []batchMessage, customRules []CompiledCELRule) {
	ruleIDs := make([]string, 0, len(customRules))
	for _, r := range customRules {
		ruleIDs = append(ruleIDs, r.rule.RuleID)
	}

	createdAt := time.Now().UTC().Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, len(messages))
	for i, msg := range messages {
		view := batchMessageView(msg)
		toolCalls := make([]*riskv1.CustomRulesAnalysis_ToolCall, 0, len(view.Tools))
		for _, t := range view.Tools {
			toolCalls = append(toolCalls, riskv1.CustomRulesAnalysis_ToolCall_builder{
				Name:      new(t.Name),
				Arguments: new(t.Arguments),
			}.Build())
		}

		publishResults[i] = a.customRulesPub.Publish(ctx, riskv1.CustomRulesAnalysis_builder{
			RequestId:         new(requestID.String()),
			ChatMessageId:     new(msg.ID.String()),
			ProjectId:         new(args.ProjectID.String()),
			OrganizationId:    &args.OrganizationID,
			RiskPolicyId:      new(args.RiskPolicyID.String()),
			RiskPolicyVersion: &args.PolicyVersion,
			CreatedAt:         &createdAt,

			Content:       new(msg.Content),
			Kind:          new(msg.Type),
			ToolCalls:     toolCalls,
			CustomRuleIds: ruleIDs,
		}.Build())
	}
	drainPublishAcks(ctx, a.logger, "failed to publish custom rules scan request", publishResults)
}

func (a *AnalyzeBatch) customRulesForPolicy(ctx context.Context, projectID uuid.UUID, detectorIDs []string) ([]CompiledCELRule, error) {
	rules, err := customrules.LoadSelected(ctx, repo.New(a.db), projectID, detectorIDs)
	if err != nil {
		return []CompiledCELRule{}, fmt.Errorf("load custom detection rules: %w", err)
	}
	compiled, err := CompileCELRules(a.celEng, rules)
	if err != nil {
		return []CompiledCELRule{}, fmt.Errorf("compile custom detection rules: %w", err)
	}
	return compiled, nil
}
