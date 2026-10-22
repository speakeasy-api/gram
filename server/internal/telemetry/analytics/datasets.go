package analytics

import (
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/risk"
)

// The field literals below state only what a field has; an omitted field is
// its zero value by design, and the file is excluded from exhaustruct for it.

// ToolCalls is one row per tool call, resolved to its terminal observation.
var ToolCalls = &Dataset{
	Name:        "tool_calls",
	Kind:        KindEvent,
	Grain:       "tool call",
	Description: "One row per tool call, resolved to its latest observation. count counts tool calls; failed calls are count with a status filter. Tools used is count_distinct over tool_name, people over user.",
	TimeExpr:    "started_at",
	Access:      AccessProject,
	Fields: []Field{
		{
			Name:      "tool_call",
			Type:      TypeString,
			Role:      RoleDimension,
			Operators: equalsIn,
			Expr:      "tool_call_id",
			Typeahead: "event_id",
		},
		{
			Name:         "tool_name",
			Type:         TypeString,
			Role:         RoleDimension,
			Default:      true,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "tool_name",
			Typeahead:    "tool_name",
		},
		// Every MCP call carries the tool name mcp_tool; the server and tool the
		// producer named are what tell them apart.
		{
			Name:         "mcp_server",
			Type:         TypeString,
			Role:         RoleDimension,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "mcp_server_name",
			Typeahead:    "mcp_server_name",
			Description:  mcpServerDescription,
			Lookup:       MCPServerDisplayNamesLookup,
		},
		{
			Name:      "mcp_tool",
			Type:      TypeString,
			Role:      RoleDimension,
			Operators: equalsIn,
			Expr:      "mcp_tool_name",
			Typeahead: "mcp_tool_name",
		},
		{
			Name:         "session",
			Type:         TypeString,
			Role:         RoleDimension,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "session_id",
			Typeahead:    "session_id",
		},
		{
			Name:         "user",
			Type:         TypeString,
			Role:         RoleDimension,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "user_email",
			Typeahead:    "user_email",
		},
		{
			Name:      "surface",
			Type:      TypeString,
			Role:      RoleDimension,
			Operators: equalsIn,
			Expr:      "surface",
			Typeahead: "surface",
		},
		// status reads outcome in agent vocabulary, not a protocol status
		// code: ok, error, rejected (a call a decision blocked) or refused (a
		// model declining). describe does not enumerate values, so this is the
		// spec of what a status filter may name.
		{
			Name:      "status",
			Type:      TypeString,
			Role:      RoleDimension,
			Operators: equalsIn,
			Expr:      "status",
			Typeahead: "outcome",
		},
		{
			Name:         "duration_ms",
			Type:         TypeFloat64,
			Role:         RoleMeasure,
			Unit:         "ms",
			Aggregations: []Aggregation{AggregationSum, AggregationAvg, AggregationP95},
			Expr:         "duration_nano / 1e6",
		},
	},
	Source: toolCallsSource,
}

// Skills is one row per skill invocation: a tool call that named a skill,
// resolved to its terminal observation like any other call. Only one
// producer family states a skill, so the dataset says so: an empty
// breakdown means skills were not reported, not that none were used.
var Skills = &Dataset{
	Name:        "skills",
	Kind:        KindEvent,
	Grain:       "skill invocation",
	Description: "One row per skill invocation: a tool call that named a skill, resolved to its latest observation. count counts invocations; skills used is count_distinct over skill, people over user. Claude Code reports the skill when tool details are logged, as do Speakeasy hooks; Codex and generic OpenTelemetry producers report none, so an empty breakdown means skills were not reported, not that none were used.",
	TimeExpr:    "started_at",
	Access:      AccessProject,
	Fields: []Field{
		// An invocation is one tool call, so its id tells two invocations of
		// the same skill apart in rows mode.
		{
			Name:      "tool_call",
			Type:      TypeString,
			Role:      RoleDimension,
			Operators: equalsIn,
			Expr:      "tool_call_id",
			Typeahead: "event_id",
		},
		{
			Name:         "skill",
			Type:         TypeString,
			Role:         RoleDimension,
			Default:      true,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "skill_name",
			Typeahead:    "skill_name",
		},
		{
			Name:         "session",
			Type:         TypeString,
			Role:         RoleDimension,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "session_id",
			Typeahead:    "session_id",
		},
		{
			Name:         "user",
			Type:         TypeString,
			Role:         RoleDimension,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "user_email",
			Typeahead:    "user_email",
		},
		{
			Name:      "surface",
			Type:      TypeString,
			Role:      RoleDimension,
			Operators: equalsIn,
			Expr:      "surface",
			Typeahead: "surface",
		},
		// status is the invocation's outcome in the same vocabulary as a tool
		// call's: ok, error, rejected or refused.
		{
			Name:      "status",
			Type:      TypeString,
			Role:      RoleDimension,
			Operators: equalsIn,
			Expr:      "status",
			Typeahead: "outcome",
		},
		{
			Name:         "duration_ms",
			Type:         TypeFloat64,
			Role:         RoleMeasure,
			Unit:         "ms",
			Aggregations: []Aggregation{AggregationSum, AggregationAvg, AggregationP95},
			Expr:         "duration_nano / 1e6",
		},
	},
	Source: skillsSource,
}

// RiskFindings is one row per live risk finding, for counting what the risk
// policies catch: by category, rule, severity, policy, agent, team and person.
// It answers aggregates only and is for organization administrators, who are
// the only ones the rest of the product shows findings to; the matched value
// and the chat it came from are not in it at all.
var RiskFindings = &Dataset{
	Name:        "risk_findings",
	Kind:        KindMetric,
	Grain:       "finding",
	Description: "One row per live risk finding: something a risk policy caught in an agent session, such as a secret, personal data, prompt injection or an unreviewed MCP server. Dismissed, excluded and false-positive findings are left out. count counts findings; people affected is count_distinct over user. It answers aggregates only, never individual findings, and only organization administrators can read it.",
	TimeExpr:    "detected_at",
	Access:      AccessOrgAdmin,
	Fields: []Field{
		{
			Name:         "category",
			Type:         TypeString,
			Role:         RoleDimension,
			Default:      true,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "category",
			Description:  "What kind of risk the finding is: secrets, pii, financial, government_ids, healthcare, prompt_injection, shadow_mcp, destructive_tool and so on. Empty when its rule maps to no category.",
		},
		{
			Name:         "rule",
			Type:         TypeString,
			Role:         RoleDimension,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "rule_id",
			Description:  "The detection rule that fired, such as pii.email_address or secret.github_pat.",
		},
		{
			Name:        "detector",
			Type:        TypeString,
			Role:        RoleDimension,
			Operators:   equalsIn,
			Expr:        "source",
			Description: "Which detector raised the finding, such as gitleaks, presidio, shadow_mcp, prompt_injection or llm_judge.",
		},
		{
			Name:        "severity",
			Type:        TypeString,
			Role:        RoleDimension,
			Operators:   equalsIn,
			Expr:        riskSeverityKeyExpr,
			Description: "critical, high, medium or low: the band of the score set on the policy that caught the finding, as on the Watchdog page. A finding whose policy has no score, or was deleted, takes its category's usual band.",
			Lookup:      RiskPolicySeveritiesLookup,
		},
		{
			Name:         "policy",
			Type:         TypeString,
			Role:         RoleDimension,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "risk_policy_id",
			Description:  "The risk policy that caught the finding, by name. A deleted policy's findings still count, under its name marked deleted.",
			Lookup:       RiskPolicyNamesLookup,
		},
		{
			Name:         "surface",
			Type:         TypeString,
			Role:         RoleDimension,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "chat_source",
			Description:  "The agent or app the finding came from, such as claude-code, codex or cursor. Empty when it was not recorded.",
		},
		{
			Name:         "team",
			Type:         TypeString,
			Role:         RoleDimension,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "team",
			Description:  "The department of the person, from the organization's directory. Empty when they have no directory profile.",
		},
		{
			Name:         "user",
			Type:         TypeString,
			Role:         RoleDimension,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "user_label",
			Description:  "The person whose session raised the finding, by email, or by the id their agent reported when they have no account email.",
		},
		{
			Name:        "enforcement",
			Type:        TypeString,
			Role:        RoleDimension,
			Operators:   equalsIn,
			Expr:        "enforcement_outcome",
			Description: "What the gateway did about a finding on an MCP call: logged, denied, withheld, quarantined or a warning outcome. Empty for findings in chat, where nothing is enforced.",
		},
	},
	Source: riskFindingsSource,
}

// riskSeverityKeyExpr is what a finding's severity is looked up by: its
// policy and the band its category reads as without one, as
// "<policy id>:<band>". The severities lookup maps every such key a
// project's findings can have, so each reads as a band, never as the key.
var riskSeverityKeyExpr = "concat(risk_policy_id, ':', " + categorySeverityExpr("category") + ")"

// categorySeverityExpr is the severity band each category reads as when its
// finding has no scored policy, as SQL over the category column. A category
// it does not know reads as the default weight's band.
func categorySeverityExpr(column string) string {
	bands, unknown := risk.CategoryFallbackSeverities()
	categories := make([]string, 0, len(bands))
	for category := range bands {
		categories = append(categories, category)
	}
	slices.Sort(categories)
	targets := make([]string, len(categories))
	for i, category := range categories {
		targets[i] = bands[category]
	}
	return "transform(" + column + ", " + sqlStrings(categories) + ", " + sqlStrings(targets) + ", " + sqlString(unknown) + ")"
}

// sqlString quotes a value known at compile time as a ClickHouse string
// literal. Values a caller supplies are bound, never quoted.
func sqlString(value string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'"
}

func sqlStrings(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = sqlString(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// Filters fold their values the way the column folds, so a raw name or its
// display name both match; the field says which name it shows.
const mcpServerDescription = "MCP server the call went to, under the display name set in Hooks settings. A raw name with no override shows as reported; a filter may name either."

// MCPServerDisplayNamesLookup names the lookup mcp_server reads through: the
// project's hook server-name overrides, raw name to display name.
const MCPServerDisplayNamesLookup = "mcp_server_display_names"

// RiskPolicyNamesLookup names the lookup a finding's policy reads through:
// the project's risk policies, id to name.
const RiskPolicyNamesLookup = "risk_policy_names"

// RiskPolicySeveritiesLookup names the lookup a finding's severity reads
// through: every riskSeverityKeyExpr key the project's findings can have, to
// the band of its policy's score, or to its category's band when the policy
// has no score, was deleted or is absent.
const RiskPolicySeveritiesLookup = "risk_policy_severities"

// Lookups is every map a dimension of the catalog reads through. The service
// attaches the loaders.
var Lookups = []*Lookup{
	{
		Name:        MCPServerDisplayNamesLookup,
		Description: "The project's hook server-name overrides, set in Hooks settings: a reported MCP server name with an override shows as its display name, the rest as reported.",
	},
	{
		Name:        RiskPolicyNamesLookup,
		Description: "The project's risk policies by name, deleted ones marked as such.",
	},
	{
		Name:        RiskPolicySeveritiesLookup,
		Description: "The severity band of each risk policy's score: critical from 9, high from 7, medium from 4, low below. A finding whose policy has no score, or was deleted, reads as its category's band.",
	},
}

// Default is the catalog: the agent session datasets and the risk findings.
var Default = MustCatalog(Lookups, ToolCalls, Skills, RiskFindings)
