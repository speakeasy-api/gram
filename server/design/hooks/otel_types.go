package hooks

import (
	. "goa.design/goa/v3/dsl"
)

// OTEL attribute value supporting all OTLP/JSON value kinds.
//
// Per the OTLP/JSON spec, 64-bit integer fields (intValue) are encoded as JSON
// strings, not numbers. We accept all known value kinds so the decoder never
// rejects a well-formed payload; handlers only read the kinds they care about.
var OTELAttributeValue = Type("OTELAttributeValue", func() {
	Description("OTEL attribute value - any of the OTLP/JSON value kinds")
	Attribute("stringValue", String, "String value")
	Attribute("intValue", String, "Integer value (string-encoded per OTLP/JSON)")
	Attribute("boolValue", Boolean, "Boolean value")
	Attribute("doubleValue", Float64, "Double value")
	Attribute("arrayValue", Any, "Array value (passed through)")
	Attribute("kvlistValue", Any, "Key-value list value (passed through)")
	Attribute("bytesValue", String, "Bytes value (base64-encoded per OTLP/JSON)")
})

// OTEL attribute with key-value pair
var OTELAttribute = Type("OTELAttribute", func() {
	Description("OTEL log attribute with key and typed value")
	Required("key")
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
	Attribute("scope", OTELScope, "Instrumentation scope information")
	Attribute("logRecords", ArrayOf(OTELLogRecord), "Array of log records")
})

// OTEL resource attribute
var OTELResourceAttribute = Type("OTELResourceAttribute", func() {
	Description("OTEL resource attribute")
	Required("key")
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
	Attribute("resource", OTELResource, "Resource information")
	Attribute("scopeLogs", ArrayOf(OTELScopeLog), "Array of scope logs")
})

// OTEL logs payload
var OTELLogsPayload = Type("OTELLogsPayload", func() {
	Description("OTEL logs export payload")
	Attribute("resourceLogs", ArrayOf(OTELResourceLog), "Array of resource logs")
})

// OTEL metrics types

// OTEL number data point.
//
// Per OTLP/JSON, asInt is string-encoded; asDouble remains a JSON number.
var OTELNumberDataPoint = Type("OTELNumberDataPoint", func() {
	Description("OTEL number data point")
	Attribute("attributes", ArrayOf(OTELAttribute), "Data point attributes")
	Attribute("startTimeUnixNano", String, "Start timestamp in nanoseconds")
	Attribute("timeUnixNano", String, "Timestamp in nanoseconds")
	Attribute("asDouble", Float64, "Value as double")
	Attribute("asInt", String, "Value as integer (string-encoded per OTLP/JSON)")
})

// OTEL sum metric.
//
// aggregationTemporality is Any because OTLP/JSON producers emit either the
// numeric enum (1, 2) or the string form ("AGGREGATION_TEMPORALITY_DELTA").
var OTELSum = Type("OTELSum", func() {
	Description("OTEL sum metric")
	Attribute("aggregationTemporality", Any, "Aggregation temporality (number or enum string)")
	Attribute("isMonotonic", Boolean, "Whether the sum is monotonic")
	Attribute("dataPoints", ArrayOf(OTELNumberDataPoint), "Data points")
})

// OTEL metric. Gauge/Histogram/Summary kinds are accepted opaquely so unknown
// metric shapes don't fail the whole batch — they're simply skipped downstream.
var OTELMetric = Type("OTELMetric", func() {
	Description("OTEL metric")
	Attribute("name", String, "Metric name")
	Attribute("description", String, "Metric description")
	Attribute("unit", String, "Metric unit")
	Attribute("sum", OTELSum, "Sum metric data")
	Attribute("gauge", Any, "Gauge metric data (passed through)")
	Attribute("histogram", Any, "Histogram metric data (passed through)")
	Attribute("exponentialHistogram", Any, "ExponentialHistogram metric data (passed through)")
	Attribute("summary", Any, "Summary metric data (passed through)")
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
	Attribute("resourceMetrics", ArrayOf(OTELResourceMetrics), "Array of resource metrics")
})
