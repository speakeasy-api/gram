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
	column string
	kind   attributeDimensionKind
}

type telemetryDimension struct {
	aggregateColumn string
	rawExpr         string
	kind            attributeDimensionKind
}

// telemetryDimensionRegistry is the single allowlist for public telemetry
// dimension keys. The aggregate and raw-log query paths use different physical
// expressions for the same public dimensions, but clients can never inject
// arbitrary columns or JSON paths.
var telemetryDimensionRegistry = map[string]telemetryDimension{
	"department_name": {
		aggregateColumn: "department_name",
		rawExpr:         "toString(attributes.user.attributes.department_name)",
		kind:            attributeDimScalar,
	},
	"job_title": {
		aggregateColumn: "job_title",
		rawExpr:         "toString(attributes.user.attributes.job_title)",
		kind:            attributeDimScalar,
	},
	"employee_type": {
		aggregateColumn: "employee_type",
		rawExpr:         "toString(attributes.user.attributes.employee_type)",
		kind:            attributeDimScalar,
	},
	"division_name": {
		aggregateColumn: "division_name",
		rawExpr:         "toString(attributes.user.attributes.division_name)",
		kind:            attributeDimScalar,
	},
	"cost_center_name": {
		aggregateColumn: "cost_center_name",
		rawExpr:         "toString(attributes.user.attributes.cost_center_name)",
		kind:            attributeDimScalar,
	},
	"email": {
		aggregateColumn: "user_email",
		rawExpr:         "user_email",
		kind:            attributeDimScalar,
	},
	"model": {
		aggregateColumn: "model",
		// Source-aware: Claude api_request rows carry the model on
		// attributes.model / gen_ai.request.model, everyone else on
		// gen_ai.response.model. Matches the aggregate MV + session select so a
		// Model filter resolves Claude sessions too (see sessionModelExpr).
		rawExpr: sessionModelExpr,
		kind:    attributeDimScalar,
	},
	"hook_source": {
		aggregateColumn: "hook_source",
		rawExpr:         "hook_source",
		kind:            attributeDimScalar,
	},
	"account_type": {
		// AI account classification: 'team' | 'personal' | '' (unclassified).
		// Materialized on telemetry_logs and a sort-key dimension on
		// attribute_metrics_summaries, so the column name is identical on both paths.
		aggregateColumn: "account_type",
		rawExpr:         "account_type",
		kind:            attributeDimScalar,
	},
	"provider": {
		// AI provider for the account: 'anthropic' | 'openai' | 'cursor' | ''.
		// Materialized on telemetry_logs and a sort-key dimension on
		// attribute_metrics_summaries, so the column name is identical on both paths.
		aggregateColumn: "provider",
		rawExpr:         "provider",
		kind:            attributeDimScalar,
	},
	"query_source": {
		aggregateColumn: "query_source",
		rawExpr:         "toString(attributes.query_source)",
		kind:            attributeDimScalar,
	},
	"skill_name": {
		aggregateColumn: "skill_name",
		rawExpr:         "toString(attributes.skill.name)",
		kind:            attributeDimScalar,
	},
	"agent_name": {
		aggregateColumn: "agent_name",
		rawExpr:         "toString(attributes.agent.name)",
		kind:            attributeDimScalar,
	},
	"mcp_server_name": {
		aggregateColumn: "mcp_server_name",
		rawExpr:         "toString(attributes.mcp_server.name)",
		kind:            attributeDimScalar,
	},
	"mcp_tool_name": {
		aggregateColumn: "mcp_tool_name",
		rawExpr:         "toString(attributes.mcp_tool.name)",
		kind:            attributeDimScalar,
	},
	"role": {
		aggregateColumn: "roles",
		rawExpr:         "arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)'))",
		kind:            attributeDimArray,
	},
	"group": {
		aggregateColumn: "groups",
		rawExpr:         "arraySort(JSONExtract(ifNull(toJSONString(attributes.user.groups), '[]'), 'Array(String)'))",
		kind:            attributeDimArray,
	},
	"project_id": {
		aggregateColumn: "gram_project_id",
		rawExpr:         "gram_project_id",
		kind:            attributeDimProject,
	},
}

func buildDimensionRegistry(columnFor func(telemetryDimension) string) map[string]attributeDimension {
	out := make(map[string]attributeDimension, len(telemetryDimensionRegistry))
	for key, dim := range telemetryDimensionRegistry {
		out[key] = attributeDimension{column: columnFor(dim), kind: dim.kind}
	}
	return out
}

var attributeDimensionRegistry = buildDimensionRegistry(func(dim telemetryDimension) string {
	return dim.aggregateColumn
})

var sessionDimensionRegistry = buildDimensionRegistry(func(dim telemetryDimension) string {
	return dim.rawExpr
})

var chatTurnDimensionRegistry = func() map[string]attributeDimension {
	out := buildDimensionRegistry(func(dim telemetryDimension) string {
		return dim.aggregateColumn
	})
	out["chat_id"] = attributeDimension{column: "chat_id", kind: attributeDimScalar}
	out["turn_id"] = attributeDimension{column: "turn_id", kind: attributeDimScalar}
	out["query_source"] = attributeDimension{column: "query_source", kind: attributeDimScalar}
	out["skill_name"] = attributeDimension{column: "skill_name", kind: attributeDimScalar}
	out["agent_name"] = attributeDimension{column: "agent_name", kind: attributeDimScalar}
	out["mcp_server_name"] = attributeDimension{column: "mcp_server_name", kind: attributeDimScalar}
	out["mcp_tool_name"] = attributeDimension{column: "mcp_tool_name", kind: attributeDimScalar}
	return out
}()
