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
		rawExpr:         "toString(attributes.gen_ai.response.model)",
		kind:            attributeDimScalar,
	},
	"hook_source": {
		aggregateColumn: "hook_source",
		rawExpr:         "hook_source",
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
