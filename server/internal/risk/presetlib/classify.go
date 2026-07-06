// Package presetlib classifies risk findings from ANY detection source against a
// git-versioned catalog of known-benign values (test credit cards, example API
// keys/tokens, module hashes, placeholder emails). It is the cross-source
// counterpart to presidiofp (which is PII/entity-only).
//
// The catalog itself is DATA, not Go source: it lives in the embedded
// data/catalog.yaml and is loaded, compiled, and evaluated here. This package is
// a leaf domain package with no Temporal/activity dependencies, so it can be
// reused from the scanner merge seam, from the offline sweep tool, or anywhere a
// stored finding needs to be re-evaluated.
package presetlib

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed data/catalog.yaml
var catalogYAML []byte

// Match is the subset of a finding needed to classify it: its detection source,
// canonical rule id, and the matched value.
type Match struct {
	Source string
	RuleID string
	Value  string
}

var (
	loadOnce   sync.Once
	compiled   []compiledRule
	ruleIDList []string
	globList   []string
	version    string
	errLoad    error
)

// load parses and compiles the embedded catalog exactly once. On any error the
// compiled set is left empty so Reason becomes a no-op (mirrors presidiofp's
// no-op-on-corrupt-embedded-data stance); Validate surfaces the error for the
// integrity test.
func load() {
	loadOnce.Do(func() {
		var cat Catalog
		if err := yaml.Unmarshal(catalogYAML, &cat); err != nil {
			errLoad = err
			return
		}
		sum := sha256.Sum256(catalogYAML)
		version = hex.EncodeToString(sum[:])[:8]

		ridSet := map[string]struct{}{}
		globSet := map[string]struct{}{}
		seenID := map[string]struct{}{}
		for _, r := range cat.Rules {
			if _, dup := seenID[r.ID]; dup {
				errLoad = &dupIDError{id: r.ID}
				compiled = nil
				return
			}
			seenID[r.ID] = struct{}{}

			cr, err := compileRule(r)
			if err != nil {
				errLoad = err
				compiled = nil
				return
			}
			compiled = append(compiled, cr)
			for _, id := range r.RuleIDs {
				ridSet[id] = struct{}{}
			}
			for _, g := range r.RuleIDGlobs {
				globSet[g] = struct{}{}
			}
		}
		ruleIDList = sortedKeys(ridSet)
		globList = sortedKeys(globSet)
	})
}

type dupIDError struct{ id string }

func (e *dupIDError) Error() string { return "duplicate rule id: " + e.id }

// Reason returns the catalog reason a finding is treated as a false positive, or
// "" when it is a real finding (or the catalog failed to load). The first
// matching rule wins.
func Reason(m Match) string {
	load()
	if errLoad != nil {
		return ""
	}
	for i := range compiled {
		if compiled[i].matches(m) {
			return compiled[i].rule.Reason
		}
	}
	return ""
}

// RuleIDs returns the sorted set of exact rule ids the catalog scopes to. The
// sweep tool uses it to narrow its DB scan (rule_id = ANY(...)). It does NOT
// include glob-scoped families — see RuleIDGlobs.
func RuleIDs() []string {
	load()
	return append([]string(nil), ruleIDList...)
}

// RuleIDGlobs returns the sorted set of "prefix*" rule-id globs the catalog
// scopes to (e.g. "secret.stripe_*"). The sweep tool translates each to a SQL
// LIKE 'prefix%' clause to widen its scan beyond the exact RuleIDs.
func RuleIDGlobs() []string {
	load()
	return append([]string(nil), globList...)
}

// Version returns a short checksum of the embedded catalog, for stamping into
// false_positive_reason as provenance (e.g. "preset:ab12cd34").
func Version() string {
	load()
	return version
}

// Validate parses and compiles the embedded catalog and returns any structural
// error (bad YAML, unknown matcher type, uncompilable regex, duplicate id). The
// integrity test calls it so a bad catalog edit fails CI, not production.
func Validate() error {
	load()
	return errLoad
}

// EntryView is a read-only, display-safe description of one catalog rule, for
// surfacing the library through the management API. Samples are the rule's own
// example values/patterns — all published test/documentation data, never real
// secrets — so they are safe to return to any org member who can view the
// exclusions UI.
type EntryView struct {
	ID          string
	Category    string
	Reason      string
	Description string
	MatchType   string
	Sources     []string
	RuleIDs     []string
	RuleIDGlobs []string
	Samples     []string
}

// Entries returns a display view of every catalog rule in file order. Returns
// nil when the catalog failed to load.
func Entries() []EntryView {
	load()
	if errLoad != nil {
		return nil
	}
	out := make([]EntryView, 0, len(compiled))
	for i := range compiled {
		r := compiled[i].rule
		out = append(out, EntryView{
			ID:          r.ID,
			Category:    r.Category,
			Reason:      r.Reason,
			Description: r.Description,
			MatchType:   r.Match.Type,
			Sources:     append([]string(nil), r.Sources...),
			RuleIDs:     append([]string(nil), r.RuleIDs...),
			RuleIDGlobs: append([]string(nil), r.RuleIDGlobs...),
			Samples:     ruleSamples(r),
		})
	}
	return out
}

// ruleSamples returns display-safe example VALUES for a rule: the literal values
// for exact/prefix/digits. Regex patterns and the email delegation carry no
// user-facing example values (and raw regex would leak matcher internals), so
// those return nothing.
func ruleSamples(r Rule) []string {
	switch r.Match.Type {
	case MatchExact, MatchPrefix, MatchDigits:
		return append([]string(nil), r.Match.Values...)
	default:
		return nil
	}
}

// --- small helpers ---

func toSet(xs []string) map[string]struct{} {
	if len(xs) == 0 {
		return nil
	}
	s := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		s[x] = struct{}{}
	}
	return s
}

func lowerAll(xs []string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = strings.ToLower(x)
	}
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func stripNonDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// luhnValid reports whether the all-digit string passes the Luhn checksum. An
// empty string is not valid.
func luhnValid(digits string) bool {
	if digits == "" {
		return false
	}
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}
