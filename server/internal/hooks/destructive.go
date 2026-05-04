package hooks

import (
	"regexp"
	"strings"
)

// destructivePattern is one curated rule for matching destructive command
// content inside a tool call's input. Patterns are tool-agnostic — they run
// against every string value found in payload.ToolInput so a destructive
// shell snippet trips regardless of whether it lives in Bash's "command"
// field or an MCP tool's "query" / "args" field.
//
// Guard, when non-nil, is an "innocence" regex: a candidate string only
// counts as destructive if Regex matches AND Guard does NOT. Used to express
// rules like "DELETE FROM is destructive only if there's no WHERE clause"
// without needing negative lookahead (which Go's RE2 doesn't support).
type destructivePattern struct {
	Category string
	Name     string
	Regex    *regexp.Regexp
	Guard    *regexp.Regexp
}

// destructivePatterns is the curated v1 set covering the four documented
// categories: Shell, Git, Database, Cloud. The list is intentionally small
// and in-code — see the PR for the rationale (string regex matching is a
// first-line guardrail, not a full sandbox).
type destructiveSpec struct {
	Category string
	Name     string
	Pattern  string
	Guard    string
}

var destructivePatterns = compileDestructivePatterns([]destructiveSpec{
	// Shell.
	{Category: "shell", Name: "rm-rf", Pattern: `(?i)\brm\s+(?:-[a-zA-Z]*r[a-zA-Z]*f|-[a-zA-Z]*f[a-zA-Z]*r|--recursive\s+--force|--force\s+--recursive)\b`, Guard: ""},
	{Category: "shell", Name: "dd", Pattern: `(?i)\bdd\b\s+(?:[a-z]+=)`, Guard: ""},
	{Category: "shell", Name: "mkfs", Pattern: `(?i)\bmkfs(?:\.\w+)?\b`, Guard: ""},
	{Category: "shell", Name: "fork-bomb", Pattern: `:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`, Guard: ""},
	{Category: "shell", Name: "chmod-recursive", Pattern: `(?i)\bchmod\s+(?:-[a-z]*R[a-z]*|--recursive)\b`, Guard: ""},
	{Category: "shell", Name: "chown-recursive", Pattern: `(?i)\bchown\s+(?:-[a-z]*R[a-z]*|--recursive)\b`, Guard: ""},
	{Category: "shell", Name: "sudo", Pattern: `(?i)\bsudo\b`, Guard: ""},

	// Git.
	{Category: "git", Name: "push-force", Pattern: `(?i)\bgit\s+push\b[^\n]*(?:--force\b|--force-with-lease\b|\s-f\b)`, Guard: ""},
	{Category: "git", Name: "reset-hard", Pattern: `(?i)\bgit\s+reset\s+--hard\b`, Guard: ""},
	{Category: "git", Name: "clean-force", Pattern: `(?i)\bgit\s+clean\s+(?:-[a-z]*f[a-z]*|--force)\b`, Guard: ""},
	{Category: "git", Name: "branch-delete-force", Pattern: `(?i)\b(?:git\s+)?branch\s+-D\b`, Guard: ""},

	// Database.
	{Category: "database", Name: "drop", Pattern: `(?i)\bDROP\s+(?:TABLE|DATABASE|SCHEMA|INDEX)\b`, Guard: ""},
	{Category: "database", Name: "truncate", Pattern: `(?i)\bTRUNCATE\b`, Guard: ""},
	// "Unguarded" DELETE: matches DELETE FROM only when the same string has
	// no WHERE clause (Go's RE2 has no negative lookahead, so the absence
	// check is expressed via Guard).
	{Category: "database", Name: "delete-without-where", Pattern: `(?i)\bDELETE\s+FROM\b`, Guard: `(?i)\bWHERE\b`},
	{Category: "database", Name: "dropdb", Pattern: `(?i)\bdropdb\b`, Guard: ""},

	// Cloud.
	{Category: "cloud", Name: "aws-ec2-terminate", Pattern: `(?i)\baws\s+ec2\s+terminate-instances\b`, Guard: ""},
	{Category: "cloud", Name: "aws-s3-rb", Pattern: `(?i)\baws\s+s3\s+rb\b`, Guard: ""},
	{Category: "cloud", Name: "gcloud-projects-delete", Pattern: `(?i)\bgcloud\s+projects\s+delete\b`, Guard: ""},
	{Category: "cloud", Name: "kubectl-delete-namespace", Pattern: `(?i)\bkubectl\s+delete\s+(?:ns|namespace)\b`, Guard: ""},
	{Category: "cloud", Name: "kubectl-delete-workload", Pattern: `(?i)\bkubectl\s+delete\s+(?:deployment|sts|statefulset|daemonset|pv|pvc)\b`, Guard: ""},
})

func compileDestructivePatterns(specs []destructiveSpec) []destructivePattern {
	out := make([]destructivePattern, 0, len(specs))
	for _, spec := range specs {
		p := destructivePattern{
			Category: spec.Category,
			Name:     spec.Name,
			Regex:    regexp.MustCompile(spec.Pattern),
			Guard:    nil,
		}
		if spec.Guard != "" {
			p.Guard = regexp.MustCompile(spec.Guard)
		}
		out = append(out, p)
	}
	return out
}

// flattenToolInputStrings walks a tool call's input and returns every string
// value reachable via map values, slice elements, or pointer dereferences.
// Keys are not included (a map key like "DROP TABLE" would be a strange but
// not actually destructive thing to express; the user's threat model is the
// values). Numbers, bools, and nil are ignored.
func flattenToolInputStrings(input any) []string {
	if input == nil {
		return nil
	}

	out := make([]string, 0, 4)
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			if t != "" {
				out = append(out, t)
			}
		case map[string]any:
			for _, val := range t {
				walk(val)
			}
		case []any:
			for _, val := range t {
				walk(val)
			}
		}
	}
	walk(input)
	return out
}

// scanForDestructive returns the first matching destructive pattern found in
// any string value reachable from input, or zero-value + false when nothing
// matches. The first-match-wins ordering keeps the hot path predictable: we
// stop scanning as soon as a deny reason is established.
func scanForDestructive(input any) (destructivePattern, bool) {
	for _, str := range flattenToolInputStrings(input) {
		if matched, ok := matchDestructiveString(str); ok {
			return matched, true
		}
	}
	return destructivePattern{}, false //nolint:exhaustruct // sentinel zero value; second return signals no match
}

// matchDestructiveString applies the curated pattern set to a single string.
// Exposed for tests; not exported.
func matchDestructiveString(s string) (destructivePattern, bool) {
	if s == "" {
		return destructivePattern{}, false //nolint:exhaustruct // sentinel zero value; second return signals no match
	}
	for _, p := range destructivePatterns {
		if !p.Regex.MatchString(s) {
			continue
		}
		// Guard expresses an "innocence" check — when present and matched,
		// the candidate string is treated as non-destructive (e.g. a DELETE
		// with a WHERE clause). Lets us encode the "unguarded DELETE" rule
		// without negative lookahead.
		if p.Guard != nil && p.Guard.MatchString(s) {
			continue
		}
		return p, true
	}
	return destructivePattern{}, false //nolint:exhaustruct // sentinel zero value; second return signals no match
}

// FullName returns "category/name" for use in deny reasons and audit subjects.
func (p destructivePattern) FullName() string {
	if p.Category == "" && p.Name == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(p.Category)
	b.WriteByte('/')
	b.WriteString(p.Name)
	return b.String()
}
