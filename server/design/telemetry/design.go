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

	Method("getProjectOverview", func() {
		Description("Get project-level overview including total chats, tool calls, active servers/users, and top lists")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(GetProjectOverviewPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetProjectOverviewResult)

		HTTP(func() {
			POST("/rpc/telemetry.getProjectOverview")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getProjectOverview")
		Meta("openapi:extension:x-speakeasy-name-override", "getProjectOverview")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetProjectOverview", "type": "query"}`)
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

	Method("listAttributeKeys", func() {
		Description("List distinct attribute keys available for filtering")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(ListAttributeKeysPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListAttributeKeysResult)

		HTTP(func() {
			POST("/rpc/telemetry.listAttributeKeys")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listAttributeKeys")
		Meta("openapi:extension:x-speakeasy-name-override", "listAttributeKeys")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListAttributeKeys", "type": "query"}`)
	})

	Method("getHooksSummary", func() {
		Description("Get aggregated hooks metrics grouped by server")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(GetHooksSummaryPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetHooksSummaryResult)

		HTTP(func() {
			POST("/rpc/telemetry.getHooksSummary")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getHooksSummary")
		Meta("openapi:extension:x-speakeasy-name-override", "getHooksSummary")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetHooksSummary", "type": "query"}`)
	})

	Method("listHooksTraces", func() {
		Description("List hook traces aggregated by trace_id with user information")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(ListHooksTracesPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListHooksTracesResult)

		HTTP(func() {
			POST("/rpc/telemetry.listHooksTraces")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listHooksTraces")
		Meta("openapi:extension:x-speakeasy-name-override", "listHooksTraces")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListHooksTraces", "type": "query"}`)
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
	Attribute("event_source", String, "Event source filter (e.g., 'hook', 'tool_call', 'chat_completion')")
})

var LogFilter = Type("LogFilter", func() {
	Description("A single filter condition for a log search query.")

	Attribute("path", String, "Attribute path. Use @ prefix for custom attributes (e.g. '@user.region'), or bare path for system attributes (e.g. 'http.route').", func() {
		// Optional @ prefix for user-defined attributes, then letter/underscore start,
		// then letters/digits/underscores/dots. @ is translated to the internal 'app.' namespace.
		Pattern(`^@?[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)*$`)
		MinLength(1)
		MaxLength(256)
		Example("@user.region")
	})
	Attribute("operator", String, "Comparison operator", func() {
		Enum("eq", "not_eq", "contains", "exists", "not_exists", "in")
		Default("eq")
	})
	Attribute("values", ArrayOf(String), "Values to compare against. Pass one value for single-value operators (eq, not_eq, contains) and multiple for 'in'. Ignored for 'exists' and 'not_exists'.", func() {
		MaxLength(256)
	})
	Required("path")
})

var SearchLogsPayload = Type("SearchLogsPayload", func() {
	Description("Payload for searching telemetry logs")

	Attribute("from", String, "Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("filters", ArrayOf(LogFilter), "Filter conditions for the search query")
	Attribute("filter", SearchLogsFilter, "[Deprecated] Use 'filters' and top-level 'from'/'to' instead.")
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

	Required("logs")
})

var TelemetryLogRecord = Type("TelemetryLogRecord", func() {
	Description("OpenTelemetry log record")

	Attribute("id", String, "Log record ID", func() {
		Format(FormatUUID)
	})
	Attribute("time_unix_nano", String, "Unix time in nanoseconds when event occurred (string for JS int64 precision)")
	Attribute("observed_time_unix_nano", String, "Unix time in nanoseconds when event was observed (string for JS int64 precision)")
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

	Attribute("event_source", String, "Event source filter (e.g., 'hook', 'tool_call', 'chat_completion')")
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

	Required("tool_calls")
})

var ToolCallSummary = Type("ToolCallSummary", func() {
	Description("Summary information for a tool call")

	Attribute("trace_id", String, "Trace ID (32 hex characters)", func() {
		Pattern("^[a-f0-9]{32}$")
	})
	Attribute("start_time_unix_nano", String, "Earliest log timestamp in Unix nanoseconds (string for JS int64 precision)")
	Attribute("log_count", UInt64, "Total number of logs in this tool call")
	Attribute("http_status_code", Int32, "HTTP status code (if applicable)")
	Attribute("gram_urn", String, "Gram URN associated with this tool call")
	Attribute("tool_name", String, "Tool name (from attributes.gram.tool.name)")
	Attribute("tool_source", String, "Tool call source (from attributes.gram.tool_call.source)")
	Attribute("event_source", String, "Event source (from attributes.gram.event.source)")

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

	Required("chats")
})

var ChatSummaryType = Type("ChatSummary", func() {
	Description("Summary information for a chat session")

	Attribute("gram_chat_id", String, "Chat session ID")
	Attribute("start_time_unix_nano", String, "Earliest log timestamp in Unix nanoseconds (string for JS int64 precision)")
	Attribute("end_time_unix_nano", String, "Latest log timestamp in Unix nanoseconds (string for JS int64 precision)")
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

	Required("users")
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
	Attribute("cache_read_input_tokens", Int64, "Sum of cache read input tokens")
	Attribute("cache_creation_input_tokens", Int64, "Sum of cache creation input tokens")
	Attribute("avg_tokens_per_request", Float64, "Average tokens per chat request")

	// Cost
	Attribute("total_cost", Float64, "Total cost of all requests")

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
		"cache_read_input_tokens",
		"cache_creation_input_tokens",
		"total_cost",
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
	Attribute("cache_read_input_tokens", Int64, "Sum of cache read input tokens")
	Attribute("cache_creation_input_tokens", Int64, "Sum of cache creation input tokens")
	Attribute("avg_tokens_per_request", Float64, "Average tokens per chat request")

	// Cost
	Attribute("total_cost", Float64, "Total cost of all requests")

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
		"cache_read_input_tokens",
		"cache_creation_input_tokens",
		"total_cost",
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

	Required("metrics")
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

	Required("metrics")
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
	Attribute("toolset_slug", String, "Optional toolset/MCP server slug filter")
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

	Required("summary", "comparison", "time_series", "top_tools_by_count", "top_tools_by_failure_rate", "interval_seconds")
})

var GetProjectOverviewPayload = Type("GetProjectOverviewPayload", func() {
	Description("Payload for getting project-level overview")

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

var GetProjectOverviewResult = Type("GetProjectOverviewResult", func() {
	Description("Result of project overview query")

	Attribute("summary", ProjectOverviewSummaryType, "Current period summary metrics")
	Attribute("comparison", ProjectOverviewSummaryType, "Previous period summary metrics for trend calculation")
	Attribute("metrics_mode", String, "Indicates whether metrics are session-based or tool-call-based", func() {
		Enum("session", "tool_call")
	})

	Required("summary", "comparison", "metrics_mode")
})

var ObservabilitySummaryType = Type("ObservabilitySummary", func() {
	Description("Aggregated summary metrics for a time period")

	// Chat metrics
	Attribute("total_chats", Int64, "Total number of chat sessions")
	Attribute("resolved_chats", Int64, "Number of resolved chat sessions")
	Attribute("failed_chats", Int64, "Number of failed chat sessions")
	Attribute("avg_session_duration_ms", Float64, "Average session duration in milliseconds")
	Attribute("avg_resolution_time_ms", Float64, "Average time to resolution in milliseconds")

	// Token usage
	Attribute("total_input_tokens", Int64, "Sum of input tokens used")
	Attribute("total_output_tokens", Int64, "Sum of output tokens used")
	Attribute("total_tokens", Int64, "Sum of all tokens used")
	Attribute("cache_read_input_tokens", Int64, "Sum of cache read input tokens")
	Attribute("cache_creation_input_tokens", Int64, "Sum of cache creation input tokens")

	// Cost
	Attribute("total_cost", Float64, "Total cost of all requests")

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
		"total_input_tokens",
		"total_output_tokens",
		"total_tokens",
		"cache_read_input_tokens",
		"cache_creation_input_tokens",
		"total_cost",
		"total_tool_calls",
		"failed_tool_calls",
		"avg_latency_ms",
	)
})

var ProjectOverviewSummaryType = Type("ProjectOverviewSummary", func() {
	Description("Aggregated project-level summary metrics for a time period")

	// Chat metrics
	Attribute("total_chats", Int64, "Total number of chat sessions")
	Attribute("resolved_chats", Int64, "Number of resolved chat sessions")
	Attribute("failed_chats", Int64, "Number of failed chat sessions")

	// Tool metrics
	Attribute("total_tool_calls", Int64, "Total number of tool calls")
	Attribute("failed_tool_calls", Int64, "Number of failed tool calls")

	// Activity counts
	Attribute("active_servers_count", Int64, "Number of MCP servers with at least one tool call in the time period")
	Attribute("active_users_count", Int64, "Number of unique users with activity in the time period")

	// Top lists
	Attribute("top_users", ArrayOf(TopUserType), "Top 10 users by activity (# of messages or tool calls depending on metrics_mode)")
	Attribute("top_servers", ArrayOf(TopServerType), "Top 10 MCP servers by tool call count")
	Attribute("llm_client_breakdown", ArrayOf(LLMClientUsageType), "Breakdown of messages/activity by LLM client/agent")

	Required(
		"total_chats",
		"resolved_chats",
		"failed_chats",
		"total_tool_calls",
		"failed_tool_calls",
		"active_servers_count",
		"active_users_count",
		"top_users",
		"top_servers",
		"llm_client_breakdown",
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

	// Token usage
	Attribute("total_input_tokens", Int64, "Sum of input tokens in this bucket")
	Attribute("total_output_tokens", Int64, "Sum of output tokens in this bucket")
	Attribute("total_tokens", Int64, "Sum of all tokens in this bucket")
	Attribute("cache_read_input_tokens", Int64, "Sum of cache read input tokens in this bucket")
	Attribute("cache_creation_input_tokens", Int64, "Sum of cache creation input tokens in this bucket")

	// Cost
	Attribute("total_cost", Float64, "Total cost in this bucket")

	Attribute("total_tool_calls", Int64, "Total tool calls in this bucket")
	Attribute("failed_tool_calls", Int64, "Failed tool calls in this bucket")
	Attribute("avg_tool_latency_ms", Float64, "Average tool latency in milliseconds")
	Attribute("avg_session_duration_ms", Float64, "Average session duration in milliseconds")

	Required(
		"bucket_time_unix_nano",
		"total_chats",
		"resolved_chats",
		"failed_chats",
		"partial_chats",
		"abandoned_chats",
		"total_input_tokens",
		"total_output_tokens",
		"total_tokens",
		"cache_read_input_tokens",
		"cache_creation_input_tokens",
		"total_cost",
		"total_tool_calls",
		"failed_tool_calls",
		"avg_tool_latency_ms",
		"avg_session_duration_ms",
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

var TopUserType = Type("TopUser", func() {
	Description("Top user by activity")

	Attribute("user_id", String, "User ID (internal or external depending on availability)")
	Attribute("user_type", String, "Type of user ID", func() {
		Enum("internal", "external")
	})
	Attribute("activity_count", Int64, "Number of messages (session mode) or tool calls (tool_call mode)")

	Required("user_id", "user_type", "activity_count")
})

var TopServerType = Type("TopServer", func() {
	Description("Top MCP server by tool call count")

	Attribute("server_name", String, "MCP server name")
	Attribute("tool_call_count", Int64, "Total number of tool calls")

	Required("server_name", "tool_call_count")
})

var LLMClientUsageType = Type("LLMClientUsage", func() {
	Description("Usage breakdown by LLM client/agent")

	Attribute("client_name", String, "Client/agent name (e.g., 'cursor', 'claude-code', 'cowork')")
	Attribute("activity_count", Int64, "Number of messages (session mode) or tool calls (tool_call mode)")

	Required("client_name", "activity_count")
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

	Required("options")
})

var FilterOptionType = Type("FilterOption", func() {
	Description("A single filter option (API key or user)")

	Attribute("id", String, "Unique identifier for the option")
	Attribute("label", String, "Display label for the option")
	Attribute("count", Int64, "Number of events for this option")

	Required("id", "label", "count")
})

// Attribute keys types

var ListAttributeKeysPayload = Type("ListAttributeKeysPayload", func() {
	Description("Payload for listing distinct attribute keys available for filtering")

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

var ListAttributeKeysResult = Type("ListAttributeKeysResult", func() {
	Description("Result of listing distinct attribute keys")

	Attribute("keys", ArrayOf(String), "Distinct attribute keys. User attributes are prefixed with @")

	Required("keys")
})

// Hooks summary types

var GetHooksSummaryPayload = Type("GetHooksSummaryPayload", func() {
	Description("Payload for getting aggregated hooks metrics")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("filters", ArrayOf(LogFilter), "Filter conditions (same as listHooksTraces)")
	Attribute("types_to_include", ArrayOf(String), "Hook types to include (mcp, local, skill). If empty, includes all types.", func() {
		Elem(func() {
			Enum("mcp", "local", "skill")
		})
		Example([]string{"mcp", "skill"})
	})

	Required("from", "to")
})

var GetHooksSummaryResult = Type("GetHooksSummaryResult", func() {
	Description("Result of hooks summary query")

	Attribute("servers", ArrayOf(HooksServerSummaryType), "Aggregated metrics grouped by server")
	Attribute("users", ArrayOf(HooksUserSummaryType), "Aggregated metrics grouped by user")
	Attribute("skills", ArrayOf(SkillSummaryType), "Aggregated metrics grouped by skill")
	Attribute("total_events", Int64, "Total number of hook events")
	Attribute("total_sessions", Int64, "Total number of unique sessions")
	Attribute("breakdown", ArrayOf(HooksBreakdownRowType), "Cross-dimensional pivot: (user, server, source, tool) x counts")
	Attribute("time_series", ArrayOf(HooksTimeSeriesPointType), "Time-bucketed event counts by server and user")
	Attribute("skill_time_series", ArrayOf(SkillTimeSeriesPointType), "Time-bucketed event counts by skill")
	Attribute("skill_breakdown", ArrayOf(SkillBreakdownRowType), "Per-user skill breakdown")

	Required("servers", "users", "skills", "total_events", "total_sessions", "breakdown", "time_series", "skill_time_series", "skill_breakdown")
})

var HooksBreakdownRowType = Type("HooksBreakdownRow", func() {
	Description("Cross-dimensional aggregation row: one entry per unique (user, server, hook_source, tool) combination")

	Attribute("user_email", String, "User email address")
	Attribute("server_name", String, "Server name ('local' for non-MCP tools)")
	Attribute("hook_source", String, "Hook source (e.g. claude-desktop, cursor)")
	Attribute("tool_name", String, "Tool name")
	Attribute("event_count", Int64, "Total events for this combination")
	Attribute("failure_count", Int64, "Number of failures for this combination")

	Required("user_email", "server_name", "hook_source", "tool_name", "event_count", "failure_count")
})

var HooksTimeSeriesPointType = Type("HooksTimeSeriesPoint", func() {
	Description("A single time-series bucket for hooks activity")

	Attribute("bucket_start_ns", String, "Bucket start time in Unix nanoseconds (string for JS int64 precision)")
	Attribute("server_name", String, "Server name")
	Attribute("user_email", String, "User email address")
	Attribute("event_count", Int64, "Number of events in this bucket")
	Attribute("failure_count", Int64, "Number of failed hook events in this bucket")

	Required("bucket_start_ns", "server_name", "user_email", "event_count", "failure_count")
})

var SkillTimeSeriesPointType = Type("SkillTimeSeriesPoint", func() {
	Description("A single time-series bucket for skill usage activity")

	Attribute("bucket_start_ns", String, "Bucket start time in Unix nanoseconds (string for JS int64 precision)")
	Attribute("skill_name", String, "Skill name")
	Attribute("event_count", Int64, "Number of skill use events in this bucket")

	Required("bucket_start_ns", "skill_name", "event_count")
})

var SkillSummaryType = Type("SkillSummary", func() {
	Description("Aggregated skills metrics for a single skill")

	Attribute("skill_name", String, "Skill name (extracted from tool name)")
	Attribute("use_count", Int64, "Total number of times this skill was used")
	Attribute("unique_users", Int64, "Number of unique users who used this skill")

	Required("skill_name", "use_count", "unique_users")
})

var SkillBreakdownRowType = Type("SkillBreakdownRow", func() {
	Description("Per-(skill, user) aggregated counts")

	Attribute("skill_name", String, "Skill name")
	Attribute("user_email", String, "User email address")
	Attribute("use_count", Int64, "Use count for this skill/user combination")

	Required("skill_name", "user_email", "use_count")
})

var HooksServerSummaryType = Type("HooksServerSummary", func() {
	Description("Aggregated hooks metrics for a single server")

	Attribute("server_name", String, "Server name (extracted from tool name, or 'local' for non-MCP tools)")
	Attribute("event_count", Int64, "Total number of hook events for this server")
	Attribute("unique_tools", Int64, "Number of unique tools used for this server")
	Attribute("success_count", Int64, "Number of successful tool completions (PostToolUse events)")
	Attribute("failure_count", Int64, "Number of failed tool completions (PostToolUseFailure events)")
	Attribute("failure_rate", Float64, "Failure rate as a decimal (0.0 to 1.0)")

	Required("server_name", "event_count", "unique_tools", "success_count", "failure_count", "failure_rate")
})

var HooksUserSummaryType = Type("HooksUserSummary", func() {
	Description("Aggregated hooks metrics for a single user")

	Attribute("user_email", String, "User email address")
	Attribute("event_count", Int64, "Total number of hook events for this user")
	Attribute("unique_tools", Int64, "Number of unique tools used by this user")
	Attribute("success_count", Int64, "Number of successful tool completions (PostToolUse events)")
	Attribute("failure_count", Int64, "Number of failed tool completions (PostToolUseFailure events)")
	Attribute("failure_rate", Float64, "Failure rate as a decimal (0.0 to 1.0)")

	Required("user_email", "event_count", "unique_tools", "success_count", "failure_count", "failure_rate")
})

// List hooks traces types

var ListHooksTracesPayload = Type("ListHooksTracesPayload", func() {
	Description("Payload for listing hook traces")

	Attribute("from", String, "Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("filters", ArrayOf(LogFilter), "Filter conditions for the search query")
	Attribute("types_to_include", ArrayOf(String), "Hook types to include (mcp, local, skill). If empty or not provided, includes all types.", func() {
		Elem(func() {
			Enum("mcp", "local", "skill")
		})
		Example([]string{"mcp", "skill"})
	})
	Attribute("cursor", String, "Cursor for pagination (trace_id)")
	Attribute("sort", String, "Sort order", func() {
		Enum("asc", "desc")
		Default("desc")
	})
	Attribute("limit", Int, "Number of items to return (1-1000)", func() {
		Minimum(1)
		Maximum(1000)
		Default(50)
	})

	Required("from", "to")
})

var ListHooksTracesResult = Type("ListHooksTracesResult", func() {
	Description("Result of listing hook traces")

	Attribute("traces", ArrayOf(HookTraceSummary), "List of hook trace summaries")
	Attribute("next_cursor", String, "Cursor for next page")

	Required("traces")
})

var HookTraceSummary = Type("HookTraceSummary", func() {
	Description("Summary information for a hook trace")

	Attribute("trace_id", String, "Trace ID (32 hex characters)", func() {
		Pattern("^[a-f0-9]{32}$")
	})
	Attribute("start_time_unix_nano", String, "Earliest log timestamp in Unix nanoseconds (string for JS int64 precision)")
	Attribute("log_count", UInt64, "Total number of logs in this trace")
	Attribute("hook_status", String, "Hook execution status", func() {
		Enum("success", "failure", "pending", "blocked")
	})
	Attribute("block_reason", String, "Reason set when hook_status is 'blocked' (e.g. shadow-MCP guard rejection)")
	Attribute("gram_urn", String, "Gram URN associated with this hook trace")
	Attribute("tool_name", String, "Tool name (from materialized column)")
	Attribute("tool_source", String, "Tool call source (from materialized column)")
	Attribute("event_source", String, "Event source (from materialized column)")
	Attribute("user_email", String, "User email (from attributes.user.email)")
	Attribute("hook_source", String, "Hook source (from attributes.gram.hook.source)")
	Attribute("skill_name", String, "Skill name (from materialized column, only for Skill tool)")
	Attribute("skill_scope", String, "Skill scope (from materialized column)")
	Attribute("skill_discovery_root", String, "Skill discovery root (from materialized column)")
	Attribute("skill_source_type", String, "Skill source type (from materialized column)")
	Attribute("skill_id", String, "Skill ID (from materialized column)")
	Attribute("skill_version_id", String, "Skill version ID (from materialized column)")
	Attribute("skill_resolution_status", String, "Skill resolution status (from materialized column)")

	Required(
		"trace_id",
		"start_time_unix_nano",
		"log_count",
		"gram_urn",
	)
})
