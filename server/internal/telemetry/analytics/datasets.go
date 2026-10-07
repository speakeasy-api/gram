package analytics

// Sessions is one row per agent session, collapsed from observed events.
var Sessions = &Dataset{
	Name:        "sessions",
	Kind:        KindEvent,
	Grain:       "session",
	Description: "One row per agent session, collapsed from observed events. count counts sessions.",
	TimeExpr:    "started_at",
	Fields: []Field{
		{Name: "session", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "session_id", Description: ""},
		// A session opens broken down by who was in it: text lives on events,
		// not on the collapsed session row.
		{Name: "user", Type: TypeString, Role: RoleDimension, Default: true, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "user_email", Description: ""},
		{Name: "model", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "model", Description: ""},
		{Name: "surface", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "surface", Description: ""},
		{Name: "provider", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "provider", Description: ""},
		{Name: "turn_count", Type: TypeInt64, Role: RoleMeasure, Default: false, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg}, Expr: "turn_count", Description: ""},
		{Name: "tool_call_count", Type: TypeInt64, Role: RoleMeasure, Default: false, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg}, Expr: "tool_call_count", Description: ""},
		// The time columns are Int64 nanoseconds, hence the division.
		{Name: "duration_seconds", Type: TypeFloat64, Role: RoleMeasure, Default: false, Unit: "s", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg, AggregationP95}, Expr: "(ended_at - started_at) / 1e9", Description: ""},
	},
	Source: sessionsSource,
}

// ToolCalls is one row per tool call, resolved to its terminal observation.
var ToolCalls = &Dataset{
	Name:        "tool_calls",
	Kind:        KindEvent,
	Grain:       "tool call",
	Description: "One row per tool call, resolved to its latest observation. count counts tool calls; failed calls are count with a status filter. Tools used is count_distinct over tool_name, people over user.",
	TimeExpr:    "started_at",
	Fields: []Field{
		{Name: "tool_call", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "tool_call_id", Description: ""},
		{Name: "tool_name", Type: TypeString, Role: RoleDimension, Default: true, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "tool_name", Description: ""},
		// Every MCP call carries the tool name mcp_tool; the server and tool the
		// producer named are what tell them apart.
		{Name: "mcp_server", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "mcp_server_name", Description: ""},
		{Name: "mcp_tool", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "mcp_tool_name", Description: ""},
		{Name: "skill", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "skill_name", Description: skillDescription},
		{Name: "session", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "session_id", Description: ""},
		{Name: "user", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "user_email", Description: ""},
		{Name: "surface", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "surface", Description: ""},
		// status reads outcome in agent vocabulary, not a protocol status
		// code: ok, error, rejected (a call a decision blocked) or refused (a
		// model declining). describe does not enumerate values, so this is the
		// spec of what a status filter may name.
		{Name: "status", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "status", Description: ""},
		{Name: "duration_ms", Type: TypeFloat64, Role: RoleMeasure, Default: false, Unit: "ms", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg, AggregationP95}, Expr: "duration_nano / 1e6", Description: ""},
	},
	Source: toolCallsSource,
}

// skillDescription is on the skill dimension because only one producer
// family states a skill: a sparse breakdown means skills were not reported,
// not that none were used, and the picker has to say so.
const skillDescription = "Skill a Skill tool invocation named. Reported by Claude Code when tool details are logged and by Speakeasy hooks; Codex and generic OpenTelemetry producers report no skills, so a sparse breakdown means they were not reported, not that none were used."

// Default is the v1 catalog: both datasets read agent_events.
var Default = MustCatalog(Sessions, ToolCalls)
