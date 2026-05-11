package risk_analysis

import (
	"regexp"
	"sort"
	"strings"
)

// SourceCLIDestructive is the policy source value that flags tool calls whose
// arguments contain a curated destructive CLI command pattern (rm -rf, git
// push --force, DROP TABLE, ...). Mirrors SourceDestructiveTool but is
// content-driven instead of annotation-driven and applies to native tools
// (Bash, run_terminal_cmd) as well as MCP-routed calls whose arguments
// happen to carry a destructive payload.
const SourceCLIDestructive = "cli_destructive"

// cliDestructivePattern is one curated rule for matching destructive command
// content inside a recorded tool call's arguments JSON. Patterns are
// tool-name-agnostic — they run against every string value reachable from
// the parsed arguments so a destructive shell snippet trips regardless of
// whether it lives in a Bash tool's "command" field or an MCP query tool's
// "query" / "args" field.
//
// Guard, when non-nil, is an "innocence" regex: a candidate string only
// counts as destructive when Regex matches AND Guard does NOT. Used to
// express rules like "DELETE FROM is destructive only if there's no WHERE
// clause" without negative lookahead (which Go's RE2 doesn't support).
type cliDestructivePattern struct {
	Category string
	Name     string
	Regex    *regexp.Regexp
	Guard    *regexp.Regexp
}

// FullName returns "category/name" for use in rule_ids and finding metadata.
func (p cliDestructivePattern) FullName() string {
	if p.Category == "" && p.Name == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(p.Category)
	b.WriteByte('/')
	b.WriteString(p.Name)
	return b.String()
}

type cliDestructiveSpec struct {
	Category string
	Name     string
	Pattern  string
	Guard    string
}

// cliDestructivePatterns is the curated v1 set covering shell, git, database,
// and cloud CLI commands. Kept in-code rather than per-policy because the
// trade-off here is "first-line guardrail, not full sandbox" — see PR
// description for the rationale. Adding a new pattern is a code change so
// reviewers see every addition.
//
// Order matters: matchCLIDestructiveString returns the first match, so
// **specific** patterns must come before broader catch-alls in the same
// category. Within "shell", e.g., chmod-recursive precedes the bare-`sudo`
// catch-all so `sudo chmod -R 777 /` reports as shell/chmod-recursive
// rather than shell/sudo.
var cliDestructivePatterns = compileCLIDestructivePatterns([]cliDestructiveSpec{
	// Shell.
	{Category: "shell", Name: "rm-rf", Pattern: `(?i)\brm\s+(?:-[a-zA-Z]*r[a-zA-Z]*f|-[a-zA-Z]*f[a-zA-Z]*r|--recursive\s+--force|--force\s+--recursive)\b`, Guard: ""},
	{Category: "shell", Name: "dd", Pattern: `(?i)\bdd\b\s+(?:[a-z]+=)`, Guard: ""},
	{Category: "shell", Name: "mkfs", Pattern: `(?i)\bmkfs(?:\.\w+)?\b`, Guard: ""},
	{Category: "shell", Name: "fork-bomb", Pattern: `:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`, Guard: ""},
	{Category: "shell", Name: "chmod-recursive", Pattern: `(?i)\bchmod\s+(?:-[a-z]*R[a-z]*|--recursive)\b`, Guard: ""},
	{Category: "shell", Name: "chown-recursive", Pattern: `(?i)\bchown\s+(?:-[a-z]*R[a-z]*|--recursive)\b`, Guard: ""},
	// `sudo` requires a following token, so a bare-`sudo` mention won't trip
	// — but prose like "sudo grants" will. Acceptable because we only scan
	// tool-call argument values, not free chat text. Declared after the more
	// specific shell patterns above so `sudo chmod -R 777 /` reports as
	// chmod-recursive, not as sudo.
	{Category: "shell", Name: "sudo", Pattern: `(?i)\bsudo\s+\S+`, Guard: ""},

	// Git.
	{Category: "git", Name: "push-force", Pattern: `(?i)\bgit\s+push\b[^\n]*(?:--force\b|--force-with-lease\b|\s-f\b)`, Guard: ""},
	{Category: "git", Name: "reset-hard", Pattern: `(?i)\bgit\s+reset\s+--hard\b`, Guard: ""},
	{Category: "git", Name: "clean-force", Pattern: `(?i)\bgit\s+clean\s+(?:-[a-z]*f[a-z]*|--force)\b`, Guard: ""},
	{Category: "git", Name: "branch-delete-force", Pattern: `(?i)\bgit\s+branch\s+-D\b`, Guard: ""},

	// Database.
	{Category: "database", Name: "drop", Pattern: `(?i)\bDROP\s+(?:TABLE|DATABASE|SCHEMA|INDEX)\b`, Guard: ""},
	{Category: "database", Name: "truncate", Pattern: `(?i)\bTRUNCATE\b`, Guard: ""},
	// Unguarded DELETE: matches DELETE FROM only when the same string has no
	// WHERE clause. Go's RE2 has no negative lookahead, so the absence check
	// is expressed via Guard. Note: the guard is whole-string, so a
	// concatenation like "DELETE FROM a; DELETE FROM b WHERE id=1" suppresses
	// the unguarded first statement. Acceptable for v1.
	{Category: "database", Name: "delete-without-where", Pattern: `(?i)\bDELETE\s+FROM\b`, Guard: `(?i)\bWHERE\b`},
	{Category: "database", Name: "dropdb", Pattern: `(?i)\bdropdb\b`, Guard: ""},

	// Cloud.
	{Category: "cloud", Name: "aws-ec2-terminate", Pattern: `(?i)\baws\s+ec2\s+terminate-instances\b`, Guard: ""},
	{Category: "cloud", Name: "aws-s3-rb", Pattern: `(?i)\baws\s+s3\s+rb\b`, Guard: ""},
	{Category: "cloud", Name: "gcloud-projects-delete", Pattern: `(?i)\bgcloud\s+projects\s+delete\b`, Guard: ""},
	{Category: "cloud", Name: "kubectl-delete-namespace", Pattern: `(?i)\bkubectl\s+delete\s+(?:ns|namespace)\b`, Guard: ""},
	{Category: "cloud", Name: "kubectl-delete-workload", Pattern: `(?i)\bkubectl\s+delete\s+(?:deployment|sts|statefulset|daemonset|pv|pvc)\b`, Guard: ""},
})

func compileCLIDestructivePatterns(specs []cliDestructiveSpec) []cliDestructivePattern {
	out := make([]cliDestructivePattern, 0, len(specs))
	for _, spec := range specs {
		p := cliDestructivePattern{
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

// flattenCLIStrings walks a parsed-JSON value and returns every string value
// reachable via map values or slice elements. Map keys are not included — the
// threat model is the values (a key like "DROP TABLE" would be strange but
// not actually destructive). Numbers, bools, and nil are ignored.
//
// Map iteration is keyed-sorted so the first-match-wins ordering downstream
// produces the same rule_id every run. Otherwise an input like
// {"shell": "rm -rf *", "query": "DROP TABLE"} would flap between
// shell/rm-rf and database/drop on alert dashboards.
func flattenCLIStrings(input any) []string {
	if input == nil {
		return nil
	}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			if t != "" {
				out = append(out, t)
			}
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(t[k])
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

// scanForCLIDestructive returns the first matching destructive pattern found
// in any string value reachable from input, or zero value + false when nothing
// matches. The first-match-wins ordering keeps the hot path predictable.
func scanForCLIDestructive(input any) (cliDestructivePattern, bool) {
	for _, str := range flattenCLIStrings(input) {
		if matched, ok := matchCLIDestructiveString(str); ok {
			return matched, true
		}
	}
	return cliDestructivePattern{Category: "", Name: "", Regex: nil, Guard: nil}, false
}

// matchCLIDestructiveString applies the curated pattern set to a single
// string. Exposed for tests; not exported.
func matchCLIDestructiveString(s string) (cliDestructivePattern, bool) {
	if s == "" {
		return cliDestructivePattern{Category: "", Name: "", Regex: nil, Guard: nil}, false
	}
	for _, p := range cliDestructivePatterns {
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
	return cliDestructivePattern{Category: "", Name: "", Regex: nil, Guard: nil}, false
}
