// Package recommendedscopes contains the centrally-maintained detection scope
// registry for built-in risk categories.
//
// Custom rules are intentionally absent from this registry: they self-scope via
// their CEL detection_expr.
package recommendedscopes

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/risk/categories"
)

// Version is surfaced via API and metrics so behavior changes are attributable.
const Version = 1

const readOnlyToolsRegex = "^(Read|Grep|Glob|LS|NotebookRead|ExitPlanMode|TodoWrite|AskUserQuestion|ToolSearch|WebSearch)$"

// Recommendation is the centrally-maintained detection scope for one category.
// It composes with (intersects) the policy's own scope at scan time; it is
// never snapshotted into policy rows, so updating this registry retunes all
// policies that haven't opted out.
type Recommendation struct {
	Category     categories.Category
	ScopeInclude string // CEL over celenv; "" = no include restriction
	ScopeExempt  string // CEL over celenv; "" = no exemption
	Rationale    string // user-facing copy: why this scope exists / what disabling costs
	Applicable   bool   // false = session-scoped category; message scoping does not apply
}

var registry = []Recommendation{
	{
		Category:     categories.CategorySecrets,
		ScopeInclude: "",
		ScopeExempt:  "",
		Rationale:    "Secrets can leak through user paste, tool output, assistant echo, and tool arguments alike, so all message surfaces remain intentionally in scope.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryFinancial,
		ScopeInclude: "",
		ScopeExempt:  "",
		Rationale:    "Financial data can appear in user input, tool results, assistant responses, and tool arguments, so all message surfaces remain intentionally in scope.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryPII,
		ScopeInclude: "",
		ScopeExempt:  "",
		Rationale:    "PII can be introduced, returned, repeated, or passed onward by any message surface, so all message surfaces remain intentionally in scope.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryGovernmentIDs,
		ScopeInclude: "",
		ScopeExempt:  "",
		Rationale:    "Government identifiers can appear in user input, tool output, assistant echo, and tool arguments, so all message surfaces remain intentionally in scope.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryHealthcare,
		ScopeInclude: "",
		ScopeExempt:  "",
		Rationale:    "Healthcare data can be exposed across prompts, tool results, assistant responses, and tool arguments, so all message surfaces remain intentionally in scope.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryOffPolicy,
		ScopeInclude: "",
		ScopeExempt:  "",
		// A kind == "tool_response" exemption is a candidate pending corpus validation.
		Rationale:  "Off-policy content can be present across message surfaces; tool-response exemption remains a candidate until corpus validation proves it does not hide violations.",
		Applicable: true,
	},
	{
		Category:     categories.CategoryPromptPolicy,
		ScopeInclude: "",
		ScopeExempt:  "",
		Rationale:    "Prompt-policy guardrails are user-authored and intent is unknowable, so all message surfaces remain intentionally in scope.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryPromptInjection,
		ScopeInclude: "",
		ScopeExempt: fmt.Sprintf(
			`kind == "assistant_message" || (kind == "tool_request" && tool_calls.exists(t, t.name.matchRegex("%[1]s")) && !tool_calls.exists(t, !t.name.matchRegex("%[1]s")))`,
			readOnlyToolsRegex,
		),
		Rationale:  "Injection rides in user input, tool output, and write/exec tool args, never the agent's own free text or all-read-only tool-call batches; validated to cut judge FPs ~37%->~5% with zero attack-corpus regressions.",
		Applicable: true,
	},
	{
		Category:     categories.CategoryShadowMCP,
		ScopeInclude: `kind == "tool_request"`,
		ScopeExempt:  "",
		Rationale:    "Shadow MCP detection only ever fires on tool calls; this formalizes existing scanner behavior and lets the pipeline skip other messages.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryDestructiveTool,
		ScopeInclude: `kind == "tool_request"`,
		ScopeExempt:  "",
		Rationale:    "Destructive-tool detection only ever fires on tool calls; this formalizes existing scanner behavior and lets the pipeline skip other messages.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryCLIDestructive,
		ScopeInclude: `kind == "tool_request"`,
		ScopeExempt:  "",
		Rationale:    "Destructive CLI detection only ever fires on tool calls; this formalizes existing scanner behavior and lets the pipeline skip other messages.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryAccountIdentity,
		ScopeInclude: "",
		ScopeExempt:  "",
		Rationale:    "Account identity is a session-scoped detector inspecting account attribution, not message content; message scoping does not apply.",
		Applicable:   false,
	},
}

var byCategory = func() map[categories.Category]Recommendation {
	out := make(map[categories.Category]Recommendation, len(registry))
	for _, rec := range registry {
		out[rec.Category] = rec
	}
	return out
}()

// For returns the recommended detection scope for cat.
func For(cat categories.Category) (Recommendation, bool) {
	rec, ok := byCategory[cat]
	return rec, ok
}

// All returns all recommendations in stable registry order.
func All() []Recommendation {
	out := make([]Recommendation, len(registry))
	copy(out, registry)
	return out
}
