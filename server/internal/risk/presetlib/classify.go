// Package presetlib classifies risk findings from ANY detection source against a
// git-versioned catalog of known-benign values (test credit cards, example API
// keys/tokens, module hashes, placeholder emails). It is the cross-source
// counterpart to presidiofp (which is PII/entity-only).
//
// The catalog itself is DATA, not Go source: it lives in the embedded
// data/catalog.yaml. Callers construct a *Library once at startup via New (which
// fails fast on a malformed catalog) and inject it where needed. This package is
// a leaf domain package with no Temporal/activity dependencies, so the Library
// can be reused from the scanner merge seam, from the offline sweep tool, or
// anywhere a stored finding needs to be re-evaluated.
package presetlib

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed data/catalog.yaml
var catalogYAML []byte

// Match is the subset of a finding needed to classify it: its detection source,
// canonical rule id, and the matched value. It is a deliberate standalone DTO
// rather than risk_analysis.Finding: presetlib is a leaf package (the scanner's
// risk_analysis package imports it), so referencing Finding here would create an
// import cycle. Keeping a minimal input also lets the offline sweep tool build
// one straight from DB columns.
type Match struct {
	Source string
	RuleID string
	Value  string
}

// Library is a parsed, compiled preset false-positive catalog. Construct it once
// with New and share it; all methods are read-only and safe for concurrent use.
type Library struct {
	rules   []compiledRule
	ruleIDs []string // sorted, deduped exact rule ids across all rules
	globs   []string // sorted, deduped "prefix*" globs across all rules
	version string
}

// New parses and compiles the embedded catalog. It returns an error for a
// malformed catalog (bad YAML, unknown key, unknown matcher type, uncompilable
// regex, invalid glob, duplicate id) so the caller can halt startup rather than
// run with a silently-empty catalog.
func New() (*Library, error) {
	var cat Catalog
	// KnownFields(true): reject unknown YAML keys so a misspelled scope field
	// (e.g. "rule_id_glob" instead of "rule_id_globs") is a hard error rather than
	// silently decoding to a zero value that defaults to "any" and widens
	// suppression.
	dec := yaml.NewDecoder(bytes.NewReader(catalogYAML))
	dec.KnownFields(true)
	if err := dec.Decode(&cat); err != nil {
		return nil, fmt.Errorf("parse preset catalog: %w", err)
	}

	lib := &Library{rules: nil, ruleIDs: nil, globs: nil, version: ""}
	ridSet := map[string]struct{}{}
	globSet := map[string]struct{}{}
	seen := map[string]struct{}{}
	for _, r := range cat.Rules {
		if _, dup := seen[r.ID]; dup {
			return nil, &dupIDError{id: r.ID}
		}
		seen[r.ID] = struct{}{}

		cr, err := compileRule(r)
		if err != nil {
			return nil, err
		}
		lib.rules = append(lib.rules, cr)
		for _, id := range r.RuleIDs {
			ridSet[id] = struct{}{}
		}
		for _, g := range r.RuleIDGlobs {
			globSet[g] = struct{}{}
		}
	}
	lib.ruleIDs = slices.Sorted(maps.Keys(ridSet))
	lib.globs = slices.Sorted(maps.Keys(globSet))

	sum := sha256.Sum256(catalogYAML)
	lib.version = hex.EncodeToString(sum[:])[:8]
	return lib, nil
}

type dupIDError struct{ id string }

func (e *dupIDError) Error() string { return "duplicate rule id: " + e.id }

// Reason returns the catalog reason a finding is treated as a false positive, or
// "" when it is a real finding. The first matching rule wins.
func (l *Library) Reason(m Match) string {
	for i := range l.rules {
		if l.rules[i].matches(m) {
			return l.rules[i].rule.Reason
		}
	}
	return ""
}

// RuleIDs returns the sorted set of exact rule ids the catalog scopes to. The
// sweep tool uses it to narrow its DB scan (rule_id = ANY(...)). It does NOT
// include glob-scoped families — see RuleIDGlobs.
func (l *Library) RuleIDs() []string {
	return slices.Clone(l.ruleIDs)
}

// RuleIDGlobs returns the sorted set of "prefix*" rule-id globs the catalog
// scopes to (e.g. "secret.stripe_*"). The sweep tool translates each to a SQL
// LIKE 'prefix%' clause to widen its scan beyond the exact RuleIDs.
func (l *Library) RuleIDGlobs() []string {
	return slices.Clone(l.globs)
}

// Version returns a short checksum of the embedded catalog, for stamping into
// false_positive_reason as provenance (e.g. "preset:ab12cd34").
func (l *Library) Version() string {
	return l.version
}

// EntryView is a read-only, display-safe description of one catalog rule, for
// surfacing the library through the management API. It deliberately omits
// engine-internal identifiers (detection sources, rule ids, matcher type).
// Samples are the rule's own example values — published test/documentation data,
// never real secrets — so they are safe to show any org member who can view the
// exclusions UI.
type EntryView struct {
	ID          string
	Category    string
	Reason      string
	Description string
	Samples     []string
}

// Entries returns a display view of every catalog rule in file order.
func (l *Library) Entries() []EntryView {
	out := make([]EntryView, 0, len(l.rules))
	for i := range l.rules {
		r := l.rules[i].rule
		out = append(out, EntryView{
			ID:          r.ID,
			Category:    r.Category,
			Reason:      r.Reason,
			Description: r.Description,
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
		return slices.Clone(r.Match.Values)
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
