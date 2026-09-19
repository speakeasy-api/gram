package analytics

// Sessions is one row per agent session, collapsed from observed events.
var Sessions = &Dataset{
	Name:        "sessions",
	Kind:        KindEvent,
	Grain:       "session",
	Description: "One row per agent session, collapsed from observed events. count counts sessions.",
	TimeExpr:    "started_at",
	Fields: []Field{
		{Name: "session", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "session_id"},
		// A session opens broken down by who was in it: text lives on events,
		// not on the collapsed session row.
		{Name: "user", Type: TypeString, Role: RoleDimension, Default: true, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "user_email"},
		{Name: "model", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "model"},
		{Name: "surface", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "surface"},
		{Name: "provider", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "provider"},
		{Name: "turn_count", Type: TypeInt64, Role: RoleMeasure, Default: false, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg}, Expr: "turn_count"},
		{Name: "tool_call_count", Type: TypeInt64, Role: RoleMeasure, Default: false, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg}, Expr: "tool_call_count"},
		// The time columns are Int64 nanoseconds, hence the division.
		{Name: "duration_seconds", Type: TypeFloat64, Role: RoleMeasure, Default: false, Unit: "s", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg, AggregationP95}, Expr: "(ended_at - started_at) / 1e9"},
	},
	Source: sessionsSource,
}

// ToolCalls is one row per tool call, resolved to its terminal observation.
var ToolCalls = &Dataset{
	Name:        "tool_calls",
	Kind:        KindEvent,
	Grain:       "tool call",
	Description: "One row per tool call, resolved to its latest observation. count counts tool calls; failed calls are count with a status filter.",
	TimeExpr:    "started_at",
	Fields: []Field{
		{Name: "tool_call", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "tool_call_id"},
		{Name: "tool_name", Type: TypeString, Role: RoleDimension, Default: true, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "tool_name"},
		// Every MCP call carries the tool name mcp_tool; the server and tool the
		// producer named are what tell them apart.
		{Name: "mcp_server", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "mcp_server_name"},
		{Name: "mcp_tool", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "mcp_tool_name"},
		{Name: "session", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "session_id"},
		{Name: "user", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "user_email"},
		{Name: "surface", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "surface"},
		// status reads outcome in agent vocabulary, not a protocol status
		// code: ok, error, rejected (a call a decision blocked) or refused (a
		// model declining). describe does not enumerate values, so this is the
		// spec of what a status filter may name.
		{Name: "status", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "status"},
		{Name: "duration_ms", Type: TypeFloat64, Role: RoleMeasure, Default: false, Unit: "ms", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg, AggregationP95}, Expr: "duration_nano / 1e6"},
	},
	Source: toolCallsSource,
}

// Default is the v1 catalog: both datasets read agent_events.
var Default = MustCatalog(Sessions, ToolCalls)
