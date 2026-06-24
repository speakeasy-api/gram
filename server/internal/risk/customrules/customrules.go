package customrules

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

type Rule struct {
	RuleID        string
	Title         string
	Description   string
	DetectionExpr string
	Regex         string
}

func LoadSelected(ctx context.Context, queries *repo.Queries, projectID uuid.UUID, detectorIDs []string) ([]Rule, error) {
	if len(detectorIDs) == 0 {
		return []Rule{}, nil
	}

	rules, err := queries.ListCustomDetectionRules(ctx, projectID)
	if err != nil {
		return []Rule{}, fmt.Errorf("list custom detection rules: %w", err)
	}

	detectors := make(map[string]struct{}, len(detectorIDs))
	for _, id := range detectorIDs {
		detectors[id] = struct{}{}
	}

	customRules := make([]Rule, 0, len(detectors))
	for _, rule := range rules {
		if _, ok := detectors[rule.RuleID]; !ok {
			continue
		}
		customRules = append(customRules, Rule{
			RuleID:        rule.RuleID,
			Title:         rule.Title,
			Description:   rule.Description,
			DetectionExpr: rule.DetectionExpr.String,
			Regex:         rule.Regex.String,
		})
	}
	return customRules, nil
}
