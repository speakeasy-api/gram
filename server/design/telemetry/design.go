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

	Method("searchUsers", func() {
		Description("Search and list user usage summaries grouped by user_id or external_user_id")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(SearchUsersPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(SearchUsersResult)

		HTTP(func() {
			POST("/rpc/telemetry.searchUsers")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "searchUsers")
		Meta("openapi:extension:x-speakeasy-name-override", "searchUsers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SearchUsers", "type": "query"}`)
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

	Method("getObservabilityOverview", func() {
		Description("Get observability overview metrics including time series, tool breakdowns, and summary stats")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(GetObservabilityOverviewPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetObservabilityOverviewResult)

		HTTP(func() {
			POST("/rpc/telemetry.getObservabilityOverview")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getObservabilityOverview")
		Meta("openapi:extension:x-speakeasy-name-override", "getObservabilityOverview")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetObservabilityOverview", "type": "query"}`)
	})

	Method("listFilterOptions", func() {
		Description("List available filter options (API keys or users) for the observability overview")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(ListFilterOptionsPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListFilterOptionsResult)

		HTTP(func() {
			POST("/rpc/telemetry.listFilterOptions")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listFilterOptions")
		Meta("openapi:extension:x-speakeasy-name-override", "listFilterOptions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListFilterOptions", "type": "query"}`)
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

var SearchUsersFilter = Type("SearchUsersFilter", func() {
	Description("Filter criteria for searching user usage summaries")

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

	Required("from", "to")
})

var SearchUsersPayload = Type("SearchUsersPayload", func() {
	Description("Payload for searching user usage summaries")

	Attribute("filter", SearchUsersFilter, "Filter criteria for the search")
	Attribute("user_type", String, "Type of user identifier to group by", func() {
		Enum("internal", "external")
	})
	Attribute("cursor", String, "Cursor for pagination (user identifier from last item)")
	Attribute("sort", String, "Sort order", func() {
		Enum("asc", "desc")
		Default("desc")
	})
	Attribute("limit", Int, "Number of items to return (1-1000)", func() {
		Minimum(1)
		Maximum(1000)
		Default(50)
	})

	Required("filter", "user_type")
})

var SearchUsersResult = Type("SearchUsersResult", func() {
	Description("Result of searching user usage summaries")

	Attribute("users", ArrayOf(UserSummaryType), "List of user usage summaries")
	Attribute("next_cursor", String, "Cursor for next page")
	Attribute("enabled", Boolean, "Whether telemetry is enabled for the organization")

	Required("users", "enabled")
})

var UserSummaryType = Type("UserSummary", func() {
	Description("Aggregated usage summary for a single user")

	Attribute("user_id", String, "User identifier (user_id or external_user_id depending on group_by)")

	// Activity timestamps (string for JS int64 precision)
	Attribute("first_seen_unix_nano", String, "Earliest activity timestamp in Unix nanoseconds")
	Attribute("last_seen_unix_nano", String, "Latest activity timestamp in Unix nanoseconds")

	// Chat metrics
	Attribute("total_chats", Int64, "Number of unique chat sessions")
	Attribute("total_chat_requests", Int64, "Total number of chat completion requests")

	// Token usage
	Attribute("total_input_tokens", Int64, "Sum of input tokens used")
	Attribute("total_output_tokens", Int64, "Sum of output tokens used")
	Attribute("total_tokens", Int64, "Sum of all tokens used")
	Attribute("avg_tokens_per_request", Float64, "Average tokens per chat request")

	// Tool calls
	Attribute("total_tool_calls", Int64, "Total number of tool calls")
	Attribute("tool_call_success", Int64, "Successful tool calls (2xx status)")
	Attribute("tool_call_failure", Int64, "Failed tool calls (4xx/5xx status)")

	// Per-tool breakdown
	Attribute("tools", ArrayOf(ToolUsage), "Per-tool usage breakdown")

	Required(
		"user_id",
		"first_seen_unix_nano",
		"last_seen_unix_nano",
		"total_chats",
		"total_chat_requests",
		"total_input_tokens",
		"total_output_tokens",
		"total_tokens",
		"avg_tokens_per_request",
		"total_tool_calls",
		"tool_call_success",
		"tool_call_failure",
		"tools",
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

var ProjectSummaryType = Type("ProjectSummary", func() {
	Description("Aggregated metrics")

	// Activity timestamps (string for JS int64 precision)
	Attribute("first_seen_unix_nano", String, "Earliest activity timestamp in Unix nanoseconds")
	Attribute("last_seen_unix_nano", String, "Latest activity timestamp in Unix nanoseconds")

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

	// Chat resolution metrics (from AI evaluation of chat outcomes)
	Attribute("chat_resolution_success", Int64, "Chats resolved successfully")
	Attribute("chat_resolution_failure", Int64, "Chats that failed to resolve")
	Attribute("chat_resolution_partial", Int64, "Chats partially resolved")
	Attribute("chat_resolution_abandoned", Int64, "Chats abandoned by user")
	Attribute("avg_chat_resolution_score", Float64, "Average chat resolution score (0-100)")

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
		"chat_resolution_success",
		"chat_resolution_failure",
		"chat_resolution_partial",
		"chat_resolution_abandoned",
		"avg_chat_resolution_score",
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

	Attribute("metrics", ProjectSummaryType, "Aggregated metrics")
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

	Attribute("metrics", ProjectSummaryType, "Aggregated metrics for the user")
	Attribute("enabled", Boolean, "Whether telemetry is enabled for the organization")

	Required("metrics", "enabled")
})

// Observability Overview types

var GetObservabilityOverviewPayload = Type("GetObservabilityOverviewPayload", func() {
	Description("Payload for getting observability overview metrics")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("external_user_id", String, "Optional external user ID filter")
	Attribute("api_key_id", String, "Optional API key ID filter")
	Attribute("include_time_series", Boolean, "Whether to include time series data (default: true)", func() {
		Default(true)
	})

	Required("from", "to")
})

var GetObservabilityOverviewResult = Type("GetObservabilityOverviewResult", func() {
	Description("Result of observability overview query")

	Attribute("summary", ObservabilitySummaryType, "Current period summary metrics")
	Attribute("comparison", ObservabilitySummaryType, "Previous period summary metrics for trend calculation")
	Attribute("time_series", ArrayOf(TimeSeriesBucketType), "Time series data points")
	Attribute("top_tools_by_count", ArrayOf(ToolMetricType), "Top tools by call count")
	Attribute("top_tools_by_failure_rate", ArrayOf(ToolMetricType), "Top tools by failure rate")
	Attribute("interval_seconds", Int64, "The time bucket interval in seconds used for the time series data")
	Attribute("enabled", Boolean, "Whether telemetry is enabled for the organization")

	Required("summary", "comparison", "time_series", "top_tools_by_count", "top_tools_by_failure_rate", "interval_seconds", "enabled")
})

var ObservabilitySummaryType = Type("ObservabilitySummary", func() {
	Description("Aggregated summary metrics for a time period")

	// Chat metrics
	Attribute("total_chats", Int64, "Total number of chat sessions")
	Attribute("resolved_chats", Int64, "Number of resolved chat sessions")
	Attribute("failed_chats", Int64, "Number of failed chat sessions")
	Attribute("avg_session_duration_ms", Float64, "Average session duration in milliseconds")
	Attribute("avg_resolution_time_ms", Float64, "Average time to resolution in milliseconds")

	// Tool metrics
	Attribute("total_tool_calls", Int64, "Total number of tool calls")
	Attribute("failed_tool_calls", Int64, "Number of failed tool calls")
	Attribute("avg_latency_ms", Float64, "Average tool latency in milliseconds")

	Required(
		"total_chats",
		"resolved_chats",
		"failed_chats",
		"avg_session_duration_ms",
		"avg_resolution_time_ms",
		"total_tool_calls",
		"failed_tool_calls",
		"avg_latency_ms",
	)
})

var TimeSeriesBucketType = Type("TimeSeriesBucket", func() {
	Description("A single time bucket for time series metrics")

	Attribute("bucket_time_unix_nano", String, "Bucket start time in Unix nanoseconds (string for JS precision)")
	Attribute("total_chats", Int64, "Total chat sessions in this bucket")
	Attribute("resolved_chats", Int64, "Resolved chat sessions in this bucket")
	Attribute("failed_chats", Int64, "Failed chat sessions in this bucket")
	Attribute("partial_chats", Int64, "Partially resolved chat sessions in this bucket")
	Attribute("abandoned_chats", Int64, "Abandoned chat sessions in this bucket")
	Attribute("total_tool_calls", Int64, "Total tool calls in this bucket")
	Attribute("failed_tool_calls", Int64, "Failed tool calls in this bucket")
	Attribute("avg_tool_latency_ms", Float64, "Average tool latency in milliseconds")
	Attribute("avg_session_duration_ms", Float64, "Average session duration in milliseconds")
	Attribute("avg_resolution_time_ms", Float64, "Average resolution time in milliseconds for successfully resolved chats")

	Required(
		"bucket_time_unix_nano",
		"total_chats",
		"resolved_chats",
		"failed_chats",
		"partial_chats",
		"abandoned_chats",
		"total_tool_calls",
		"failed_tool_calls",
		"avg_tool_latency_ms",
		"avg_session_duration_ms",
		"avg_resolution_time_ms",
	)
})

var ToolMetricType = Type("ToolMetric", func() {
	Description("Aggregated metrics for a single tool")

	Attribute("gram_urn", String, "Tool URN")
	Attribute("call_count", Int64, "Total number of calls")
	Attribute("success_count", Int64, "Number of successful calls")
	Attribute("failure_count", Int64, "Number of failed calls")
	Attribute("avg_latency_ms", Float64, "Average latency in milliseconds")
	Attribute("failure_rate", Float64, "Failure rate (0.0 to 1.0)")

	Required("gram_urn", "call_count", "success_count", "failure_count", "avg_latency_ms", "failure_rate")
})

// Filter options types

var ListFilterOptionsPayload = Type("ListFilterOptionsPayload", func() {
	Description("Payload for listing filter options")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("filter_type", String, "Type of filter to list options for", func() {
		Enum("api_key", "user")
	})

	Required("from", "to", "filter_type")
})

var ListFilterOptionsResult = Type("ListFilterOptionsResult", func() {
	Description("Result of listing filter options")

	Attribute("options", ArrayOf(FilterOptionType), "List of filter options")
	Attribute("enabled", Boolean, "Whether telemetry is enabled for the organization")

	Required("options", "enabled")
})

var FilterOptionType = Type("FilterOption", func() {
	Description("A single filter option (API key or user)")

	Attribute("id", String, "Unique identifier for the option")
	Attribute("label", String, "Display label for the option")
	Attribute("count", Int64, "Number of events for this option")

	Required("id", "label", "count")
})
