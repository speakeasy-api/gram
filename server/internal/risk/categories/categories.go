// Package categories is the single source of truth for the
// (source, rule_id) → risk category mapping shown across the dashboard.
//
// Previously the mapping lived in four duplicated SQL CASE expressions
// (queries.sql) and a separate TypeScript classifier (risk-utils.ts),
// which silently drifted whenever a new rule was added. Now:
//
//   - Definitions below is the canonical list.
//   - Classify(source, ruleID) is the canonical lookup.
//   - SQLFilter(category) produces the parameter set the queries use to
//     filter without an in-query CASE.
//   - JSONResult() is what /rpc/risk.categories returns so the dashboard
//     can consume the same data instead of maintaining its own copy.
//
// When adding a new rule or category, edit Definitions and the matching
// frontend RULE_CATEGORY_META label, and everything else follows.
package categories

import "slices"

import "strings"

// Category is the user-facing bucket a finding falls into.
type Category string

const (
	CategorySecrets         Category = "secrets"
	CategoryFinancial       Category = "financial"
	CategoryPII             Category = "pii"
	CategoryGovernmentIDs   Category = "government_ids"
	CategoryHealthcare      Category = "healthcare"
	CategoryOffPolicy       Category = "off_policy"
	CategoryPromptPolicy    Category = "prompt_policy"
	CategoryPromptInjection Category = "prompt_injection"
	CategoryShadowMCP       Category = "shadow_mcp"
	CategoryDestructiveTool Category = "destructive_tool"
	CategoryCLIDestructive  Category = "cli_destructive"
	CategoryCustom          Category = "custom"
)

// Definition is one category's metadata + how to classify findings into it.
//
// A finding is classified to this category when ANY of the matching forms
// apply (checked in declaration order in Definitions, first match wins):
//   - Source matches the finding's source, OR
//   - RuleIDs contains the finding's rule_id, OR
//   - RulePrefix is a non-empty prefix of the finding's rule_id.
//
// ExcludeRuleIDs is only meaningful with RulePrefix: rule_ids in this list
// fall through to a later definition (e.g. "pii" uses prefix "pii." but the
// more-specific PII subcategories declared before it list their own rule_ids,
// so a credit-card rule_id matches "financial" first and never reaches "pii").
type Definition struct {
	Category    Category
	Label       string
	Description string
	Icon        string

	// Classification (any non-zero field participates).
	Source         string
	RuleIDs        []string
	RulePrefix     string
	ExcludeRuleIDs []string
}

// Definitions is the canonical, ordered classifier.
//
// Order matters: source-based categories that "own" their source come first
// (shadow_mcp etc.); then explicit rule_id lists; then prefix matches; finally
// scanner-source fallbacks (gitleaks → secrets, presidio → pii) for unprefixed
// rule_ids like `generic-api-key`. Scanner-source names MUST NOT leak to the
// UI — only the resolved Category does.
var Definitions = []Definition{
	{
		Category:    CategoryShadowMCP,
		Label:       "Shadow MCP",
		Description: "Tool calls in Cursor and Claude Code that don't come from a Speakeasy-issued MCP server. Requires Speakeasy hooks to be installed on the agent.",
		Icon:        "shield-off",
		Source:      "shadow_mcp",
	},
	{
		Category:    CategoryDestructiveTool,
		Label:       "Destructive Tools",
		Description: "MCP tool calls whose Gram tool definition is annotated as destructive. Requires Speakeasy hooks and Gram-issued MCP tool metadata.",
		Icon:        "shield-alert",
		Source:      "destructive_tool",
	},
	{
		Category:    CategoryCLIDestructive,
		Label:       "Destructive CLI Commands",
		Description: "Tool calls whose arguments match a curated set of destructive shell, git, database, or cloud CLI patterns (rm -rf, git push --force, DROP TABLE, kubectl delete ns, ...). Applies to native Bash / run_terminal_cmd as well as MCP-routed tools whose arguments carry destructive content.",
		Icon:        "terminal",
		Source:      "cli_destructive",
	},
	{
		Category:    CategoryPromptPolicy,
		Label:       "Prompt Policies",
		Description: "Natural-language guardrails evaluated by the policy judge",
		Icon:        "message-square-warning",
		Source:      "llm_judge",
	},
	{
		Category:    CategoryPromptInjection,
		Label:       "Prompt Injection",
		Description: "Indirect injection via tool outputs, hidden instructions",
		Icon:        "syringe",
		Source:      "prompt_injection",
	},
	{
		Category:    CategorySecrets,
		Label:       "Secrets",
		Description: "API keys, tokens, private keys, credentials",
		Icon:        "key-round",
		RulePrefix:  "secret.",
	},
	{
		Category:    CategoryFinancial,
		Label:       "Financial Information",
		Description: "Credit cards, bank accounts, routing numbers, IBAN codes",
		Icon:        "credit-card",
		RuleIDs:     []string{"pii.credit_card", "pii.iban_code", "pii.us_bank_number", "pii.crypto"},
	},
	{
		Category:    CategoryGovernmentIDs,
		Label:       "Government Identifiers",
		Description: "SSNs, passport numbers, national IDs, tax IDs",
		Icon:        "landmark",
		RuleIDs: []string{
			"pii.us_ssn",
			"pii.us_passport",
			"pii.us_itin",
			"pii.uk_nhs",
			"pii.uk_nino",
			"pii.uk_passport",
			"pii.es_nif",
			"pii.it_fiscal_code",
			"pii.au_tfn",
			"pii.in_pan",
			"pii.in_aadhaar",
			"pii.sg_nric_fin",
		},
	},
	{
		Category:    CategoryHealthcare,
		Label:       "Healthcare Information",
		Description: "Medical record numbers, patient data, Medicare IDs",
		Icon:        "heart-pulse",
		RuleIDs: []string{
			"pii.medical_license",
			"pii.us_mbi",
			"pii.us_npi",
			"pii.medical_disease_disorder",
			"pii.medical_medication",
			"pii.medical_therapeutic_procedure",
			"pii.medical_clinical_event",
			"pii.medical_biological_attribute",
			"pii.medical_family_history",
		},
	},
	{
		Category:    CategoryOffPolicy,
		Label:       "Off-Policy Content",
		Description: "Requests that violate usage policies or acceptable use guidelines",
		Icon:        "ban",
		RuleIDs: []string{
			"pii.harmful_content_request",
			"pii.policy_violation",
			"pii.unauthorized_action",
			"pii.topic_boundary_violation",
		},
	},
	{
		Category:    CategoryPII,
		Label:       "Personal Identifiable Information",
		Description: "Phone numbers, email addresses, IP and MAC addresses",
		Icon:        "user",
		RulePrefix:  "pii.",
	},
	// Scanner-source fallbacks. These let unprefixed rule_ids that come from
	// our integrated scanners (e.g. gitleaks' bare "generic-api-key") still
	// resolve to a user-facing category instead of leaking the scanner name.
	// Keep these LAST so any rule_id with a real prefix takes precedence.
	{
		Category:    CategorySecrets,
		Label:       "Secrets",
		Description: "API keys, tokens, private keys, credentials",
		Icon:        "key-round",
		Source:      "gitleaks",
	},
	{
		Category:    CategoryPII,
		Label:       "Personal Identifiable Information",
		Description: "Phone numbers, email addresses, IP and MAC addresses",
		Icon:        "user",
		Source:      "presidio",
	},
}

// CustomDefinition is the metadata for the fallback category.
var CustomDefinition = Definition{
	Category:    CategoryCustom,
	Label:       "Custom Patterns",
	Description: "Organization-specific data patterns (regex)",
	Icon:        "regex",
}

// Classify returns the canonical category for a finding's (source, rule_id).
// Falls back to CategoryCustom for unmatched findings.
func Classify(source, ruleID string) Category {
	for _, def := range Definitions {
		if def.Source != "" && def.Source == source {
			return def.Category
		}
		if matchesRule(def, ruleID) {
			return def.Category
		}
	}
	return CategoryCustom
}

func matchesRule(def Definition, ruleID string) bool {
	if ruleID == "" {
		return false
	}
	if slices.Contains(def.RuleIDs, ruleID) {
		return true
	}
	if def.RulePrefix != "" && strings.HasPrefix(ruleID, def.RulePrefix) {
		return true
	}
	return false
}

// Filter is the parameter set a SQL query uses to express "rows belonging
// to this category" without an in-query CASE expression. Pass these
// directly to query params; a query is filtered by category iff at least
// one of Sources / RuleIDs / RulePrefix is non-empty.
type Filter struct {
	Sources    []string
	RuleIDs    []string
	RulePrefix string
}

// FilterFor returns the SQL filter for one category. Empty Filter means
// "match nothing" (unknown category); callers should distinguish that
// from "no filter applied" upstream.
func FilterFor(cat Category) Filter {
	if cat == "" {
		return Filter{}
	}
	if cat == CategoryCustom {
		// Custom is the fallback: anything not matched by the explicit
		// definitions. Caller composes it as NOT (any other category).
		// In practice the dashboard never filters by "custom" since it's
		// the "everything else" bucket; emit an empty filter and let it
		// be a no-op rather than implementing the negation here.
		return Filter{}
	}
	for _, def := range Definitions {
		if def.Category != cat {
			continue
		}
		out := Filter{RulePrefix: def.RulePrefix}
		if def.Source != "" {
			out.Sources = []string{def.Source}
		}
		if len(def.RuleIDs) > 0 {
			out.RuleIDs = append(out.RuleIDs, def.RuleIDs...)
		}
		return out
	}
	return Filter{}
}

// All returns every Definition, with CustomDefinition appended last.
// Used by the JSON endpoint and tests.
func All() []Definition {
	out := make([]Definition, 0, len(Definitions)+1)
	out = append(out, Definitions...)
	out = append(out, CustomDefinition)
	return out
}
