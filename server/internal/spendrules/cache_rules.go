package spendrules

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/spendrules/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func BuildGateRules(rows []repo.SpendRule, now time.Time) (GateRules, error) {
	sourceUpdatedAt := time.Time{}
	rules := make([]GateRule, 0, len(rows))
	for _, row := range rows {
		if row.UpdatedAt.Valid && row.UpdatedAt.Time.After(sourceUpdatedAt) {
			sourceUpdatedAt = row.UpdatedAt.Time
		}
		windowStart, windowEnd, err := WindowBounds(row.WindowKind, now)
		if err != nil {
			return GateRules{}, fmt.Errorf("window bounds for rule %s: %w", row.ID, err)
		}
		rules = append(rules, GateRule{
			RuleURN:     urn.NewSpendRule(row.Slug, row.Version).String(),
			RuleName:    row.Name,
			Action:      row.Action,
			TargetExpr:  row.TargetExpr,
			RuleExpr:    row.RuleExpr,
			LimitUSD:    row.LimitUsdCents.USD(),
			WarnAtPct:   row.WarnAtPct,
			WindowKind:  row.WindowKind,
			WindowStart: windowStart,
			WindowEnd:   windowEnd,
		})
	}
	if sourceUpdatedAt.IsZero() {
		sourceUpdatedAt = now
	}
	return GateRules{
		SourceUpdatedAt: sourceUpdatedAt,
		Rules:           rules,
	}, nil
}

func WriteGateRulesFromDB(ctx context.Context, cacheImpl cache.Cache, queries *repo.Queries, organizationID string, now time.Time) error {
	rules, err := queries.ListEnabledSpendRules(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("list enabled spend rules: %w", err)
	}
	gateRules, err := BuildGateRules(rules, now)
	if err != nil {
		return err
	}
	if err := WriteGateRules(ctx, cacheImpl, organizationID, gateRules); err != nil {
		return err
	}
	return nil
}
