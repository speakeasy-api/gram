// Package recommendedscopes contains the centrally-maintained detection scope
// registry for risk categories.
//
// Custom rules carry an intentionally empty recommendation: they self-scope via
// their CEL detection_expr, and the registry entry exists so policies can
// specify a per-policy detection scope for them.
package recommendedscopes

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/risk/categories"
)

// Version is surfaced via API and metrics so behavior changes are attributable.
const Version = 3

// assistantExempt drops the agent's own free text from scanning: risky content
// reaching the assistant enters via an already-scanned surface (user input,
// tool output), so assistant text is dominated by echo.
const assistantExempt = `kind == "assistant_message"`

// readOnlyToolsRegex is the narrow allowlist of read-only tool names; a
// tool-call batch is exempt from prompt-injection scanning only when every
// call in it is read-only, so write and exec tool arguments stay in scope.
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
		ScopeExempt:  assistantExempt,
		Rationale:    "Secrets enter through user paste, tool output, and tool arguments; the agent's own free text only echoes content those surfaces already scanned.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryFinancial,
		ScopeInclude: "",
		ScopeExempt:  assistantExempt,
		Rationale:    "Financial data enters through user input, tool results, and tool arguments; the agent's own free text only echoes content those surfaces already scanned.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryPII,
		ScopeInclude: "",
		ScopeExempt:  assistantExempt,
		Rationale:    "PII enters through user input, tool results, and tool arguments; the agent's own free text only echoes content those surfaces already scanned.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryGovernmentIDs,
		ScopeInclude: "",
		ScopeExempt:  assistantExempt,
		Rationale:    "Government identifiers enter through user input, tool output, and tool arguments; the agent's own free text only echoes content those surfaces already scanned.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryHealthcare,
		ScopeInclude: "",
		ScopeExempt:  assistantExempt,
		Rationale:    "Healthcare data enters through prompts, tool results, and tool arguments; the agent's own free text only echoes content those surfaces already scanned.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryOffPolicy,
		ScopeInclude: "",
		ScopeExempt:  assistantExempt,
		// A kind == "tool_response" exemption is a further candidate pending corpus validation.
		Rationale:  "Off-policy content is scanned on user input and tool traffic; the agent's own free text is excluded as echo of already-scanned surfaces.",
		Applicable: true,
	},
	{
		Category:     categories.CategoryPromptPolicy,
		ScopeInclude: "",
		ScopeExempt:  assistantExempt,
		Rationale:    "Prompt-policy guardrails are judged on user input and tool traffic; specify a custom detection scope on the policy if a guardrail targets the agent's own responses.",
		Applicable:   true,
	},
	{
		Category:     categories.CategoryPromptInjection,
		ScopeInclude: "",
		ScopeExempt: fmt.Sprintf(
			`kind == "assistant_message" || (kind == "tool_request" && tool_calls.exists(t, t.name.matchRegex("%[1]s")) && !tool_calls.exists(t, !t.name.matchRegex("%[1]s")))`,
			readOnlyToolsRegex,
		),
		Rationale:  "Injection enters through user input, tool output, and write or exec tool calls; the agent's own free text and all-read-only tool-call batches are excluded.",
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
		Category:     categories.CategoryCustom,
		ScopeInclude: "",
		ScopeExempt:  "",
		Rationale:    "Custom rules self-scope via their detection expression, so no surfaces are excluded by default; specify a detection scope to additionally gate which surfaces they run on.",
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
