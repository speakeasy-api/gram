package analytics

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var analyticsOps = []any{"count", "sum", "avg", "min", "max", "p50", "p95", "p99"}
var analyticsOperators = []any{"equals", "in"}
var analyticsGrains = []any{"none", "hour", "day", "week", "month"}

var Measure = Type("AnalyticsMeasure", func() {
	Description("A composed measure: an op over a field, or count alone.")
	Attribute("op", String, "Aggregation to apply. count takes no field; every other op needs a measure field that admits it, per describe.", func() {
		Enum(analyticsOps...)
		Example("sum")
	})
	Attribute("field", String, "Measure field the op applies to. Absent for count.", func() {
		Example("tool_call_count")
	})
	Attribute("alias", String, "Result column name. Defaults to the op, or op_field.", func() {
		Example("tool_calls")
	})
	Required("op")
})

var Filter = Type("AnalyticsFilter", func() {
	Description("A filter on a dimension. All filters are ANDed.")
	Attribute("field", String, "Dimension to filter on", func() { Example("surface") })
	Attribute("operator", String, "Operator the dimension admits, per describe", func() {
		Enum(analyticsOperators...)
	})
	Attribute("values", ArrayOf(String), "equals takes exactly one value; in matches any of them.", func() {
		MinLength(1)
	})
	Required("field", "operator", "values")
})

var OrderBy = Type("AnalyticsOrderBy", func() {
	Description("Sort a grouped result by one of its measures.")
	Attribute("measure", String, "Alias of a requested measure", func() { Example("tool_calls") })
	Attribute("direction", String, func() {
		Enum("asc", "desc")
		Default("desc")
	})
	Required("measure")
})

var QueryPayload = Type("AnalyticsQueryPayload", func() {
	Description("A query against one dataset, in catalog vocabulary. Organization comes from the session and project from the project header; neither is a field.")
	Attribute("dataset", String, "Dataset to query, per describe", func() { Example("sessions") })
	Attribute("from", String, "Start of the half-open window [from, to), ISO 8601", func() {
		Format(FormatDateTime)
		Example("2026-09-01T00:00:00Z")
	})
	Attribute("to", String, "End of the half-open window [from, to), ISO 8601", func() {
		Format(FormatDateTime)
		Example("2026-09-08T00:00:00Z")
	})
	Attribute("grain", String, "Time bucket width for a grouped query. Absent means none.", func() {
		Enum(analyticsGrains...)
	})
	Attribute("dimensions", ArrayOf(String), "Group-by key when grouped; projected columns when ungrouped. At most 3.")
	Attribute("measures", ArrayOf(Measure), "Composed measures. Required when grouped, forbidden when ungrouped.")
	Attribute("filters", ArrayOf(Filter), "Filters, ANDed. At most 100 values per filter.")
	Attribute("order_by", ArrayOf(OrderBy), "Sort for a grouped result, by measure alias. Ungrouped rows are always newest first.")
	Attribute("limit", Int, "Maximum rows. Defaults to 100, at most 1000.", func() {
		Minimum(1)
		Maximum(1000)
	})
	Attribute("ungrouped", Boolean, "Return rows at the dataset's grain instead of aggregating.", func() {
		Default(false)
	})
	Required("dataset", "from", "to")
})

var QueryResult = Type("AnalyticsQueryResult", func() {
	Description("One shape for both modes. Row keys are the requested dimension names plus each measure's alias; time_bucket is present only when grain is set, and time on ungrouped rows.")
	Attribute("dataset", String, "The dataset queried")
	Attribute("plan", String, "A stable diagnostic identifier for how the query was answered. Deliberately not a table name.")
	Attribute("rows", ArrayOf(MapOf(String, Any)), "Result rows")
	Required("dataset", "plan", "rows")
})

var FieldType = Type("AnalyticsField", func() {
	Description("One queryable field on a dataset.")
	Attribute("name", String, func() { Example("user") })
	Attribute("type", String, func() { Enum("string", "int64", "float64") })
	Attribute("role", String, func() { Enum("dimension", "measure") })
	Attribute("unit", String, "Unit of a measure, when it has one", func() { Example("s") })
	Attribute("operators", ArrayOf(String), "Filter operators a dimension admits")
	Attribute("aggregations", ArrayOf(String), "Ops a measure admits")
	Required("name", "type", "role")
})

var DatasetType = Type("AnalyticsDataset", func() {
	Description("A dataset and the fields it exposes. The builder is generated from this.")
	Attribute("name", String, func() { Example("sessions") })
	Attribute("kind", String, func() { Enum("event", "metric") })
	Attribute("grain", String, "What one row represents, as a noun", func() { Example("session") })
	Attribute("description", String)
	Attribute("fields", ArrayOf(FieldType))
	Required("name", "kind", "grain", "description", "fields")
})

var DescribeResult = Type("AnalyticsDescribeResult", func() {
	Attribute("datasets", ArrayOf(DatasetType))
	Required("datasets")
})

var _ = Service("analytics", func() {
	Description("Query agent session data by dataset and field, never by table or SQL. The catalog describe serves is the contract.")

	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("query", func() {
		Description("Run a query against one dataset. Grouped: aggregate over dimensions, optionally bucketed by time. Ungrouped: rows at the dataset's grain, newest first.")

		Payload(func() {
			Extend(QueryPayload)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(QueryResult)

		HTTP(func() {
			POST("/rpc/analytics.query")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "analyticsQuery")
		Meta("openapi:extension:x-speakeasy-name-override", "query")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AnalyticsQuery", "type": "query"}`)
	})

	Method("describe", func() {
		Description("Describe every dataset and the fields it exposes, with the operators and aggregations each admits.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(DescribeResult)

		HTTP(func() {
			GET("/rpc/analytics.describe")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "analyticsDescribe")
		Meta("openapi:extension:x-speakeasy-name-override", "describe")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AnalyticsDescribe"}`)
	})
})
