package analytics

// Sessions is one row per agent session, collapsed from observed events.
var Sessions = &Dataset{
	Name:        "sessions",
	Kind:        KindEvent,
	Grain:       "session",
	Description: "One row per agent session, collapsed from observed events. count counts sessions.",
	TimeExpr:    "started_at",
	Fields: []Field{
		{Name: "session", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "session_id", Description: "", Lookup: ""},
		// A session opens broken down by who was in it: text lives on events,
		// not on the collapsed session row.
		{Name: "user", Type: TypeString, Role: RoleDimension, Default: true, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "user_email", Description: "", Lookup: ""},
		{Name: "model", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "model", Description: "", Lookup: ""},
		{Name: "surface", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "surface", Description: "", Lookup: ""},
		{Name: "provider", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "provider", Description: "", Lookup: ""},
		{Name: "turn_count", Type: TypeInt64, Role: RoleMeasure, Default: false, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg}, Expr: "turn_count", Description: "", Lookup: ""},
		{Name: "tool_call_count", Type: TypeInt64, Role: RoleMeasure, Default: false, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg}, Expr: "tool_call_count", Description: "", Lookup: ""},
		// The time columns are Int64 nanoseconds, hence the division.
		{Name: "duration_seconds", Type: TypeFloat64, Role: RoleMeasure, Default: false, Unit: "s", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg, AggregationP95}, Expr: "(ended_at - started_at) / 1e9", Description: "", Lookup: ""},
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
		{Name: "tool_call", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "tool_call_id", Description: "", Lookup: ""},
		{Name: "tool_name", Type: TypeString, Role: RoleDimension, Default: true, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "tool_name", Description: "", Lookup: ""},
		// Every MCP call carries the tool name mcp_tool; the server and tool the
		// producer named are what tell them apart.
		{Name: "mcp_server", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "mcp_server_name", Description: mcpServerDescription, Lookup: MCPServerDisplayNamesLookup},
		{Name: "mcp_tool", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "mcp_tool_name", Description: "", Lookup: ""},
		{Name: "session", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "session_id", Description: "", Lookup: ""},
		{Name: "user", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "user_email", Description: "", Lookup: ""},
		{Name: "surface", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "surface", Description: "", Lookup: ""},
		// status reads outcome in agent vocabulary, not a protocol status
		// code: ok, error, rejected (a call a decision blocked) or refused (a
		// model declining). describe does not enumerate values, so this is the
		// spec of what a status filter may name.
		{Name: "status", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "status", Description: "", Lookup: ""},
		{Name: "duration_ms", Type: TypeFloat64, Role: RoleMeasure, Default: false, Unit: "ms", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg, AggregationP95}, Expr: "duration_nano / 1e6", Description: "", Lookup: ""},
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
	Fields: []Field{
		// An invocation is one tool call, so its id tells two invocations of
		// the same skill apart in rows mode.
		{Name: "tool_call", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "tool_call_id", Description: "", Lookup: ""},
		{Name: "skill", Type: TypeString, Role: RoleDimension, Default: true, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "skill_name", Description: "", Lookup: ""},
		{Name: "session", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "session_id", Description: "", Lookup: ""},
		{Name: "user", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: countDistinct, Expr: "user_email", Description: "", Lookup: ""},
		{Name: "surface", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "surface", Description: "", Lookup: ""},
		// status is the invocation's outcome in the same vocabulary as a tool
		// call's: ok, error, rejected or refused.
		{Name: "status", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "status", Description: "", Lookup: ""},
		{Name: "duration_ms", Type: TypeFloat64, Role: RoleMeasure, Default: false, Unit: "ms", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg, AggregationP95}, Expr: "duration_nano / 1e6", Description: "", Lookup: ""},
	},
	Source: skillsSource,
}

// Filters fold their values the way the column folds, so a raw name or its
// display name both match; the field says which name it shows.
const mcpServerDescription = "MCP server the call went to, under the display name set in Hooks settings. A raw name with no override shows as reported; a filter may name either."

// MCPServerDisplayNamesLookup names the lookup mcp_server reads through: the
// project's hook server-name overrides, raw name to display name.
const MCPServerDisplayNamesLookup = "mcp_server_display_names"

// Lookups is every map a dimension of the v1 catalog reads through.
var Lookups = []*Lookup{
	{
		Name:        MCPServerDisplayNamesLookup,
		Description: "The project's hook server-name overrides, set in Hooks settings: a reported MCP server name with an override shows as its display name, the rest as reported.",
		Load:        nil,
	},
}

// Default is the v1 catalog: every dataset reads agent_events.
var Default = MustCatalog(Lookups, Sessions, ToolCalls, Skills)
