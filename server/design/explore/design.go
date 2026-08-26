// Package explore designs the authority-aware semantic query surface over
// event, turn-usage, and user-usage datasets.
//
// The query language and canonicalization semantics are specified in
// server/internal/explore/GRAMMAR.md.
package explore

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("explore", func() {
	Description("Authority-aware ad-hoc analytics over canonical event and usage datasets, with saved queries.")

	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("meta", func() {
		Description("Describe the labeled semantic datasets and typed fields available to Explore.")
		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
		})

		Result(ExploreMetaResult)

		HTTP(func() {
			GET("/rpc/explore.meta")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "exploreMeta")
		Meta("openapi:extension:x-speakeasy-name-override", "meta")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ExploreMeta", "type": "query"}`)
	})

	Method("query", func() {
		Description("Run an organization-scoped query over one semantic dataset. Observations are canonicalized by field authority before filters, grouping, and calculations.")
		Security(security.Session)

		Payload(func() {
			Attribute("from", String, "Start time in ISO 8601 format", func() {
				Format(FormatDateTime)
				Example("2026-08-01T00:00:00Z")
			})
			Attribute("to", String, "End time in ISO 8601 format", func() {
				Format(FormatDateTime)
				Example("2026-08-08T00:00:00Z")
			})
			exploreQuerySpec()
			security.SessionPayload()
			Required("from", "to", "dataset", "calculations")
		})

		Result(ExploreQueryResult)

		HTTP(func() {
			POST("/rpc/explore.query")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "exploreQuery")
		Meta("openapi:extension:x-speakeasy-name-override", "query")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ExploreQuery", "type": "query"}`)
	})

	Method("dimensionValues", func() {
		Description("List a semantic dataset dimension's most frequent canonical values inside a time window.")
		Security(security.Session)

		Payload(func() {
			Attribute("dataset", String, "Semantic dataset carrying the dimension", func() {
				Example("turn_usage")
			})
			Attribute("dimension", String, "Dimension to list values for", func() {
				Example("model")
			})
			Attribute("from", String, "Start time in ISO 8601 format", func() {
				Format(FormatDateTime)
			})
			Attribute("to", String, "End time in ISO 8601 format", func() {
				Format(FormatDateTime)
			})
			security.SessionPayload()
			Required("dataset", "dimension", "from", "to")
		})

		Result(func() {
			Attribute("values", ArrayOf(String), "Distinct non-empty values, most frequent first, capped at 50")
			Required("values")
		})

		HTTP(func() {
			GET("/rpc/explore.dimensionValues")
			Param("dataset")
			Param("dimension")
			Param("from")
			Param("to")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "exploreDimensionValues")
		Meta("openapi:extension:x-speakeasy-name-override", "dimensionValues")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ExploreDimensionValues", "type": "query"}`)
	})

	Method("listSavedQueries", func() {
		Description("List the organization's saved Explore queries.")
		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
		})

		Result(func() {
			Attribute("queries", ArrayOf(ExploreSavedQuery), "Saved queries, most recently updated first")
			Required("queries")
		})

		HTTP(func() {
			GET("/rpc/explore.listSavedQueries")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "exploreListSavedQueries")
		Meta("openapi:extension:x-speakeasy-name-override", "listSavedQueries")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ExploreListSavedQueries", "type": "query"}`)
	})

	Method("createSavedQuery", func() {
		Description("Save a dataset-and-calculations query to the organization's dashboard.")
		Security(security.Session)

		Payload(func() {
			exploreSavedQueryForm()
			security.SessionPayload()
			Required("name", "chart_type", "window", "dataset", "calculations")
		})

		Result(ExploreSavedQuery)

		HTTP(func() {
			POST("/rpc/explore.createSavedQuery")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "exploreCreateSavedQuery")
		Meta("openapi:extension:x-speakeasy-name-override", "createSavedQuery")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ExploreCreateSavedQuery"}`)
	})

	Method("updateSavedQuery", func() {
		Description("Update a saved dataset-and-calculations query in place.")
		Security(security.Session)

		Payload(func() {
			Attribute("id", String, "Saved query ID", func() {
				Format(FormatUUID)
			})
			exploreSavedQueryForm()
			security.SessionPayload()
			Required("id", "name", "chart_type", "window", "dataset", "calculations")
		})

		Result(ExploreSavedQuery)

		HTTP(func() {
			POST("/rpc/explore.updateSavedQuery")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "exploreUpdateSavedQuery")
		Meta("openapi:extension:x-speakeasy-name-override", "updateSavedQuery")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ExploreUpdateSavedQuery"}`)
	})

	Method("deleteSavedQuery", func() {
		Description("Delete a saved Explore query.")
		Security(security.Session)

		Payload(func() {
			Attribute("id", String, "Saved query ID", func() {
				Format(FormatUUID)
			})
			security.SessionPayload()
			Required("id")
		})

		HTTP(func() {
			DELETE("/rpc/explore.deleteSavedQuery")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "exploreDeleteSavedQuery")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteSavedQuery")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ExploreDeleteSavedQuery"}`)
	})
})

func exploreQuerySpec() {
	Attribute("dataset", String, "Semantic dataset to query", func() {
		Enum("events", "turn_usage", "user_usage")
		Example("turn_usage")
	})
	Attribute("calculations", ArrayOf(ExploreCalculation), "Aggregate op(column) calculations to compute over the dataset")
	Attribute("group_by", ArrayOf(String), "Dimensions to break results down by, in order. Empty returns a single aggregate group.", func() {
		Example([]string{"model", "user_key"})
	})
	Attribute("group_expressions", ArrayOf(ExploreGroupExpression), "Named conditional grouping axes evaluated independently on canonical rows. Each produces true and false groups without filtering rows.")
	Attribute("filters", ArrayOf(ExploreFilter), "Filter predicates on canonical fields. All are ANDed together.")
	Attribute("granularity_seconds", Int64, "Timeseries bucket width in seconds. Zero or omitted aggregates the whole range into a table.")
	Attribute("sort_by", String, "Calculation used to rank table rows, referenced by canonical name such as SUM(input_tokens). Must be requested by the query and is ignored for timeseries.")
	Attribute("sort_desc", Boolean, "Sort descending when sort_by is set.", func() {
		Default(true)
	})
	Attribute("limit", Int, "Keep at most this many table rows after ranking. Zero keeps everything.", func() {
		Default(0)
		Minimum(0)
	})
}

func exploreSavedQueryForm() {
	Attribute("name", String, "Display name of the saved query", func() {
		MinLength(1)
		MaxLength(200)
		Example("Input tokens by model")
	})
	Attribute("chart_type", String, "How the dashboard renders the results", func() {
		Enum("line", "bar", "area", "table", "number")
	})
	Attribute("window", String, "Relative time window the query runs over", func() {
		Enum("1h", "24h", "7d", "30d", "90d")
		Example("7d")
	})
	exploreQuerySpec()
}

var ExploreCalculation = Type("ExploreCalculation", func() {
	Description("One aggregate operation over a semantic dataset field.")

	Attribute("op", String, "Aggregate operation", func() {
		Enum("COUNT", "COUNT_DISTINCT", "SUM", "AVG", "MIN", "MAX", "P50", "P95", "P99")
		Example("SUM")
	})
	Attribute("column", String, "Target field. Omit for COUNT.", func() {
		Example("input_tokens")
	})

	Required("op")
})

var ExploreFilter = Type("ExploreFilter", func() {
	Description("One filter predicate over a canonical dataset field.")

	Attribute("dimension", String, "Field to filter on: a dimension, or a measure for numeric operators", func() {
		Example("model")
	})
	Attribute("op", String, "Filter operator. String fields take in, not_in, contains, or exists. Measures take eq, neq, gt, gte, lt, or lte.", func() {
		Enum("in", "not_in", "contains", "exists", "eq", "neq", "gt", "gte", "lt", "lte")
		Default("in")
	})
	Attribute("values", ArrayOf(String), "Operator operands. Numeric comparisons and contains use the first value. Exists ignores values.")

	Required("dimension", "values")
})

var ExploreGroupExpression = Type("ExploreGroupExpression", func() {
	Description("One named conditional grouping axis evaluated over a canonical dataset field.")

	Attribute("name", String, "Customer-facing name returned in the result group_by tuple", func() {
		MinLength(1)
		MaxLength(200)
		Example("Is Claude")
	})
	Attribute("dimension", String, "Field whose value is tested", func() {
		Example("model")
	})
	Attribute("op", String, "Type-appropriate predicate operator. String fields take in, not_in, contains, or exists. Measures take eq, neq, gt, gte, lt, or lte.", func() {
		Enum("in", "not_in", "contains", "exists", "eq", "neq", "gt", "gte", "lt", "lte")
		Default("in")
	})
	Attribute("values", ArrayOf(String), "Predicate operands. Numeric comparisons and contains use the first value. Exists ignores values.")

	Required("name", "dimension", "values")
})

var ExploreRow = Type("ExploreRow", func() {
	Description("One result row of an Explore query.")

	Attribute("bucket", String, "Timeseries bucket start in ISO 8601. Empty for whole-range table queries.")
	Attribute("group", ArrayOf(String), "Group values parallel to group_by. Empty string means the canonical dimension is empty.")
	Attribute("values", MapOf(String, Float64), "Values keyed by canonical calculation name, such as SUM(input_tokens).")

	Required("bucket", "group", "values")
})

var ExploreQueryResult = Type("ExploreQueryResult", func() {
	Description("Result of an Explore query.")

	Attribute("calculations", ArrayOf(String), "Canonical names of the calculations answered, in request order")
	Attribute("dataset", String, "Semantic dataset that answered the query", func() {
		Enum("events", "turn_usage", "user_usage")
	})
	Attribute("group_by", ArrayOf(String), "Group-by dimensions answered, in request order")
	Attribute("granularity_seconds", Int64, "Timeseries bucket width. Zero for table queries.")
	Attribute("rows", ArrayOf(ExploreRow), "Rows ordered by bucket and group for timeseries, or by sort_by for tables")

	Required("calculations", "dataset", "group_by", "granularity_seconds", "rows")
})

var ExploreFieldMeta = Type("ExploreFieldMeta", func() {
	Description("One labeled, typed field of a semantic dataset.")

	Attribute("name", String, "Stable field name", func() {
		Example("input_tokens")
	})
	Attribute("label", String, "Customer-facing display name", func() {
		Example("Input tokens")
	})
	Attribute("type", String, "Value type", func() {
		Enum("string", "float", "int")
	})
	Attribute("role", String, "Field role", func() {
		Enum("dimension", "measure")
	})
	Attribute("unit", String, "Measure unit, or empty for dimensions")
	Attribute("description", String, "What the field carries")
	Attribute("filter_ops", ArrayOf(String), "Row-filter operators legal on the field")

	Required("name", "label", "type", "role", "unit", "description", "filter_ops")
})

var ExploreDatasetSchema = Type("ExploreDatasetSchema", func() {
	Description("One labeled semantic dataset and its typed fields.")

	Attribute("name", String, "Stable dataset ID", func() {
		Enum("events", "turn_usage", "user_usage")
	})
	Attribute("label", String, "Customer-facing display name", func() {
		Example("Turn usage")
	})
	Attribute("category", String, "Customer-facing dataset category", func() {
		Enum("event", "usage")
	})
	Attribute("description", String, "What the dataset contains")
	Attribute("grain", String, "What one canonical row represents")
	Attribute("fields", ArrayOf(ExploreFieldMeta), "Typed fields in display order")

	Required("name", "label", "category", "description", "grain", "fields")
})

var ExploreMetaResult = Type("ExploreMetaResult", func() {
	Description("Semantic datasets available to Explore.")

	Attribute("datasets", ArrayOf(ExploreDatasetSchema), "Labeled semantic dataset schemas in display order")

	Required("datasets")
})

var ExploreSavedQuery = Type("ExploreSavedQuery", func() {
	Description("A saved dataset-and-calculations query with a chart type and relative window.")

	Attribute("id", String, "Saved query ID", func() {
		Format(FormatUUID)
	})
	exploreSavedQueryForm()
	Attribute("created_at", String, "Creation time in ISO 8601 format", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "Last update time in ISO 8601 format", func() {
		Format(FormatDateTime)
	})

	Required("id", "name", "chart_type", "window", "dataset", "calculations", "group_by", "group_expressions", "filters", "granularity_seconds", "sort_by", "sort_desc", "limit", "created_at", "updated_at")
})
