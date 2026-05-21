package risk_analysis

// DisabledRuleSet is a lookup set of canonical rule_ids the policy author has
// unchecked within otherwise-enabled categories. The scanner pipeline calls
// FilterFindings after each scanner returns to drop matching findings before
// they reach the dedup/write path. Empty set is a no-op.
type DisabledRuleSet struct {
	rules map[string]struct{}
}

// NewDisabledRuleSet builds a lookup from the policy's disabled_rules column.
// nil and empty slices both produce a zero-cost no-op set.
func NewDisabledRuleSet(disabled []string) DisabledRuleSet {
	if len(disabled) == 0 {
		return DisabledRuleSet{rules: nil}
	}
	m := make(map[string]struct{}, len(disabled))
	for _, r := range disabled {
		if r == "" {
			continue
		}
		m[r] = struct{}{}
	}
	return DisabledRuleSet{rules: m}
}

// Contains reports whether ruleID is in the disabled set.
func (d DisabledRuleSet) Contains(ruleID string) bool {
	if d.rules == nil {
		return false
	}
	_, ok := d.rules[ruleID]
	return ok
}

// Empty reports whether the set carries any disabled rule_ids.
func (d DisabledRuleSet) Empty() bool {
	return len(d.rules) == 0
}

// FilterFindings returns a new slice with findings whose RuleID is in the
// disabled set removed. Returns the input slice unchanged when the set is
// empty so callers can call this unconditionally.
func (d DisabledRuleSet) FilterFindings(in []Finding) []Finding {
	if d.Empty() || len(in) == 0 {
		return in
	}
	out := in[:0:0]
	for _, f := range in {
		if d.Contains(f.RuleID) {
			continue
		}
		out = append(out, f)
	}
	return out
}
