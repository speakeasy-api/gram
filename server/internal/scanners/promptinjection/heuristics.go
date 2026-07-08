package promptinjection

import (
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

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
			description: "Long encoded blob with explicit decode/eval intent",
			family:      familyEncodedPayload,
			confidence:  0.7,
			pattern:     regexp.MustCompile(`(?i)\b(decode|eval|execute)\b.{0,30}\b(base64|hex|encoded|payload|following|this)\b.{0,40}([A-Za-z0-9+/]{40,}={0,2}|[0-9a-fA-F]{64,})`),
		},
	}

	delimiterPatterns = regexp.MustCompile(
		`(?i)(<\s*/?\s*(system|assistant|user|instructions)\s*>|###\s*system\b|\[INST\]|<\|im_start\|>|<\|im_end\|>)`,
	)

	overrideKeywords = generateOverrideKeywords()
}

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

func runHeuristics(text string) []scanners.Finding {
	if text == "" {
		return nil
	}

	var findings []scanners.Finding
	findings = append(findings, detectInstructionOverrides(text)...)
	findings = append(findings, runFamily(text, familyRoleHijack)...)
	findings = append(findings, runFamily(text, familySystemPromptLeak)...)
	findings = append(findings, detectDelimiterInjection(text)...)
	findings = append(findings, runFamily(text, familyEncodedPayload)...)
	return findings
}

func runFamily(text string, fam ruleFamily) []scanners.Finding {
	var out []scanners.Finding
	for _, rule := range heuristicRules {
		if rule.family != fam {
			continue
		}
		loc := rule.pattern.FindStringIndex(text)
		if loc == nil {
			continue
		}
		ruleID, description := Describe()
		out = append(out, scanners.Finding{
			RuleID:              ruleID,
			Description:         description,
			Match:               text[loc[0]:loc[1]],
			StartPos:            loc[0],
			EndPos:              loc[1],
			Source:              Source,
			Confidence:          rule.confidence,
			Tags:                []string{},
			DeadLetterReason:    "",
			McpLookupToolCallID: "",
			SpanGroupKey:        "",
			Field:               "",
			Path:                "",
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
func detectInstructionOverrides(text string) []scanners.Finding {
	scan := text
	if len(scan) > fuzzyMatchInputCap {
		scan = scan[:fuzzyMatchInputCap]
	}
	lower := asciiToLower(scan)

	var out []scanners.Finding
	for _, kw := range overrideKeywords {
		idx := strings.Index(lower, kw)
		if idx < 0 {
			continue
		}
		ruleID, description := Describe()
		out = append(out, scanners.Finding{
			RuleID:              ruleID,
			Description:         description,
			Match:               scan[idx : idx+len(kw)],
			StartPos:            idx,
			EndPos:              idx + len(kw),
			Source:              Source,
			Confidence:          0.9,
			Tags:                []string{},
			DeadLetterReason:    "",
			McpLookupToolCallID: "",
			SpanGroupKey:        "",
			Field:               "",
			Path:                "",
		})
		// One finding is enough for this family; multiple keywords would
		// just produce overlapping findings the dedup pass would drop.
		break
	}
	return out
}

func detectDelimiterInjection(text string) []scanners.Finding {
	loc := delimiterPatterns.FindStringIndex(text)
	if loc == nil {
		return nil
	}
	ruleID, description := Describe()
	return []scanners.Finding{{
		RuleID:              ruleID,
		Description:         description,
		Match:               text[loc[0]:loc[1]],
		StartPos:            loc[0],
		EndPos:              loc[1],
		Source:              Source,
		Confidence:          0.8,
		Tags:                []string{},
		DeadLetterReason:    "",
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	}}
}
