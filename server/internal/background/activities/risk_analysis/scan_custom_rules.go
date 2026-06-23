package risk_analysis

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/risk/customrules"
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
