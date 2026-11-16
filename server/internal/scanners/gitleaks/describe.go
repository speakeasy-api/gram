package gitleaks

import "sync"

// ruleDescriptions maps canonical rule ids to the detector's human-readable
// descriptions, built once from the same effective config the scanner uses.
var ruleDescriptions = sync.OnceValue(func() map[string]string {
	cfg, err := effectiveConfig()
	if err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(cfg.Rules))
	for _, rule := range cfg.Rules {
		out[guardRuleID(CanonicalRuleID(rule.RuleID))] = rule.Description
	}
	return out
})

// DescribeRule returns the human-readable description for a canonical rule id,
// so remote enforcement replies (which carry only rule ids) can present the
// same wording as the local scanner.
func DescribeRule(ruleID string) (string, bool) {
	description, ok := ruleDescriptions()[ruleID]
	return description, ok && description != ""
}
