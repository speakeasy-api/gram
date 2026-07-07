package risk_analysis

import (
	"strings"
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
//	identity.<check>                 — session authenticated with a non-corporate AI account
//	custom.<rule_slug>               — project-defined custom detection rule
//
// The pair (source, rule_id) is the stable composite identity for downstream
// consumers, but the prefix alone is enough to bucket findings into
// dashboard categories.
//
// Per-source `Describe*` builders that produce (rule_id, description) for a
// Finding live next to the scanner that owns them: presidio.go, pi_scanner.go,
// and the extracted scanner packages under internal/scanners (gitleaks,
// clidestructive, destructivetool, shadowmcpscan). This file is just the shared
// grammar + constants.

const (
	prefixPII      = "pii."
	prefixIdentity = "identity."

	// RulePromptInjection is the canonical rule id emitted for every
	// prompt-injection finding. There is exactly one rule: whether the
	// match came from the L1 LLM judge or an L0 heuristic regex is an
	// implementation detail not part of the public contract.
	RulePromptInjection = "prompt_injection"

	// DeadLetterRuleID is the rule id emitted for Presidio dead-letter
	// sentinel rows when a message could not be analyzed.
	DeadLetterRuleID = prefixPII + "dead_letter"

	// RuleIdentityPersonalAccount fires when a session's AI account is
	// classified as a personal (non-team) account.
	RuleIdentityPersonalAccount = prefixIdentity + "personal_account"

	// RuleIdentityUnapprovedDomain fires when a session's AI-account email
	// domain is not on the policy's approved corporate domain list.
	RuleIdentityUnapprovedDomain = prefixIdentity + "unapproved_domain"
)

// Canonical rule_id helpers per source. These transform a raw upstream
// identifier (Presidio's UPPER_SNAKE entity type, a gitleaks rule name,
// ...) into the canonical snake_case-with-dots form. Their counterparts in
// the per-source module files (gitleaks.go, presidio.go, ...) compose the
// description sentence around the result.

// CanonicalPresidioRuleID converts a Presidio entity type (UPPER_SNAKE) to
// the canonical `pii.<snake_case>` rule id (`MEDICAL_LICENSE` -> `pii.medical_license`).
func CanonicalPresidioRuleID(raw string) string {
	return prefixPII + strings.ToLower(raw)
}
