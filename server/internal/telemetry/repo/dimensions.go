package repo

// attributeDimensionKind classifies how a dimension is grouped/filtered.
type attributeDimensionKind int

const (
	// attributeDimScalar is a plain string value (one value per row).
	attributeDimScalar attributeDimensionKind = iota
	// attributeDimArray is a multi-valued Array(String); grouping arrayJoin()s it
	// and filtering uses hasAny().
	attributeDimArray
	// attributeDimProject is the gram_project_id UUID key.
	attributeDimProject
)

type attributeDimension struct {
	column                 string
	kind                   attributeDimensionKind
	coLocateSessionFilters bool
	emptyIsNotApplicable   bool
	summaryTupleField      string
}

type telemetryDimension struct {
	aggregateColumn        string
	rawExpr                string
	kind                   attributeDimensionKind
	coLocateSessionFilters bool
	// emptyIsNotApplicable marks dimensions where an empty value means the
	// attribute does not apply to the row at all (e.g. a turn that ran no
	// skill), rather than an unclassified/unset population worth surfacing.
	// dimension_values drops '' for these; for every other dimension '' is the
	// real, drillable "(unset)" bucket the breakdown table renders, and
	// consumers (the cost explorer's axis pruning) must be able to count it
	// (DNO-384, DNO-425).
	emptyIsNotApplicable bool
	// summaryColumn is the chat_session_summaries column carrying the
	// dimension's per-chat distinct values (a groupUniqArrayArray-merged
	// Array(String) for scalar/array dimensions; the key column for
	// project_id). Empty for the co-located Claude attribution dimensions,
	// which are matched through summaryTupleField instead.
	summaryColumn string
	// summaryTupleField names the dimension's field inside the
	// chat_session_summaries attribution_tuples named tuple, for the
	// co-located Claude attribution dimensions.
	summaryTupleField string
}

// telemetryDimensionRegistry is the single allowlist for public telemetry
// dimension keys. The aggregate and raw-log query paths use different physical
// expressions for the same public dimensions, but clients can never inject
// arbitrary columns or JSON paths.
var telemetryDimensionRegistry = map[string]telemetryDimension{
	"department_name": {
		aggregateColumn:        "department_name",
		rawExpr:                "toString(attributes.user.attributes.department_name)",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "department_names",
		summaryTupleField:      "",
	},
	"job_title": {
		aggregateColumn:        "job_title",
		rawExpr:                "toString(attributes.user.attributes.job_title)",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "job_titles",
		summaryTupleField:      "",
	},
	"employee_type": {
		aggregateColumn:        "employee_type",
		rawExpr:                "toString(attributes.user.attributes.employee_type)",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "employee_types",
		summaryTupleField:      "",
	},
	"division_name": {
		aggregateColumn:        "division_name",
		rawExpr:                "toString(attributes.user.attributes.division_name)",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "division_names",
		summaryTupleField:      "",
	},
	"cost_center_name": {
		aggregateColumn:        "cost_center_name",
		rawExpr:                "toString(attributes.user.attributes.cost_center_name)",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "cost_center_names",
		summaryTupleField:      "",
	},
	"email": {
		// The user breakdown falls back to the device hostname when a session
		// carries no email: company-credential sessions emit no user identity,
		// but the Go hooks report gram.hook.hostname on every event (and the
		// session cache propagates it onto Claude OTEL rows), so identity-less
		// spend splits per device instead of pooling into one empty bucket.
		// Group, filter, and dimension_values all share this expression, which
		// keeps the hostname buckets drillable. Rows with neither email nor
		// hostname still land in the '' bucket.
		aggregateColumn:        "if(user_email != '', user_email, hook_hostname)",
		rawExpr:                "if(user_email != '', user_email, toString(attributes.gram.hook.hostname))",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "emails",
		summaryTupleField:      "",
	},
	"hostname": {
		// The device hostname the Go hooks report on every event
		// (gram.hook.hostname), propagated onto Claude OTEL rows via the
		// session cache. A standalone per-device breakdown, independent of the
		// email dimension's fallback use of the same column.
		aggregateColumn:        "hook_hostname",
		rawExpr:                "toString(attributes.gram.hook.hostname)",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "hostnames",
		summaryTupleField:      "",
	},
	"model": {
		aggregateColumn: "model",
		// Source-aware: Claude api_request rows carry the model on
		// attributes.model / gen_ai.request.model, everyone else on
		// gen_ai.response.model. Matches the aggregate MV + session select so a
		// Model filter resolves Claude sessions too (see sessionModelExpr).
		rawExpr:                sessionModelExpr,
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "models",
		summaryTupleField:      "",
	},
	"hook_source": {
		aggregateColumn:        "hook_source",
		rawExpr:                "hook_source",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "hook_sources",
		summaryTupleField:      "",
	},
	"account_type": {
		// AI account classification: 'team' | 'personal' | '' (unclassified).
		// Materialized on telemetry_logs and a sort-key dimension on
		// attribute_metrics_summaries, so the column name is identical on both paths.
		aggregateColumn:        "account_type",
		rawExpr:                "account_type",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "account_types",
		summaryTupleField:      "",
	},
	"provider": {
		// AI provider for the account: 'anthropic' | 'openai' | 'cursor' | ''.
		// Materialized on telemetry_logs and a sort-key dimension on
		// attribute_metrics_summaries, so the column name is identical on both paths.
		aggregateColumn:        "provider",
		rawExpr:                "provider",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "providers",
		summaryTupleField:      "",
	},
	"billing_mode": {
		// How the account is billed: 'metered' (pay-per-token; cost is real spend)
		// | 'flat_rate' (subscription seat; cost is an estimate) | 'unknown' | ''.
		// Lets the cost view separate real spend from a token×API-rate estimate.
		// Materialized on telemetry_logs and a sort-key dimension on
		// attribute_metrics_summaries, so the column name is identical on both paths.
		aggregateColumn:        "billing_mode",
		rawExpr:                "billing_mode",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "billing_modes",
		summaryTupleField:      "",
	},
	"query_source": {
		aggregateColumn:        "query_source",
		rawExpr:                "toString(attributes.query_source)",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: true,
		emptyIsNotApplicable:   true,
		summaryColumn:          "",
		summaryTupleField:      "query_source",
	},
	"skill_name": {
		aggregateColumn:        "skill_name",
		rawExpr:                "toString(attributes.skill.name)",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: true,
		emptyIsNotApplicable:   true,
		summaryColumn:          "",
		summaryTupleField:      "skill_name",
	},
	"agent_name": {
		aggregateColumn:        "agent_name",
		rawExpr:                "toString(attributes.agent.name)",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: true,
		emptyIsNotApplicable:   true,
		summaryColumn:          "",
		summaryTupleField:      "agent_name",
	},
	"mcp_server_name": {
		aggregateColumn:        "mcp_server_name",
		rawExpr:                "toString(attributes.mcp_server.name)",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: true,
		emptyIsNotApplicable:   true,
		summaryColumn:          "",
		summaryTupleField:      "mcp_server_name",
	},
	"mcp_tool_name": {
		aggregateColumn:        "mcp_tool_name",
		rawExpr:                "toString(attributes.mcp_tool.name)",
		kind:                   attributeDimScalar,
		coLocateSessionFilters: true,
		emptyIsNotApplicable:   true,
		summaryColumn:          "",
		summaryTupleField:      "mcp_tool_name",
	},
	"role": {
		aggregateColumn:        "roles",
		rawExpr:                "arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)'))",
		kind:                   attributeDimArray,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "roles",
		summaryTupleField:      "",
	},
	"group": {
		aggregateColumn:        "groups",
		rawExpr:                "arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)'))",
		kind:                   attributeDimArray,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "groups",
		summaryTupleField:      "",
	},
	"project_id": {
		aggregateColumn:        "gram_project_id",
		rawExpr:                "gram_project_id",
		kind:                   attributeDimProject,
		coLocateSessionFilters: false,
		emptyIsNotApplicable:   false,
		summaryColumn:          "gram_project_id",
		summaryTupleField:      "",
	},
}

func buildDimensionRegistry(columnFor func(telemetryDimension) string) map[string]attributeDimension {
	out := make(map[string]attributeDimension, len(telemetryDimensionRegistry))
	for key, dim := range telemetryDimensionRegistry {
		out[key] = attributeDimension{
			column:                 columnFor(dim),
			kind:                   dim.kind,
			coLocateSessionFilters: dim.coLocateSessionFilters,
			emptyIsNotApplicable:   dim.emptyIsNotApplicable,
			summaryTupleField:      dim.summaryTupleField,
		}
	}
	return out
}

var attributeDimensionRegistry = buildDimensionRegistry(func(dim telemetryDimension) string {
	return dim.aggregateColumn
})

var sessionDimensionRegistry = buildDimensionRegistry(func(dim telemetryDimension) string {
	return dim.rawExpr
})

// sessionSummaryDimensionRegistry resolves dimensions against the
// chat_session_summaries columns for the summary-backed ListSessions path.
// Co-located attribution dimensions resolve to "" here and are matched via
// summaryTupleField instead.
var sessionSummaryDimensionRegistry = buildDimensionRegistry(func(dim telemetryDimension) string {
	return dim.summaryColumn
})
