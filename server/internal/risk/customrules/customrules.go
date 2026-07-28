package customrules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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

// EffectiveDetectionExpr returns the stored CEL predicate, or a legacy regex
// wrapped as a CEL expression, or "" when the rule has neither.
func (r Rule) EffectiveDetectionExpr() string {
	if expr := strings.TrimSpace(r.DetectionExpr); expr != "" {
		return expr
	}
	if pattern := strings.TrimSpace(r.Regex); pattern != "" {
		return "content.matchRegex(" + strconv.Quote(pattern) + ")"
	}
	return ""
}

// DisplayDescription returns the best human-facing label for a rule: its
// description, then its title, falling back to the rule id.
func (r Rule) DisplayDescription() string {
	if d := strings.TrimSpace(r.Description); d != "" {
		return d
	}
	if t := strings.TrimSpace(r.Title); t != "" {
		return t
	}
	return r.RuleID
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
