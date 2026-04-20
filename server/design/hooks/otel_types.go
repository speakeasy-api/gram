package hooks

import (
	. "goa.design/goa/v3/dsl"
)

// OTEL attribute value supporting string and int types
var OTELAttributeValue = Type("OTELAttributeValue", func() {
	Description("OTEL attribute value - supports stringValue or intValue")
	Attribute("stringValue", String, "String value")
	Attribute("intValue", Int64, "Integer value")
})

// OTEL attribute with key-value pair
var OTELAttribute = Type("OTELAttribute", func() {
	Description("OTEL log attribute with key and typed value")
	Required("key", "value")
	Attribute("key", String, "Attribute key")
	Attribute("value", OTELAttributeValue, "Attribute value")
})

// OTEL log body
var OTELLogBody = Type("OTELLogBody", func() {
	Description("OTEL log body")
	Attribute("stringValue", String, "String body value")
})

// OTEL log record
var OTELLogRecord = Type("OTELLogRecord", func() {
	Description("Individual OTEL log record")
	Required("timeUnixNano", "observedTimeUnixNano", "body", "attributes")
	Attribute("timeUnixNano", String, "Timestamp in nanoseconds since Unix epoch")
	Attribute("observedTimeUnixNano", String, "Observed timestamp in nanoseconds")
	Attribute("body", OTELLogBody, "Log body content")
	Attribute("attributes", ArrayOf(OTELAttribute), "Log attributes")
	Attribute("droppedAttributesCount", Int, "Number of dropped attributes")
})

// OTEL scope
var OTELScope = Type("OTELScope", func() {
	Description("OTEL instrumentation scope")
	Attribute("name", String, "Scope name")
	Attribute("version", String, "Scope version")
})

// OTEL scope logs
var OTELScopeLog = Type("OTELScopeLog", func() {
	Description("OTEL scope logs container")
	Required("logRecords")
	Attribute("scope", OTELScope, "Instrumentation scope information")
	Attribute("logRecords", ArrayOf(OTELLogRecord), "Array of log records")
})

// OTEL resource attribute
var OTELResourceAttribute = Type("OTELResourceAttribute", func() {
	Description("OTEL resource attribute")
	Required("key", "value")
	Attribute("key", String, "Resource attribute key")
	Attribute("value", OTELAttributeValue, "Resource attribute value")
})

// OTEL resource
var OTELResource = Type("OTELResource", func() {
	Description("OTEL resource information")
	Attribute("attributes", ArrayOf(OTELResourceAttribute), "Resource attributes")
	Attribute("droppedAttributesCount", Int, "Number of dropped attributes")
})

// OTEL resource logs
var OTELResourceLog = Type("OTELResourceLog", func() {
	Description("OTEL resource logs container")
	Required("scopeLogs")
	Attribute("resource", OTELResource, "Resource information")
	Attribute("scopeLogs", ArrayOf(OTELScopeLog), "Array of scope logs")
})

// OTEL logs payload
var OTELLogsPayload = Type("OTELLogsPayload", func() {
	Description("OTEL logs export payload")
	Required("resourceLogs")
	Attribute("resourceLogs", ArrayOf(OTELResourceLog), "Array of resource logs")
})

// OTEL metrics types

// OTEL number data point
var OTELNumberDataPoint = Type("OTELNumberDataPoint", func() {
	Description("OTEL number data point")
	Attribute("attributes", ArrayOf(OTELAttribute), "Data point attributes")
	Attribute("startTimeUnixNano", String, "Start timestamp in nanoseconds")
	Attribute("timeUnixNano", String, "Timestamp in nanoseconds")
	Attribute("asDouble", Float64, "Value as double")
	Attribute("asInt", Int64, "Value as integer")
})

// OTEL sum metric
var OTELSum = Type("OTELSum", func() {
	Description("OTEL sum metric")
	Attribute("aggregationTemporality", Int, "Aggregation temporality")
	Attribute("isMonotonic", Boolean, "Whether the sum is monotonic")
	Attribute("dataPoints", ArrayOf(OTELNumberDataPoint), "Data points")
})

// OTEL metric
var OTELMetric = Type("OTELMetric", func() {
	Description("OTEL metric")
	Attribute("name", String, "Metric name")
	Attribute("description", String, "Metric description")
	Attribute("unit", String, "Metric unit")
	Attribute("sum", OTELSum, "Sum metric data")
})

// OTEL scope metrics
var OTELScopeMetrics = Type("OTELScopeMetrics", func() {
	Description("OTEL scope metrics container")
	Attribute("scope", OTELScope, "Instrumentation scope information")
	Attribute("metrics", ArrayOf(OTELMetric), "Array of metrics")
})

// OTEL resource metrics
var OTELResourceMetrics = Type("OTELResourceMetrics", func() {
	Description("OTEL resource metrics container")
	Attribute("resource", OTELResource, "Resource information")
	Attribute("scopeMetrics", ArrayOf(OTELScopeMetrics), "Array of scope metrics")
})

// OTEL metrics payload
var OTELMetricsPayload = Type("OTELMetricsPayload", func() {
	Description("OTEL metrics export payload")
	Required("resourceMetrics")
	Attribute("resourceMetrics", ArrayOf(OTELResourceMetrics), "Array of resource metrics")
})
