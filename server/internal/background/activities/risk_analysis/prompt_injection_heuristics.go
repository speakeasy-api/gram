package risk_analysis

import (
	"regexp"
	"strings"
)

// heuristicRule is one named L0 detector. Each rule contributes findings
// independently; the orchestrator filters by HeuristicEmitThreshold and the
// per-policy family toggle (e.g. PromptInjectionConfig.DetectRoleHijack).
type heuristicRule struct {
	id          string
	description string
	family      ruleFamily
	confidence  float64
	pattern     *regexp.Regexp
}

type ruleFamily int

const (
	familyInstructionOverride ruleFamily = iota
	familyRoleHijack
	familySystemPromptLeak
	familyDelimiterInjection
	familyEncodedPayload
	familyToolAbuse
)

// fuzzyMatchInputCap bounds the sliding-substring scan so a 10MB blob
// pasted into a chat message can't pin a CPU. The realtime hook path
// is on the critical path of every tool call.
const fuzzyMatchInputCap = 8 * 1024

var (
	heuristicRules    []heuristicRule
	overrideKeywords  []string // precomputed combinatorial bank, lowercased
	delimiterPatterns *regexp.Regexp
)

func init() {
	heuristicRules = []heuristicRule{
		{
			id:          "pi.role-hijack.you-are-now",
			description: "Role hijack: 'you are now' assertion",
			family:      familyRoleHijack,
			confidence:  0.75,
			pattern:     regexp.MustCompile(`(?i)\byou are now\b`),
		},
		{
			id:          "pi.role-hijack.act-as-privileged",
			description: "Role hijack: 'act as <privileged role>'",
			family:      familyRoleHijack,
			confidence:  0.85,
			pattern:     regexp.MustCompile(`(?i)\bact as\b.{0,40}\b(admin|root|developer|dan|jailbroken|unrestricted)\b`),
		},
		{
			id:          "pi.system-prompt-leak",
			description: "Attempt to elicit system prompt or initial instructions",
			family:      familySystemPromptLeak,
			confidence:  0.85,
			pattern:     regexp.MustCompile(`(?i)\b(reveal|print|show|repeat|output|leak)\b.{0,30}\b(system\s*prompt|initial\s*instructions|hidden\s*prompt)\b`),
		},
		{
			id:          "pi.encoded-payload",
			description: "Long encoded blob adjacent to decode/execute verb",
			family:      familyEncodedPayload,
			confidence:  0.7,
			pattern:     regexp.MustCompile(`(?i)\b(decode|execute|eval|run)\b.{0,40}([A-Za-z0-9+/]{40,}={0,2}|[0-9a-fA-F]{64,})`),
		},
		{
			id:          "pi.tool-abuse.shell",
			description: "Instruction to invoke shell or destructive command",
			family:      familyToolAbuse,
			confidence:  0.8,
			// `/bin/sh` lives outside the leading `\b` group: a space-then-slash
			// transition is non-word-to-non-word, which `\b` doesn't satisfy, so
			// the literal needs its own no-boundary alternative.
			pattern: regexp.MustCompile(`(?i)\b(call|invoke|execute|run)\b.{0,20}(?:/bin/sh|\b(?:shell|bash|exec|rm\s+-rf|curl\s+http))`),
		},
	}

	// Delimiter injection: structural attempts to forge role boundaries.
	delimiterPatterns = regexp.MustCompile(
		`(?i)(<\s*/?\s*(system|assistant|user|instructions)\s*>|###\s*system\b|\[INST\]|<\|im_start\|>|<\|im_end\|>)`,
	)

	overrideKeywords = generateOverrideKeywords()
}

// generateOverrideKeywords builds the combinatorial keyword bank for
// instruction-override detection. Inspired by LiteLLM's verb × adjective ×
// preposition expansion but adds a noun axis (instructions / prompts / rules /
// context) so exact-substring matching catches real-world phrasings like
// "ignore previous instructions" without needing a fuzzy ratio. A fuzzy layer
// can be added later for typo/paraphrase tolerance.
func generateOverrideKeywords() []string {
	verbs := []string{
		"ignore", "disregard", "skip", "forget", "neglect", "overlook",
		"omit", "bypass", "do not follow", "do not obey",
		"pay no attention to",
	}
	adjectives := []string{
		"", "prior", "previous", "preceding", "above", "foregoing", "earlier", "initial", "all", "all prior", "all previous", "the above",
	}
	nouns := []string{
		"instructions", "instruction", "prompts", "prompt", "rules", "context", "directives", "guidelines", "constraints",
	}

	seen := make(map[string]struct{})
	out := make([]string, 0, len(verbs)*len(adjectives)*len(nouns))
	for _, v := range verbs {
		for _, a := range adjectives {
			for _, n := range nouns {
				phrase := strings.TrimSpace(strings.Join(filterEmpty([]string{v, a, n}), " "))
				if phrase == "" {
					continue
				}
				if len(strings.Fields(phrase)) < 2 {
					continue
				}
				lower := strings.ToLower(phrase)
				if _, dup := seen[lower]; dup {
					continue
				}
				seen[lower] = struct{}{}
				out = append(out, lower)
			}
		}
	}
	return out
}

func filterEmpty(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// asciiToLower folds ASCII A-Z to a-z while preserving byte length, so a byte
// index found in the result is valid as an offset into the original string.
// Non-ASCII bytes pass through unchanged. Required because strings.ToLower
// performs Unicode case folding that can expand byte length (e.g. Ⱥ → ⱥ goes
// from 2 to 3 bytes).
func asciiToLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// runHeuristics applies every rule family to text and returns one Finding
// per match.
//
// Only detectInstructionOverrides applies fuzzyMatchInputCap — it does an
// O(N×M) substring scan against ~1,100 keywords, so unbounded input is the
// real CPU risk on the realtime hook path. The regex-based detectors run on
// the full input but rely on Go RE2 with bounded `.{0,N}` quantifiers, which
// are linear-time; capping them would just trade defense-in-depth coverage
// (delimiters near the end of long messages) for no measurable CPU win.
func runHeuristics(text string) []Finding {
	if text == "" {
		return nil
	}

	var findings []Finding
	findings = append(findings, detectInstructionOverrides(text)...)
	findings = append(findings, runFamily(text, familyRoleHijack)...)
	findings = append(findings, runFamily(text, familySystemPromptLeak)...)
	findings = append(findings, detectDelimiterInjection(text)...)
	findings = append(findings, runFamily(text, familyEncodedPayload)...)
	findings = append(findings, runFamily(text, familyToolAbuse)...)
	return findings
}

// runFamily applies all heuristicRules in a family to text.
func runFamily(text string, fam ruleFamily) []Finding {
	var out []Finding
	for _, rule := range heuristicRules {
		if rule.family != fam {
			continue
		}
		loc := rule.pattern.FindStringIndex(text)
		if loc == nil {
			continue
		}
		out = append(out, Finding{
			RuleID:      rule.id,
			Description: rule.description,
			Match:       text[loc[0]:loc[1]],
			StartPos:    loc[0],
			EndPos:      loc[1],
			Source:      SourcePromptInjection,
			Confidence:  rule.confidence,
			Tags:        nil,
		})
	}
	return out
}

// detectInstructionOverrides scans for any phrase from the combinatorial
// keyword bank as a lowercased substring. Length-capped to bound work on
// the realtime path. Uses asciiToLower (not strings.ToLower) so byte indices
// found in the lowered copy stay valid as offsets into the original — some
// Unicode runes expand under Unicode-aware lowercasing (e.g. U+023A → U+2C65,
// 2 bytes → 3 bytes), which would let an attacker craft input that panics on
// the substring slice.
func detectInstructionOverrides(text string) []Finding {
	scan := text
	if len(scan) > fuzzyMatchInputCap {
		scan = scan[:fuzzyMatchInputCap]
	}
	lower := asciiToLower(scan)

	var out []Finding
	for _, kw := range overrideKeywords {
		idx := strings.Index(lower, kw)
		if idx < 0 {
			continue
		}
		out = append(out, Finding{
			RuleID:      "pi.instruction-override",
			Description: "Instruction override phrase detected: " + kw,
			Match:       scan[idx : idx+len(kw)],
			StartPos:    idx,
			EndPos:      idx + len(kw),
			Source:      SourcePromptInjection,
			Confidence:  0.9,
			Tags:        nil,
		})
		// One finding is enough for this family; multiple keywords would
		// just produce overlapping findings the dedup pass would drop.
		break
	}
	return out
}

// detectDelimiterInjection looks for forged role/instruction delimiters.
func detectDelimiterInjection(text string) []Finding {
	loc := delimiterPatterns.FindStringIndex(text)
	if loc == nil {
		return nil
	}
	return []Finding{{
		RuleID:      "pi.delimiter-injection",
		Description: "Forged role or instruction delimiter detected",
		Match:       text[loc[0]:loc[1]],
		StartPos:    loc[0],
		EndPos:      loc[1],
		Source:      SourcePromptInjection,
		Confidence:  0.8,
		Tags:        nil,
	}}
}
