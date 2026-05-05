package risk_analysis

import "strings"

// SourcePromptInjection is the policy source value that enables prompt
// injection scanning. Used by both the batch analyzer (writes findings to
// risk_results) and the realtime risk scanner (hook deny path for
// action='block' policies).
const SourcePromptInjection = "prompt_injection"

// promptInjectionStubMarker is the literal substring the stub detector
// flags. This is intentional placeholder logic so the wiring (policy
// source -> scanner -> findings -> dashboard) can be exercised
// end-to-end before a real classifier lands.
const promptInjectionStubMarker = "__INJECT__"

const (
	promptInjectionRuleStub = "prompt-injection-stub"
	promptInjectionDescStub = "Stub prompt injection marker detected"
)

// DetectPromptInjection scans text for prompt-injection signals and returns
// one Finding per match. Returns nil when nothing matches.
func DetectPromptInjection(text string) []Finding {
	if text == "" {
		return nil
	}

	var findings []Finding
	idx := 0
	for {
		rel := strings.Index(text[idx:], promptInjectionStubMarker)
		if rel < 0 {
			break
		}
		start := idx + rel
		end := start + len(promptInjectionStubMarker)
		findings = append(findings, Finding{
			RuleID:      promptInjectionRuleStub,
			Description: promptInjectionDescStub,
			Match:       promptInjectionStubMarker,
			StartPos:    start,
			EndPos:      end,
			Tags:        nil,
			Source:      SourcePromptInjection,
			Confidence:  1.0,
		})
		idx = end
	}
	return findings
}
