package risk_analysis

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func (a *AnalyzeBatch) scanCustomRules(ctx context.Context, messages []batchMessage, customRules []CompiledCELRule) ([][]Finding, error) {
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

func (a *AnalyzeBatch) customRulesForPolicy(ctx context.Context, projectID uuid.UUID, detectorIDs []string) ([]CompiledCELRule, error) {
	if len(detectorIDs) == 0 {
		return []CompiledCELRule{}, nil
	}

	rules, err := repo.New(a.db).ListCustomDetectionRules(ctx, projectID)
	if err != nil {
		return []CompiledCELRule{}, fmt.Errorf("list custom detection rules: %w", err)
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

	compiled, err := CompileCELRules(a.celEng, customRules)
	if err != nil {
		return []CompiledCELRule{}, fmt.Errorf("compile custom detection rules: %w", err)
	}
	return compiled, nil
}
