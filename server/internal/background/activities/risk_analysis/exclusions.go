package risk_analysis

import (
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// ExclusionSet evaluates risk exclusions against findings. It mirrors
// DisabledRuleSet: the scanner pipeline calls FilterFindings to drop findings
// an exclusion suppresses before they reach the write path (going-forward
// suppression). The same matching logic is mirrored in SQL by the retroactive
// reconcile sweep so existing findings are flagged consistently.
//
// An exclusion's rule_id_filter / source_filter narrow which findings it
// applies to; an empty filter means "any". match_type selects the comparison:
//
//	exact       finding.Match == value
//	regex       value (RE2) matches finding.Match
//	rule_id     finding.RuleID == value
//	source      finding.Source == value
//	entity_type finding.RuleID == "pii." + lower(value)
type ExclusionSet struct {
	rules []exclusionRule
}

type exclusionRule struct {
	id           uuid.UUID
	matchType    string
	value        string
	ruleIDFilter string
	sourceFilter string
	re           *regexp.Regexp // compiled when matchType == "regex"
}

// NewExclusionSet builds a set from the enabled exclusions that apply to a
// policy (its own plus any global ones). A regex that fails to compile is
// skipped defensively — patterns are validated at create/update time.
func NewExclusionSet(exclusions []repo.RiskExclusion) ExclusionSet {
	if len(exclusions) == 0 {
		return ExclusionSet{rules: nil}
	}
	rules := make([]exclusionRule, 0, len(exclusions))
	for _, e := range exclusions {
		r := exclusionRule{
			id:           e.ID,
			matchType:    e.MatchType,
			value:        e.MatchValue,
			ruleIDFilter: e.RuleIDFilter.String, // NULL -> "" (any)
			sourceFilter: e.SourceFilter.String, // NULL -> "" (any)
			re:           nil,
		}
		if e.MatchType == "regex" {
			re, err := regexp.Compile(e.MatchValue)
			if err != nil {
				continue
			}
			r.re = re
		}
		rules = append(rules, r)
	}
	return ExclusionSet{rules: rules}
}

// Empty reports whether the set carries any exclusions.
func (s ExclusionSet) Empty() bool {
	return len(s.rules) == 0
}

// Excluded reports whether any exclusion suppresses the finding.
func (s ExclusionSet) Excluded(f scanners.Finding) bool {
	_, ok := s.ExcludedBy(f)
	return ok
}

// ExcludedBy returns the id of the first exclusion that suppresses the finding,
// and whether one did. Rules are evaluated in the order they were loaded, so
// the returned id is the earliest-matching exclusion.
func (s ExclusionSet) ExcludedBy(f scanners.Finding) (uuid.UUID, bool) {
	for _, r := range s.rules {
		if r.ruleIDFilter != "" && f.RuleID != r.ruleIDFilter {
			continue
		}
		if r.sourceFilter != "" && f.Source != r.sourceFilter {
			continue
		}
		switch r.matchType {
		case "exact":
			if f.Match == r.value {
				return r.id, true
			}
		case "regex":
			if r.re != nil && r.re.MatchString(f.Match) {
				return r.id, true
			}
		case "rule_id":
			if f.RuleID == r.value {
				return r.id, true
			}
		case "source":
			if f.Source == r.value {
				return r.id, true
			}
		case "entity_type":
			if f.RuleID == "pii."+strings.ToLower(r.value) {
				return r.id, true
			}
		}
	}
	return uuid.UUID{}, false
}

// FilterFindings returns a new slice with excluded findings removed. Returns
// the input unchanged when the set is empty so callers can call it
// unconditionally.
func (s ExclusionSet) FilterFindings(in []scanners.Finding) []scanners.Finding {
	if s.Empty() || len(in) == 0 {
		return in
	}
	out := in[:0:0]
	for _, f := range in {
		if s.Excluded(f) {
			continue
		}
		out = append(out, f)
	}
	return out
}
