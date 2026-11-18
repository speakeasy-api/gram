package analytics

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var SignalDimension = Type("AnalyticsSignalDimension", func() {
	Description("Exactly one system field or arbitrary attribute key.")
	Attribute("field", String, "System dimension from the analytics catalog: source, role, actor, assistant or definition")
	Attribute("attribute", String, "Exact label key, at most 256 UTF-8 bytes; the shared query engine enforces the byte limit.", func() { MinLength(1) })
})

var SignalFilter = Type("AnalyticsSignalFilter", func() {
	Attribute("dimension", SignalDimension)
	Attribute("operator", String, "Comparison from the analytics catalog: equals, in, exists or missing")
	Attribute("values", ArrayOf(String), func() { MaxLength(100) })
	Required("dimension", "operator")
})

var SignalSelection = Type("AnalyticsSignalSelection", func() {
	Attribute("sensor_id", String, func() { Format(FormatUUID) })
	Attribute("signal_id", String, "For multi-label or exclusive results; omit when minimum_level is set", func() { Format(FormatUUID) })
	Attribute("minimum_level", Int, "Zero-based ordered level; matches cumulative probability at or above this level", func() { Minimum(0); Maximum(9) })
	Attribute("threshold", Float64, "Explicit probability threshold greater than zero. Exclusive results use the selected signal instead.", func() { ExclusiveMinimum(0); Maximum(1) })
	Required("sensor_id", "threshold")
})

var SignalScope = Type("AnalyticsSignalScope", func() {
	Attribute("sensor_id", String, func() { Format(FormatUUID) })
	Attribute("trigger", String, "Evaluation population: stream or manual", func() { Default("stream") })
	Attribute("run_id", String, "Required for manual; absent for stream", func() { Format(FormatUUID) })
	Required("sensor_id")
})

var SignalQuery = Type("AnalyticsSignalQuery", func() {
	Attribute("selection", SignalSelection)
	Attribute("trigger", String, "Evaluation population: stream or manual", func() { Default("stream") })
	Attribute("run_id", String, func() { Format(FormatUUID) })
	Attribute("group_by", ArrayOf(SignalDimension), func() { MaxLength(3) })
	Attribute("filters", ArrayOf(SignalFilter), func() { MaxLength(20) })
	Attribute("groups", Int, "Top groups selected over the whole window", func() { Default(5); Minimum(1); Maximum(20) })
	Attribute("points", Int, "Budget across complete selected series", func() { Default(2000); Minimum(1); Maximum(10000) })
	Required("selection")
})

var SignalMetadata = Type("AnalyticsSignalMetadata", func() {
	Attribute("selection", SignalSelection)
	Attribute("trigger", String)
	Attribute("run_id", String)
	Attribute("step", String)
	Attribute("physical_resolution", String)
	Attribute("time_axis", String)
	Attribute("unit", String)
	Attribute("population", String)
	Attribute("coverage", String)
	Attribute("queried_at", String, func() { Format(FormatDateTime) })
	Attribute("from", String, func() { Format(FormatDateTime) })
	Attribute("to", String, func() { Format(FormatDateTime) })
	Attribute("filters", ArrayOf(SignalFilter))
	Attribute("snapshot", String, "Live, not a replayable historical snapshot")
	Attribute("exact", Boolean)
	Attribute("series_complete", Boolean)
	Attribute("groups_truncated", Boolean)
	Attribute("group_by", ArrayOf(SignalDimension))
	Attribute("point_budget", Int)
	Required("selection", "trigger", "step", "physical_resolution", "time_axis", "unit", "population", "coverage", "queried_at", "from", "to", "filters", "snapshot", "exact", "series_complete", "groups_truncated", "group_by", "point_budget")
})

var SignalResult = Type("AnalyticsSignalResult", func() {
	Attribute("metadata", SignalMetadata)
	Attribute("summary", MapOf(String, Any), "Exact whole-window measures, including groups outside the selected top groups")
	Attribute("groups", ArrayOf(MapOf(String, Any)), "Whole-window totals for selected groups; group_values follows metadata.group_by")
	Required("metadata", "summary", "groups")
})

var SignalSensor = Type("AnalyticsSignalSensor", func() {
	Attribute("id", String)
	Attribute("name", String)
	Attribute("mode", String)
	Attribute("enabled", Boolean)
	Attribute("signal_ids", ArrayOf(String))
	Required("id", "name", "mode", "enabled", "signal_ids")
})

var SignalName = Type("AnalyticsSignalName", func() {
	Attribute("id", String)
	Attribute("name", String)
	Required("id", "name")
})

var SignalCatalog = Type("AnalyticsSignalCatalog", func() {
	Attribute("dataset", String)
	Attribute("unit", String)
	Attribute("population", String)
	Attribute("steps", ArrayOf(String))
	Attribute("operators", ArrayOf(String))
	Attribute("system_dimensions", ArrayOf(String))
	Attribute("initial_attributes", ArrayOf(String))
	Attribute("max_span_days", Int)
	Attribute("max_groups", Int)
	Attribute("max_points", Int)
	Attribute("sensors", ArrayOf(SignalSensor))
	Attribute("signals", ArrayOf(SignalName))
	Required("dataset", "unit", "population", "steps", "operators", "system_dimensions", "initial_attributes", "max_span_days", "max_groups", "max_points", "sensors", "signals")
})

var SignalDefinition = Type("AnalyticsSignalDefinition", func() {
	Attribute("sensor_id", String)
	Attribute("definition_hash", String)
	Attribute("definition", MapOf(String, Any))
	Attribute("captured_at", String, func() { Format(FormatDateTime) })
	Required("sensor_id", "definition_hash", "definition", "captured_at")
})

func signalDefinitionMethod() {
	Method("signalDefinition", func() {
		Description("Inspect the immutable semantic definition used by a signal reading. Requires signals_intelligence and project:read.")
		Payload(func() {
			Attribute("sensor_id", String, func() { Format(FormatUUID) })
			Attribute("definition_hash", String, func() { MinLength(64); MaxLength(64); Pattern("^[a-f0-9]{64}$") })
			Required("sensor_id", "definition_hash")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(SignalDefinition)
		HTTP(func() {
			GET("/rpc/analytics.signalDefinition")
			Param("sensor_id")
			Param("definition_hash")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "analyticsSignalDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "signalDefinition")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AnalyticsSignalDefinition"}`)
	})
}
