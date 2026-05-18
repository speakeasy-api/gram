package risk_analysis

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// Rule id conventions
//
// All rule ids are lowercase. They are grouped by **risk category** via a
// short dotted prefix so consumers can pattern-match on category without a
// switch on `source`:
//
//	secret.<gitleaks_rule>           — credentials / API keys / tokens
//	pii.<presidio_entity>            — personal / financial / medical data
//	shadow_mcp                       — unverified MCP tool call (single rule)
//	destructive.tool                 — MCP tool annotated as destructive
//	destructive.<cat>.<name>         — destructive shell / git / db / cloud command
//	prompt_injection                 — prompt injection (engine selected per-org)
//
// The pair (source, rule_id) is the stable composite identity for downstream
// consumers, but the prefix alone is enough to bucket findings into
// dashboard categories.
//
// Per-source `Describe*` builders that produce (rule_id, description) for a
// Finding live next to the scanner that owns them: gitleaks.go, presidio.go,
// pi_scanner.go, cli_destructive.go, and analyze_batch.go (shadow_mcp +
// destructive_tool, whose writers live there too). This file is just the
// shared grammar + constants.

const (
	prefixSecret      = "secret."
	prefixPII         = "pii."
	prefixDestructive = "destructive."

	// RuleShadowMCP is the canonical rule id emitted for every shadow_mcp
	// finding. The detection mechanism (missing toolset id, unknown
	// toolset, ...) is implementation detail kept in logs; the rule_id
	// describes the risk itself.
	RuleShadowMCP = "shadow_mcp"

	// RuleDestructiveTool is the canonical rule id emitted for every
	// destructive_tool finding.
	RuleDestructiveTool = prefixDestructive + "tool"

	// RulePromptInjection is the canonical rule id emitted for every
	// prompt-injection finding. There is exactly one rule: whether the
	// match came from the L1 deberta classifier or an L0 heuristic regex
	// is an implementation detail not part of the public contract.
	RulePromptInjection = "prompt_injection"

	// DeadLetterRuleID is the rule id emitted for Presidio dead-letter
	// sentinel rows when a message could not be analyzed.
	DeadLetterRuleID = prefixPII + "dead_letter"
)

// guard validates the rule id against the canonical grammar and panics in
// dev/test if it doesn't match. Returns the id unchanged so callers can
// compose it inline.
func guard(ruleID string) string {
	if !enforceRuleIDFormat {
		return ruleID
	}
	if err := ValidateRuleID(ruleID); err != nil {
		panic(fmt.Sprintf("risk_analysis: invalid canonical rule id: %v", err))
	}
	return ruleID
}

// ruleIDFormat is the canonical rule id grammar: lowercase ASCII letters
// and digits, underscores within a segment, dots between segments.
var ruleIDFormat = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*(\.[a-z0-9]+(_[a-z0-9]+)*)*$`)

// ValidateRuleID returns an error when id does not conform to the canonical
// rule id grammar.
func ValidateRuleID(id string) error {
	if id == "" {
		return fmt.Errorf("rule id is empty")
	}
	if !ruleIDFormat.MatchString(id) {
		return fmt.Errorf("rule id %q is not in canonical form (lowercase snake_case segments joined by dots)", id)
	}
	return nil
}

// enforceRuleIDFormat is true when the binary is running under `go test`
// (covers unit + integration suites) or after cmd/ wiring opts in via
// EnableRuleIDFormatEnforcement (used in local-dev). In those modes the
// Describe* builders panic on a malformed canonical rule id so writer
// drift is caught immediately. Production runs leave the value through.
var enforceRuleIDFormat = testing.Testing()

// EnableRuleIDFormatEnforcement opts the process in to strict canonical
// rule_id validation: any Describe* builder returning a malformed id will
// panic. Intended for local development; cmd/ wires this on when
// `--environment=local`. Test binaries get the same behavior automatically
// via testing.Testing().
func EnableRuleIDFormatEnforcement() {
	enforceRuleIDFormat = true
}

// Canonical rule_id helpers per source. These transform a raw upstream
// identifier (Presidio's UPPER_SNAKE entity type, a gitleaks rule name,
// ...) into the canonical snake_case-with-dots form. Their counterparts in
// the per-source module files (gitleaks.go, presidio.go, ...) compose the
// description sentence around the result.

// CanonicalGitleaksRuleID prepends the `secret.` prefix to a gitleaks rule
// id and converts its kebab-case to snake_case so the result conforms to
// the canonical grammar.
func CanonicalGitleaksRuleID(raw string) string {
	return prefixSecret + strings.ReplaceAll(strings.ToLower(raw), "-", "_")
}

// CanonicalPresidioRuleID converts a Presidio entity type (UPPER_SNAKE) to
// the canonical `pii.<snake_case>` rule id (`MEDICAL_LICENSE` -> `pii.medical_license`).
func CanonicalPresidioRuleID(raw string) string {
	return prefixPII + strings.ToLower(raw)
}
