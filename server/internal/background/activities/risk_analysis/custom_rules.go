package risk_analysis

import (
	"fmt"
	"regexp"
	"strings"
)

const SourceCustom = "custom"

type CustomDetectionRule struct {
	RuleID      string
	Title       string
	Description string
	Regex       string
}

type CompiledCustomDetectionRule struct {
	CustomDetectionRule
	re *regexp.Regexp
}

func CompileCustomDetectionRules(rules []CustomDetectionRule) ([]CompiledCustomDetectionRule, error) {
	compiled := make([]CompiledCustomDetectionRule, 0, len(rules))
	for _, rule := range rules {
		pattern := strings.TrimSpace(rule.Regex)
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile custom rule %s: %w", rule.RuleID, err)
		}
		rule.RuleID = guard(rule.RuleID)
		compiled = append(compiled, CompiledCustomDetectionRule{
			CustomDetectionRule: rule,
			re:                  re,
		})
	}
	return compiled, nil
}

func ScanCustomDetectionRules(text string, rules []CompiledCustomDetectionRule) []Finding {
	var findings []Finding
	for _, rule := range rules {
		matches := rule.re.FindAllStringIndex(text, -1)
		for _, match := range matches {
			findings = append(findings, Finding{
				Source:           SourceCustom,
				RuleID:           rule.RuleID,
				Description:      customRuleDescription(rule),
				Match:            text[match[0]:match[1]],
				StartPos:         match[0],
				EndPos:           match[1],
				Tags:             nil,
				Confidence:       1.0,
				DeadLetterReason: "",
				toolCallID:       "",
			})
		}
	}
	return findings
}

func customRuleDescription(rule CompiledCustomDetectionRule) string {
	if strings.TrimSpace(rule.Description) != "" {
		return rule.Description
	}
	if strings.TrimSpace(rule.Title) != "" {
		return rule.Title
	}
	return "Custom detection rule match"
}
