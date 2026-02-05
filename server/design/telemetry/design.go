package telemetry

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("telemetry", func() {
	Description("Fetch telemetry data for tools in Gram.")

	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("searchLogs", func() {
		Description("Search and list telemetry logs that match a search filter")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(SearchLogsPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(SearchLogsResult)

		HTTP(func() {
			POST("/rpc/telemetry.searchLogs")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "searchLogs")
		Meta("openapi:extension:x-speakeasy-name-override", "searchLogs")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SearchLogs"}`)
	})

	Method("searchToolCalls", func() {
		Description("Search and list tool calls that match a search filter")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(SearchToolCallsPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(SearchToolCallsResult)

		HTTP(func() {
			POST("/rpc/telemetry.searchToolCalls")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "searchToolCalls")
		Meta("openapi:extension:x-speakeasy-name-override", "searchToolCalls")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SearchToolCalls"}`)
	})

	Method("searchChats", func() {
		Description("Search and list chat session summaries that match a search filter")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(SearchChatsPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(SearchChatsResult)

		HTTP(func() {
			POST("/rpc/telemetry.searchChats")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "searchChats")
		Meta("openapi:extension:x-speakeasy-name-override", "searchChats")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SearchChats", "type": "query"}`)
	})

	Method("captureEvent", func() {
		Description("Capture a telemetry event and forward it to PostHog")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)
		Security(security.ChatSessionsToken)

		Payload(func() {
			Extend(CaptureEventPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
		})

		Result(CaptureEventResult)

		HTTP(func() {
			POST("/rpc/telemetry.captureEvent")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "captureEvent")
		Meta("openapi:extension:x-speakeasy-name-override", "captureEvent")
	})

	Method("getProjectMetricsSummary", func() {
		Description("Get aggregated metrics summary for an entire project")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(GetProjectMetricsSummaryPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetMetricsSummaryResult)

		HTTP(func() {
			POST("/rpc/telemetry.getProjectMetricsSummary")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getProjectMetricsSummary")
		Meta("openapi:extension:x-speakeasy-name-override", "getProjectMetricsSummary")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetProjectMetricsSummary", "type": "query"}`)
	})

	Method("getUserMetricsSummary", func() {
		Description("Get aggregated metrics summary grouped by user")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(GetUserMetricsSummaryPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetUserMetricsSummaryResult)

		HTTP(func() {
			POST("/rpc/telemetry.getUserMetricsSummary")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getUserMetricsSummary")
		Meta("openapi:extension:x-speakeasy-name-override", "getUserMetricsSummary")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetUserMetricsSummary", "type": "query"}`)
	})
})

var TelemetryFilter = Type("TelemetryFilter", func() {
	Description("Filter criteria for telemetry queries")

	Attribute("from", String, "Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("deployment_id", String, "Deployment ID filter", func() {
		Format(FormatUUID)
	})
	Attribute("function_id", String, "Function ID filter", func() {
		Format(FormatUUID)
	})
	Attribute("gram_urn", String, "Gram URN filter (single URN, use gram_urns for multiple)")
})

var SearchLogsFilter = Type("SearchLogsFilter", func() {
	Description("Filter criteria for searching logs")

	Extend(TelemetryFilter)

	Attribute("trace_id", String, "Trace ID filter (32 hex characters)", func() {
		Pattern("^[a-f0-9]{32}$")
	})
	Attribute("severity_text", String, "Severity level filter", func() {
		Enum("DEBUG", "INFO", "WARN", "ERROR", "FATAL")
	})
	Attribute("http_status_code", Int32, "HTTP status code filter")
	Attribute("http_route", String, "HTTP route filter")
	Attribute("http_method", String, "HTTP method filter", func() {
		Enum("GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS")
	})
	Attribute("service_name", String, "Service name filter")
	Attribute("gram_urns", ArrayOf(String), "Gram URN filter (one or more URNs)")
	Attribute("gram_chat_id", String, "Chat ID filter")
	Attribute("user_id", String, "User ID filter")
	Attribute("external_user_id", String, "External user ID filter")
})

var SearchLogsPayload = Type("SearchLogsPayload", func() {
	Description("Payload for searching telemetry logs")

	Attribute("filter", SearchLogsFilter, "Filter criteria for the search")
	Attribute("cursor", String, "Cursor for pagination")
	Attribute("sort", String, "Sort order", func() {
		Enum("asc", "desc")
		Default("desc")
	})
	Attribute("limit", Int, "Number of items to return (1-1000)", func() {
		Minimum(1)
		Maximum(1000)
		Default(50)
	})
})

var SearchLogsResult = Type("SearchLogsResult", func() {
	Description("Result of searching telemetry logs")

	Attribute("logs", ArrayOf(TelemetryLogRecord), "List of telemetry log records")
	Attribute("next_cursor", String, "Cursor for next page")
	Attribute("enabled", Boolean, "Whether tool metrics are enabled for the organization")

	Required("logs", "enabled")
})

var TelemetryLogRecord = Type("TelemetryLogRecord", func() {
	Description("OpenTelemetry log record")

	Attribute("id", String, "Log record ID", func() {
		Format(FormatUUID)
	})
	Attribute("time_unix_nano", Int64, "Unix time in nanoseconds when event occurred")
	Attribute("observed_time_unix_nano", Int64, "Unix time in nanoseconds when event was observed")
	Attribute("severity_text", String, "Text representation of severity")
	Attribute("body", String, "The primary log message")
	Attribute("trace_id", String, "W3C trace ID (32 hex characters)")
	Attribute("span_id", String, "W3C span ID (16 hex characters)")
	Attribute("attributes", Any, "Log attributes as JSON object")
	Attribute("resource_attributes", Any, "Resource attributes as JSON object")
	Attribute("service", ServiceInfo, "Service information")

	Required(
		"id",
		"time_unix_nano",
		"observed_time_unix_nano",
		"body",
		"attributes",
		"resource_attributes",
		"service",
	)
})

var ServiceInfo = Type("ServiceInfo", func() {
	Description("Service information")

	Attribute("name", String, "Service name")
	Attribute("version", String, "Service version")

	Required("name")
})

var SearchToolCallsFilter = Type("SearchToolCallsFilter", func() {
	Description("Filter criteria for searching tool calls")

	Extend(TelemetryFilter)
})

var SearchToolCallsPayload = Type("SearchToolCallsPayload", func() {
	Description("Payload for searching tool call summaries")

	Attribute("filter", SearchToolCallsFilter, "Filter criteria for the search")
	Attribute("cursor", String, "Cursor for pagination")
	Attribute("sort", String, "Sort order", func() {
		Enum("asc", "desc")
		Default("desc")
	})
	Attribute("limit", Int, "Number of items to return (1-1000)", func() {
		Minimum(1)
		Maximum(1000)
		Default(50)
	})
})

var SearchToolCallsResult = Type("SearchToolCallsResult", func() {
	Description("Result of searching tool call summaries")

	Attribute("tool_calls", ArrayOf(ToolCallSummary), "List of tool call summaries")
	Attribute("next_cursor", String, "Cursor for next page")
	Attribute("enabled", Boolean, "Whether tool metrics are enabled for the organization")

	Required("tool_calls", "enabled")
})

var ToolCallSummary = Type("ToolCallSummary", func() {
	Description("Summary information for a tool call")

	Attribute("trace_id", String, "Trace ID (32 hex characters)", func() {
		Pattern("^[a-f0-9]{32}$")
	})
	Attribute("start_time_unix_nano", Int64, "Earliest log timestamp in Unix nanoseconds")
	Attribute("log_count", UInt64, "Total number of logs in this tool call")
	Attribute("http_status_code", Int32, "HTTP status code (if applicable)")
	Attribute("gram_urn", String, "Gram URN associated with this tool call")

	Required(
		"trace_id",
		"start_time_unix_nano",
		"log_count",
		"gram_urn",
	)
})

var SearchChatsFilter = Type("SearchChatsFilter", func() {
	Description("Filter criteria for searching chat sessions")

	Attribute("from", String, "Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("deployment_id", String, "Deployment ID filter", func() {
		Format(FormatUUID)
	})
	Attribute("gram_urn", String, "Gram URN filter (single URN, use gram_urns for multiple)")
	Attribute("user_id", String, "User ID filter")
	Attribute("external_user_id", String, "External user ID filter")
})

var SearchChatsPayload = Type("SearchChatsPayload", func() {
	Description("Payload for searching chat session summaries")

	Attribute("filter", SearchChatsFilter, "Filter criteria for the search")
	Attribute("cursor", String, "Cursor for pagination")
	Attribute("sort", String, "Sort order", func() {
		Enum("asc", "desc")
		Default("desc")
	})
	Attribute("limit", Int, "Number of items to return (1-1000)", func() {
		Minimum(1)
		Maximum(1000)
		Default(50)
	})
})

var SearchChatsResult = Type("SearchChatsResult", func() {
	Description("Result of searching chat session summaries")

	Attribute("chats", ArrayOf(ChatSummaryType), "List of chat session summaries")
	Attribute("next_cursor", String, "Cursor for next page")
	Attribute("enabled", Boolean, "Whether tool metrics are enabled for the organization")

	Required("chats", "enabled")
})

var ChatSummaryType = Type("ChatSummary", func() {
	Description("Summary information for a chat session")

	Attribute("gram_chat_id", String, "Chat session ID")
	Attribute("start_time_unix_nano", Int64, "Earliest log timestamp in Unix nanoseconds")
	Attribute("end_time_unix_nano", Int64, "Latest log timestamp in Unix nanoseconds")
	Attribute("log_count", UInt64, "Total number of logs in this chat session")
	Attribute("tool_call_count", UInt64, "Number of tool calls in this chat session")
	Attribute("message_count", UInt64, "Number of LLM completion messages in this chat session")
	Attribute("duration_seconds", Float64, "Chat session duration in seconds")
	Attribute("status", String, "Chat session status", func() {
		Enum("success", "error")
	})
	Attribute("user_id", String, "User ID associated with this chat session")
	Attribute("model", String, "LLM model used in this chat session")
	Attribute("total_input_tokens", Int64, "Total input tokens used")
	Attribute("total_output_tokens", Int64, "Total output tokens used")
	Attribute("total_tokens", Int64, "Total tokens used (input + output)")

	Required(
		"gram_chat_id",
		"start_time_unix_nano",
		"end_time_unix_nano",
		"log_count",
		"tool_call_count",
		"message_count",
		"duration_seconds",
		"status",
		"total_input_tokens",
		"total_output_tokens",
		"total_tokens",
	)
})

var CaptureEventPayload = Type("CaptureEventPayload", func() {
	Description("Payload for capturing a telemetry event")

	Attribute("event", String, "Event name", func() {
		MinLength(1)
		MaxLength(255)
		Example("button_clicked")
	})
	Attribute("distinct_id", String, "Distinct ID for the user or entity (defaults to organization ID if not provided)")
	Attribute("properties", MapOf(String, Any), "Event properties as key-value pairs", func() {
		Example(map[string]any{
			"button_name": "submit",
			"page":        "checkout",
			"value":       100,
		})
	})

	Required("event")
})

var CaptureEventResult = Type("CaptureEventResult", func() {
	Description("Result of capturing a telemetry event")

	Attribute("success", Boolean, "Whether the event was successfully captured")

	Required("success")
})

// Metrics types

var ModelUsage = Type("ModelUsage", func() {
	Description("Model usage statistics")

	Attribute("name", String, "Model name")
	Attribute("count", Int64, "Number of times used")

	Required("name", "count")
})

var ToolUsage = Type("ToolUsage", func() {
	Description("Tool usage statistics")

	Attribute("urn", String, "Tool URN")
	Attribute("count", Int64, "Total call count")
	Attribute("success_count", Int64, "Successful calls (2xx status)")
	Attribute("failure_count", Int64, "Failed calls (4xx/5xx status)")

	Required("urn", "count", "success_count", "failure_count")
})

var Metrics = Type("Metrics", func() {
	Description("Aggregated metrics")

	// Activity timestamps
	Attribute("first_seen_unix_nano", Int64, "Earliest activity timestamp in Unix nanoseconds")
	Attribute("last_seen_unix_nano", Int64, "Latest activity timestamp in Unix nanoseconds")

	// Token usage
	Attribute("total_input_tokens", Int64, "Sum of input tokens used")
	Attribute("total_output_tokens", Int64, "Sum of output tokens used")
	Attribute("total_tokens", Int64, "Sum of all tokens used")
	Attribute("avg_tokens_per_request", Float64, "Average tokens per chat request")

	// Chat requests
	Attribute("total_chat_requests", Int64, "Total number of chat requests")
	Attribute("avg_chat_duration_ms", Float64, "Average chat request duration in milliseconds")

	// Resolution status (count of each finish reason)
	Attribute("finish_reason_stop", Int64, "Requests that completed naturally")
	Attribute("finish_reason_tool_calls", Int64, "Requests that resulted in tool calls")

	// Tool calls
	Attribute("total_tool_calls", Int64, "Total number of tool calls")
	Attribute("tool_call_success", Int64, "Successful tool calls (2xx status)")
	Attribute("tool_call_failure", Int64, "Failed tool calls (4xx/5xx status)")
	Attribute("avg_tool_duration_ms", Float64, "Average tool call duration in milliseconds")

	// Cardinality (project scope only, 0 for chat scope)
	Attribute("total_chats", Int64, "Number of unique chat sessions (project scope only)")
	Attribute("distinct_models", Int64, "Number of distinct models used (project scope only)")
	Attribute("distinct_providers", Int64, "Number of distinct providers used (project scope only)")

	// Detailed breakdowns
	Attribute("models", ArrayOf(ModelUsage), "List of models used with call counts")
	Attribute("tools", ArrayOf(ToolUsage), "List of tools used with success/failure counts")

	Required(
		"first_seen_unix_nano",
		"last_seen_unix_nano",
		"total_input_tokens",
		"total_output_tokens",
		"total_tokens",
		"avg_tokens_per_request",
		"total_chat_requests",
		"avg_chat_duration_ms",
		"finish_reason_stop",
		"finish_reason_tool_calls",
		"total_tool_calls",
		"tool_call_success",
		"tool_call_failure",
		"avg_tool_duration_ms",
		"total_chats",
		"distinct_models",
		"distinct_providers",
		"models",
		"tools",
	)
})

var GetProjectMetricsSummaryPayload = Type("GetProjectMetricsSummaryPayload", func() {
	Description("Payload for getting project-level metrics summary")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})

	Required("from", "to")
})

var GetMetricsSummaryResult = Type("GetMetricsSummaryResult", func() {
	Description("Result of metrics summary query")

	Attribute("metrics", Metrics, "Aggregated metrics")
	Attribute("enabled", Boolean, "Whether telemetry is enabled for the organization")

	Required("metrics", "enabled")
})

// User metrics types

var GetUserMetricsSummaryPayload = Type("GetUserMetricsSummaryPayload", func() {
	Description("Payload for getting user-level metrics summary")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("user_id", String, "User ID to get metrics for (mutually exclusive with external_user_id)")
	Attribute("external_user_id", String, "External user ID to get metrics for (mutually exclusive with user_id)")

	Required("from", "to")
})

var GetUserMetricsSummaryResult = Type("GetUserMetricsSummaryResult", func() {
	Description("Result of user metrics summary query")

	Attribute("metrics", Metrics, "Aggregated metrics for the user")
	Attribute("enabled", Boolean, "Whether telemetry is enabled for the organization")

	Required("metrics", "enabled")
})
