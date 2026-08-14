// Package catalog loads the embedded rule documents that define every
// diagnostic sqlclint can report and every exemption category an annotation may
// name.
//
// The documents are the single source of truth for the vocabulary: the exemption
// categories accepted in `sqlclint:ignore` annotations are exactly the ids of the
// exemption documents, so a category cannot exist without an explanation of what
// it claims.
package catalog

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/speakeasy-api/gram/sqlclint/docs"
)

// Kind distinguishes the two sorts of document in the catalog.
type Kind string

const (
	// KindDiagnostic is a problem sqlclint reports.
	KindDiagnostic Kind = "diagnostic"

	// KindExemption is a category an annotation may cite to silence a diagnostic.
	KindExemption Kind = "exemption"
)

// Diagnostic ids. These are referenced from the rule engine, and the catalog
// test asserts each one resolves to a document.
const (
	MissingTenantScope       = "missing-tenant-scope"
	WrongTenantColumn        = "wrong-tenant-column"
	NullableTenantParam      = "nullable-tenant-param"
	UnparseableQuery         = "unparseable-query"
	UnknownTableReference    = "unknown-table-reference"
	UnknownExemptionCategory = "unknown-exemption-category"
	MissingExemptionReason   = "missing-exemption-reason"
	RedundantExemption       = "redundant-exemption"
	StaleIgnoreEntry         = "stale-ignore-entry"
	ModifiedIgnoredQuery     = "modified-ignored-query"
)

// DiagnosticIDs lists every diagnostic the engine can emit. The catalog test
// checks this against the documents in both directions, so a diagnostic cannot
// be added without a document and a document cannot be orphaned.
var DiagnosticIDs = []string{
	MissingTenantScope,
	WrongTenantColumn,
	NullableTenantParam,
	UnparseableQuery,
	UnknownTableReference,
	UnknownExemptionCategory,
	MissingExemptionReason,
	RedundantExemption,
	StaleIgnoreEntry,
	ModifiedIgnoredQuery,
}

// Rule is one parsed catalog document.
type Rule struct {
	// ID is the rule identifier, equal to the document's filename stem.
	ID string

	// Kind is whether this document describes a diagnostic or an exemption.
	Kind Kind

	// Summary is the one-line description shown by `sqlclint rules`.
	Summary string

	// Severity is set on diagnostics only and is always "error" today.
	Severity string

	// SilencedBy lists the exemption ids that may silence this diagnostic. It is
	// empty on exemptions, and deliberately empty on diagnostics that no
	// annotation may suppress.
	SilencedBy []string

	// Body is the markdown following the frontmatter, rendered by `sqlclint rule`.
	Body string
}

// frontmatter mirrors the YAML block at the top of each document.
type frontmatter struct {
	ID         string   `yaml:"id"`
	Kind       string   `yaml:"kind"`
	Summary    string   `yaml:"summary"`
	Severity   string   `yaml:"severity"`
	SilencedBy []string `yaml:"silenced_by"`
}

// Catalog is the loaded set of rule documents.
type Catalog struct {
	byID map[string]Rule
}

// Load reads and validates every embedded document. It fails rather than
// degrading: a catalog that is missing or malformed would let the engine cite
// rule ids it cannot explain.
func Load() (*Catalog, error) {
	entries, err := fs.ReadDir(docs.Rules, "rules")
	if err != nil {
		return nil, fmt.Errorf("read rules dir: %w", err)
	}

	byID := make(map[string]Rule, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		name := path.Join("rules", e.Name())
		raw, err := fs.ReadFile(docs.Rules, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		rule, err := parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}

		stem := strings.TrimSuffix(e.Name(), ".md")
		if rule.ID != stem {
			return nil, fmt.Errorf("parse %s: frontmatter id %q does not match filename", name, rule.ID)
		}
		if _, dup := byID[rule.ID]; dup {
			return nil, fmt.Errorf("parse %s: duplicate rule id %q", name, rule.ID)
		}

		byID[rule.ID] = rule
	}

	if len(byID) == 0 {
		return nil, fmt.Errorf("rule catalog is empty")
	}

	c := &Catalog{byID: byID}

	// Cross-reference last, once every id is known, so a document may cite any
	// other regardless of directory order.
	for _, r := range c.All() {
		for _, s := range r.SilencedBy {
			target, ok := byID[s]
			if !ok {
				return nil, fmt.Errorf("rule %q: silenced_by names unknown rule %q", r.ID, s)
			}
			if target.Kind != KindExemption {
				return nil, fmt.Errorf("rule %q: silenced_by names %q, which is a %s, not an exemption", r.ID, s, target.Kind)
			}
		}
	}

	return c, nil
}

var frontmatterDelim = []byte("---\n")

// parse splits a document into its YAML frontmatter and markdown body, and
// checks the fields each Kind requires.
func parse(raw []byte) (Rule, error) {
	raw = bytes.TrimLeft(raw, "\ufeff \t\r\n")
	if !bytes.HasPrefix(raw, frontmatterDelim) {
		return Rule{}, fmt.Errorf("document does not start with a --- frontmatter block")
	}

	rest := raw[len(frontmatterDelim):]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return Rule{}, fmt.Errorf("frontmatter block is not terminated by ---")
	}

	var fm frontmatter
	if err := yaml.Unmarshal(rest[:end+1], &fm); err != nil {
		return Rule{}, fmt.Errorf("decode frontmatter: %w", err)
	}

	body := strings.TrimSpace(string(rest[end+len("\n---\n"):]))

	switch {
	case fm.ID == "":
		return Rule{}, fmt.Errorf("frontmatter is missing id")
	case fm.Summary == "":
		return Rule{}, fmt.Errorf("frontmatter is missing summary")
	case body == "":
		return Rule{}, fmt.Errorf("document has no body")
	}

	kind := Kind(fm.Kind)
	switch kind {
	case KindDiagnostic:
		if fm.Severity == "" {
			return Rule{}, fmt.Errorf("diagnostic %q is missing severity", fm.ID)
		}
	case KindExemption:
		if fm.Severity != "" {
			return Rule{}, fmt.Errorf("exemption %q must not set severity", fm.ID)
		}
		if len(fm.SilencedBy) > 0 {
			return Rule{}, fmt.Errorf("exemption %q must not set silenced_by", fm.ID)
		}
	default:
		return Rule{}, fmt.Errorf("frontmatter kind %q must be %q or %q", fm.Kind, KindDiagnostic, KindExemption)
	}

	return Rule{
		ID:         fm.ID,
		Kind:       kind,
		Summary:    fm.Summary,
		Severity:   fm.Severity,
		SilencedBy: fm.SilencedBy,
		Body:       body,
	}, nil
}

// Lookup returns the rule with the given id.
func (c *Catalog) Lookup(id string) (Rule, bool) {
	r, ok := c.byID[id]
	return r, ok
}

// All returns every rule, ordered by id.
func (c *Catalog) All() []Rule {
	out := make([]Rule, 0, len(c.byID))
	for _, r := range c.byID {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Rule) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// ByKind returns every rule of one kind, ordered by id.
func (c *Catalog) ByKind(k Kind) []Rule {
	var out []Rule
	for _, r := range c.All() {
		if r.Kind == k {
			out = append(out, r)
		}
	}
	return out
}

// IDs returns the ids of every rule of one kind, ordered.
func (c *Catalog) IDs(k Kind) []string {
	rules := c.ByKind(k)
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.ID)
	}
	return out
}

// IsExemption reports whether id names an exemption category, which is what
// annotation parsing validates against.
func (c *Catalog) IsExemption(id string) bool {
	r, ok := c.byID[id]
	return ok && r.Kind == KindExemption
}
