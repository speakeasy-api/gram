package otel

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("otel", func() {
	Description("Receives OpenTelemetry signals from LLM providers and harnesses.")

	shared.DeclareErrorResponses()

	Method("logs", func() {
		Description("Endpoint to receive OTEL logs data from LLM providers and harnesses.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
		})

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()

			Attribute("content_encoding", String, "Encoding applied to the OTLP request body. Supported values are gzip and identity.")
		})

		Result(Empty)

		HTTP(func() {
			POST("/otel/v1/logs")
			security.ByKeyHeader()
			security.ProjectHeader()
			Header("content_encoding:Content-Encoding")
			Response(StatusOK)
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "uploadLogs")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadLogs")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UploadOpenTelemetryLogs"}`)
	})

	Method("traces", func() {
		Description("Endpoint to receive OTEL traces data from LLM providers and harnesses.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("hooks")
		})

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()

			Attribute("content_encoding", String, "Encoding applied to the OTLP request body. Supported values are gzip and identity.")
		})

		Result(Empty)

		HTTP(func() {
			POST("/otel/v1/traces")
			security.ByKeyHeader()
			security.ProjectHeader()
			Header("content_encoding:Content-Encoding")
			Response(StatusOK)
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "uploadTraces")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadTraces")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UploadOpenTelemetryTraces"}`)
	})

	Method("listEventLog", func() {
		Description("Org-scoped event feed over ingested OpenTelemetry signals: log records and spans merged into one reverse-chronological list with keyset pagination and a capped total count.")

		// Org-scoped dashboard read: unlike the OTLP upload methods above,
		// callers authenticate with a session, not an API key.
		Security(security.Session)

		Payload(func() {
			Extend(ListEventLogPayload)
			security.SessionPayload()
		})

		Result(ListEventLogResult)

		HTTP(func() {
			POST("/rpc/otel.listEventLog")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listEventLog")
		Meta("openapi:extension:x-speakeasy-name-override", "listEventLog")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListEventLog", "type": "query"}`)
	})

	Method("getEventVolume", func() {
		Description("Org-scoped event volume timeseries for the event feed: bucketed counts of ingested OpenTelemetry log records vs spans over a time range, honoring the same filters as listEventLog.")

		// Org-scoped dashboard read: unlike the OTLP upload methods above,
		// callers authenticate with a session, not an API key.
		Security(security.Session)

		Payload(func() {
			Extend(GetEventVolumePayload)
			security.SessionPayload()
		})

		Result(GetEventVolumeResult)

		HTTP(func() {
			POST("/rpc/otel.getEventVolume")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getEventVolume")
		Meta("openapi:extension:x-speakeasy-name-override", "getEventVolume")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetEventVolume", "type": "query"}`)
	})

	Method("getEventFacets", func() {
		Description("Org-scoped filter facets for the event feed: the distinct sources and event/span names observed in a time range.")

		// Org-scoped dashboard read: unlike the OTLP upload methods above,
		// callers authenticate with a session, not an API key.
		Security(security.Session)

		Payload(func() {
			Extend(GetEventFacetsPayload)
			security.SessionPayload()
		})

		Result(GetEventFacetsResult)

		HTTP(func() {
			POST("/rpc/otel.getEventFacets")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getEventFacets")
		Meta("openapi:extension:x-speakeasy-name-override", "getEventFacets")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetEventFacets", "type": "query"}`)
	})
})

var ListEventLogPayload = Type("ListEventLogPayload", func() {
	Description("Payload for listing org-scoped OpenTelemetry events")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-26T10:00:00Z")
	})
	Attribute("kinds", ArrayOf(String, func() {
		Enum("log", "span")
	}), "Optional signal kind filter. Empty means both logs and spans.")
	Attribute("sources", ArrayOf(String), "Optional source filter (canonicalized service names). Values are ORed.")
	Attribute("names", ArrayOf(String), "Optional event/span name filter. Values are ORed.")
	Attribute("search", String, "Optional case-insensitive substring match over event body and name")
	Attribute("limit", Int, "Number of events to return (1-200)", func() {
		Minimum(1)
		Maximum(200)
		Default(50)
	})
	Attribute("cursor", String, "Opaque cursor for pagination")

	Required("from", "to")
})

var ListEventLogResult = Type("ListEventLogResult", func() {
	Description("Result of listing org-scoped OpenTelemetry events")

	Attribute("events", ArrayOf(EventLogEntryType), "Events ordered newest first")
	Attribute("next_cursor", String, "Cursor for next page")
	Attribute("total_count", Int64, "Number of events matching the filters, capped at 10000")
	Attribute("total_count_capped", Boolean, "True when total_count hit the cap and the true count is higher")

	Required("events", "total_count", "total_count_capped")
})

var EventLogEntryType = Type("EventLogEntry", func() {
	Description("A single ingested OpenTelemetry signal (log record or span) in the event feed")

	Attribute("time_unix_nano", String, "Event time in Unix nanoseconds (string for JS int64 precision)")
	Attribute("kind", String, "Signal kind", func() {
		Enum("log", "span")
	})
	Attribute("source", String, "Canonicalized source derived from the resource service name")
	Attribute("name", String, "Event name for logs, span name for spans")
	Attribute("body_preview", String, "Log body truncated to 200 characters; empty for spans")
	Attribute("trace_id", String, "W3C trace ID")
	Attribute("span_id", String, "W3C span ID")
	Attribute("project_id", String, "Project the event was recorded under")
	Attribute("attributes", Any, "Signal attributes as a JSON object")
	Attribute("resource_attributes", Any, "Resource attributes as a JSON object")

	Required(
		"time_unix_nano",
		"kind",
		"source",
		"name",
		"body_preview",
		"trace_id",
		"span_id",
		"project_id",
		"attributes",
		"resource_attributes",
	)
})

var GetEventVolumePayload = Type("GetEventVolumePayload", func() {
	Description("Payload for the org-scoped event volume timeseries")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-26T10:00:00Z")
	})
	Attribute("kinds", ArrayOf(String, func() {
		Enum("log", "span")
	}), "Optional signal kind filter. Empty means both logs and spans.")
	Attribute("sources", ArrayOf(String), "Optional source filter (canonicalized service names). Values are ORed.")
	Attribute("names", ArrayOf(String), "Optional event/span name filter. Values are ORed.")
	Attribute("search", String, "Optional case-insensitive substring match over event body and name")

	Required("from", "to")
})

var GetEventVolumeResult = Type("GetEventVolumeResult", func() {
	Description("Bucketed counts of logs vs spans over the requested range")

	Attribute("interval_seconds", Int64, "Bucket width in seconds")
	Attribute("buckets", ArrayOf(EventVolumeBucketType), "Zero-filled buckets in ascending time order")

	Required("interval_seconds", "buckets")
})

var EventVolumeBucketType = Type("EventVolumeBucket", func() {
	Description("One event volume bucket")

	Attribute("bucket_time_unix_nano", String, "Bucket start in Unix nanoseconds (string for JS int64 precision)")
	Attribute("log_count", Int64, "Log records in this bucket")
	Attribute("span_count", Int64, "Spans in this bucket")

	Required("bucket_time_unix_nano", "log_count", "span_count")
})

var GetEventFacetsPayload = Type("GetEventFacetsPayload", func() {
	Description("Payload for listing event feed filter facets")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-26T10:00:00Z")
	})
	Attribute("kinds", ArrayOf(String, func() {
		Enum("log", "span")
	}), "Optional signal kind filter. Empty means facets from both logs and spans.")

	Required("from", "to")
})

var GetEventFacetsResult = Type("GetEventFacetsResult", func() {
	Description("Distinct filterable values observed in the range")

	Attribute("sources", ArrayOf(String), "Distinct sources in ascending order")
	Attribute("names", ArrayOf(String), "Distinct event/span names in ascending order")

	Required("sources", "names")
})
