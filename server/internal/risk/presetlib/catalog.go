package presetlib

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/risk/presidiofp"
)

// Catalog is the parsed preset false-positive library (the embedded
// data/catalog.yaml). It is data, never Go source: entries live in the YAML,
// this package only loads, compiles, and evaluates them.
type Catalog struct {
	// Version is the schema-format version; bump on a breaking format change.
	Version int    `yaml:"version"`
	Rules   []Rule `yaml:"rules"`
}

// Rule is one catalog entry. A finding matches when ALL of Sources,
// RuleIDs/RuleIDGlobs, and Match match; empty Sources/RuleIDs/RuleIDGlobs mean
// "no constraint on that axis". The first matching rule (file order) wins and
// its Reason is returned.
type Rule struct {
	// ID is a stable unique slug; it is stamped into false_positive_reason.
	ID string `yaml:"id"`
	// Category is a human-facing group label (e.g. "Test credit cards") used only
	// to organize the catalog for display. Not used at runtime matching.
	Category string `yaml:"category"`
	// Description is human rationale (why this value is noise). Not used at runtime.
	Description string `yaml:"description"`
	// Reason is the label returned by Reason() when this rule fires.
	Reason string `yaml:"reason"`
	// Sources scopes the rule to Finding.Source values (e.g. "gitleaks"). Empty = any.
	Sources []string `yaml:"sources"`
	// RuleIDs scopes to exact Finding.RuleID values. Empty = any.
	RuleIDs []string `yaml:"rule_ids"`
	// RuleIDGlobs scopes via "prefix*" globs (e.g. "secret.stripe_*"). Empty = any.
	RuleIDGlobs []string `yaml:"rule_id_globs"`
	Match       Matcher  `yaml:"match"`
}

// Matcher decides whether a finding's value is a known false positive. Only the
// fields relevant to Type are read.
type Matcher struct {
	// Type is one of Match* below.
	Type string `yaml:"type"`
	// CaseInsensitive lowercases both sides for exact/prefix matching.
	CaseInsensitive bool `yaml:"case_insensitive"`
	// Values is the literal set for exact/prefix, or the digit-string set for digits.
	Values []string `yaml:"values"`
	// Patterns are RE2 regexes for the regex matcher (compiled once at load).
	Patterns []string `yaml:"patterns"`
	// Luhn (digits): also match any Luhn-valid PAN whose digit count is in [MinLen,MaxLen].
	Luhn   bool `yaml:"luhn"`
	MinLen int  `yaml:"min_len"`
	MaxLen int  `yaml:"max_len"`
}

// Matcher type discriminants.
const (
	MatchExact  = "exact"  // value ∈ Values (optional CaseInsensitive)
	MatchPrefix = "prefix" // value has a Values prefix (optional CaseInsensitive)
	MatchRegex  = "regex"  // value matches any Patterns entry
	MatchDigits = "digits" // strip non-digits → in Values, or (Luhn && len∈[min,max] && Luhn-valid)
	MatchEmail  = "email"  // delegates to presidiofp placeholder email catalog
)

// compiledRule is a Rule with its match scopes precomputed and regexes compiled.
type compiledRule struct {
	rule     Rule
	sources  map[string]struct{}
	ruleIDs  map[string]struct{}
	globs    []string // prefix globs with the trailing '*' stripped
	patterns []*regexp.Regexp
	values   []string // Values, lowercased when CaseInsensitive (exact/prefix)
}

// compileRule validates a rule and precomputes its matcher. A returned error
// means the catalog is malformed (caught by the integrity test, not prod).
func compileRule(r Rule) (compiledRule, error) {
	cr := compiledRule{
		rule:     r,
		sources:  nil,
		ruleIDs:  nil,
		globs:    nil,
		patterns: nil,
		values:   nil,
	}
	if strings.TrimSpace(r.ID) == "" {
		return cr, fmt.Errorf("rule with empty id")
	}
	if strings.TrimSpace(r.Reason) == "" {
		return cr, fmt.Errorf("rule %q: empty reason", r.ID)
	}
	cr.sources = toSet(r.Sources)
	cr.ruleIDs = toSet(r.RuleIDs)
	for _, g := range r.RuleIDGlobs {
		// A glob must be a non-empty prefix ending in "*". Reject "" and "*",
		// which compile to an empty prefix — strings.HasPrefix(id, "") is always
		// true, so they would suppress findings across every rule id. Precision guard.
		prefix, ok := strings.CutSuffix(g, "*")
		if !ok || prefix == "" {
			return cr, fmt.Errorf("rule %q: rule_id_glob %q must be a non-empty prefix ending in %q (would match every rule id otherwise)", r.ID, g, "*")
		}
		cr.globs = append(cr.globs, prefix)
	}
	switch r.Match.Type {
	case MatchExact, MatchPrefix:
		if len(r.Match.Values) == 0 {
			return cr, fmt.Errorf("rule %q: %s matcher needs values", r.ID, r.Match.Type)
		}
		// Reject empty values: an empty exact value is dead config and an empty
		// prefix value matches EVERYTHING (strings.HasPrefix(x, "") == true),
		// which would suppress every finding in the rule's scope. Precision guard.
		if slices.Contains(r.Match.Values, "") {
			return cr, fmt.Errorf("rule %q: %s matcher has an empty value (would match indiscriminately)", r.ID, r.Match.Type)
		}
		cr.values = r.Match.Values
		if r.Match.CaseInsensitive {
			cr.values = lowerAll(cr.values)
		}
	case MatchRegex:
		if len(r.Match.Patterns) == 0 {
			return cr, fmt.Errorf("rule %q: regex matcher needs patterns", r.ID)
		}
		for _, p := range r.Match.Patterns {
			if p == "" {
				return cr, fmt.Errorf("rule %q: regex matcher has an empty pattern", r.ID)
			}
			re, err := regexp.Compile(p)
			if err != nil {
				return cr, fmt.Errorf("rule %q: bad regex %q: %w", r.ID, p, err)
			}
			cr.patterns = append(cr.patterns, re)
		}
	case MatchDigits:
		if len(r.Match.Values) == 0 && !r.Match.Luhn {
			return cr, fmt.Errorf("rule %q: digits matcher needs values or luhn", r.ID)
		}
		// Values are compared against the input with all non-digits stripped, so a
		// value containing non-digits (or empty) can never match — that is a
		// catalog authoring mistake, not a live rule. Reject it here.
		for _, v := range r.Match.Values {
			if v == "" || stripNonDigits(v) != v {
				return cr, fmt.Errorf("rule %q: digits matcher value %q must be non-empty and digits-only", r.ID, v)
			}
		}
		if r.Match.Luhn {
			// A Luhn rule with min_len <= 0 accepts any Luhn-valid run of any
			// length (a 2-digit "18" passes Luhn), suppressing far more than
			// intended. Require an explicit lower bound. max_len == 0 means "no
			// upper bound"; otherwise it must not be below min_len (dead range).
			if r.Match.MinLen <= 0 {
				return cr, fmt.Errorf("rule %q: digits+luhn matcher needs min_len > 0", r.ID)
			}
			if r.Match.MaxLen != 0 && r.Match.MinLen > r.Match.MaxLen {
				return cr, fmt.Errorf("rule %q: digits matcher min_len %d exceeds max_len %d", r.ID, r.Match.MinLen, r.Match.MaxLen)
			}
		}
		cr.values = r.Match.Values // digit strings; case-irrelevant
	case MatchEmail:
		// No per-rule data: delegates to presidiofp's placeholder email catalog.
	default:
		return cr, fmt.Errorf("rule %q: unknown match type %q", r.ID, r.Match.Type)
	}
	return cr, nil
}

// matches reports whether m satisfies this rule across all three axes.
func (c *compiledRule) matches(m Match) bool {
	if len(c.sources) > 0 {
		if _, ok := c.sources[m.Source]; !ok {
			return false
		}
	}
	if !c.ruleIDMatches(m.RuleID) {
		return false
	}
	return c.valueMatches(m.Value)
}

func (c *compiledRule) ruleIDMatches(id string) bool {
	if len(c.ruleIDs) == 0 && len(c.globs) == 0 {
		return true // no rule-id constraint
	}
	if _, ok := c.ruleIDs[id]; ok {
		return true
	}
	for _, g := range c.globs {
		if strings.HasPrefix(id, g) {
			return true
		}
	}
	return false
}

func (c *compiledRule) valueMatches(v string) bool {
	switch c.rule.Match.Type {
	case MatchExact:
		cmp := v
		if c.rule.Match.CaseInsensitive {
			cmp = strings.ToLower(v)
		}
		return slices.Contains(c.values, cmp)
	case MatchPrefix:
		cmp := v
		if c.rule.Match.CaseInsensitive {
			cmp = strings.ToLower(v)
		}
		for _, val := range c.values {
			if strings.HasPrefix(cmp, val) {
				return true
			}
		}
		return false
	case MatchRegex:
		// SUBSTRING semantics: MatchString reports whether the pattern matches
		// ANYWHERE in v, it is NOT anchored to the whole value. Catalog authors
		// who want a whole-value match must anchor their pattern with ^...$
		// themselves (as the shipped entries do). We deliberately do not
		// force-anchor here so an author can still write a substring pattern when
		// that is what they mean; the trade-off is that an unanchored pattern is
		// broader than it may look, so anchoring is the safer default.
		for _, re := range c.patterns {
			if re.MatchString(v) {
				return true
			}
		}
		return false
	case MatchDigits:
		norm := stripNonDigits(v)
		if slices.Contains(c.values, norm) {
			return true
		}
		if c.rule.Match.Luhn && luhnValid(norm) {
			n := len(norm)
			if n >= c.rule.Match.MinLen && (c.rule.Match.MaxLen == 0 || n <= c.rule.Match.MaxLen) {
				return true
			}
		}
		return false
	case MatchEmail:
		return presidiofp.Reason(presidiofp.EntityTypeEmailAddress, v) != ""
	}
	return false
}
