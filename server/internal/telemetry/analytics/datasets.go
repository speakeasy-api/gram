package analytics

// Sessions is one row per agent session, collapsed from observed events.
var Sessions = &Dataset{
	Name:        "sessions",
	Kind:        KindEvent,
	Grain:       "One row per agent session.",
	Description: "One row per agent session, collapsed from observed events. count counts sessions.",
	TimeExpr:    "started_at",
	// A session's headline is who was in it: text lives on events, not on
	// the collapsed session row.
	SummaryField: "user",
	Fields: []Field{
		{Name: "session", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "session_id"},
		{Name: "user", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "user_email"},
		{Name: "model", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "model"},
		{Name: "surface", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "surface"},
		{Name: "provider", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "provider"},
		{Name: "turn_count", Type: TypeInt64, Role: RoleMeasure, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg}, Expr: "turn_count"},
		{Name: "tool_call_count", Type: TypeInt64, Role: RoleMeasure, Unit: "", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg}, Expr: "tool_call_count"},
		// The time columns are Int64 nanoseconds, hence the division.
		{Name: "duration_seconds", Type: TypeFloat64, Role: RoleMeasure, Unit: "s", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg, AggregationP95}, Expr: "(ended_at - started_at) / 1e9"},
	},
	Source: sessionsSource,
}

// ToolCalls is one row per tool call, resolved to its terminal observation.
var ToolCalls = &Dataset{
	Name:         "tool_calls",
	Kind:         KindEvent,
	Grain:        "One row per tool call.",
	Description:  "One row per tool call, resolved to its latest observation. count counts tool calls; failed calls are count with a status filter.",
	TimeExpr:     "started_at",
	SummaryField: "tool_name",
	Fields: []Field{
		{Name: "tool_call", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "tool_call_id"},
		{Name: "tool_name", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "tool_name"},
		{Name: "session", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "session_id"},
		{Name: "user", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "user_email"},
		{Name: "surface", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "surface"},
		// status reads outcome: ok or error in agent vocabulary, not a
		// protocol status code.
		{Name: "status", Type: TypeString, Role: RoleDimension, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "status"},
		{Name: "duration_ms", Type: TypeFloat64, Role: RoleMeasure, Unit: "ms", Operators: nil, Aggregations: []Aggregation{AggregationSum, AggregationAvg, AggregationP95}, Expr: "duration_nano / 1e6"},
	},
	Source: toolCallsSource,
}

// Default is the v1 catalog: both datasets read agent_events.
var Default = MustCatalog(Sessions, ToolCalls)
