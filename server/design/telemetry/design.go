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

	Method("getEmployeeDataFlowGraph", func() {
		Description("Get an employee's MCP data flow graph across origins, clients, servers, and tools")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(GetEmployeeDataFlowGraphPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetEmployeeDataFlowGraphResult)

		HTTP(func() {
			POST("/rpc/telemetry.getEmployeeDataFlowGraph")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getEmployeeDataFlowGraph")
		Meta("openapi:extension:x-speakeasy-name-override", "getEmployeeDataFlowGraph")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetEmployeeDataFlowGraph", "type": "query"}`)
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

	Method("query", func() {
		Description("Generic, org-scoped analytics query over pre-aggregated usage metrics. Returns both a grouped table and a per-group hourly timeseries for the same slice of data, supporting arbitrary allowlisted group-by dimensions and filters (e.g. group by department_name, then drill in by filtering department_name and grouping by role).")

		// Org-scoped: the query spans every project in the caller's
		// organization. project_id is an optional filter, not the auth scope.
		Security(security.Session)

		Payload(func() {
			Extend(QueryPayload)
			security.SessionPayload()
		})

		Result(QueryResult)

		HTTP(func() {
			POST("/rpc/telemetry.query")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "query")
		Meta("openapi:extension:x-speakeasy-name-override", "query")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "TelemetryQuery", "type": "query"}`)
	})

	Method("queryRiskTokens", func() {
		Description("Org-scoped daily token usage split by risk involvement: tokens from sessions with at least one active risk finding in the window versus all session tokens. Powers the token-usage panel's risk breakdown on the costs page.")

		// Org-scoped like telemetry.query; project_id optionally narrows the
		// slice to one of the caller's projects.
		Security(security.Session)

		Payload(func() {
			Extend(QueryRiskTokensPayload)
			security.SessionPayload()
		})

		Result(QueryRiskTokensResult)

		HTTP(func() {
			POST("/rpc/telemetry.queryRiskTokens")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "queryRiskTokens")
		Meta("openapi:extension:x-speakeasy-name-override", "queryRiskTokens")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "TelemetryQueryRiskTokens", "type": "query"}`)
	})

	Method("queryMessageTokenStats", func() {
		Description("Org-scoped daily message-level token stats: tokens in messages carrying at least one active risk finding and tokens in tool-call messages. Powers the billing page's usage details table.")

		// Org-scoped like queryRiskTokens; project_id optionally narrows the
		// slice to one of the caller's projects.
		Security(security.Session)

		Payload(func() {
			Extend(MessageTokenStatsPayload)
			security.SessionPayload()
		})

		Result(MessageTokenStatsResult)

		HTTP(func() {
			POST("/rpc/telemetry.queryMessageTokenStats")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "queryMessageTokenStats")
		Meta("openapi:extension:x-speakeasy-name-override", "queryMessageTokenStats")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "TelemetryQueryMessageTokenStats", "type": "query"}`)
	})

	Method("listSessions", func() {
		Description("Org-scoped list of individual chat sessions for a slice of usage, filtered by the same allowlisted dimensions as telemetry.query. Returns per-session cost, token, and tool metrics with cursor pagination.")

		// Org-scoped: the query spans every project in the caller's
		// organization. project_id is an optional filter, not the auth scope.
		Security(security.Session)

		Payload(func() {
			Extend(ListSessionsPayload)
			security.SessionPayload()
		})

		Result(ListSessionsResult)

		HTTP(func() {
			POST("/rpc/telemetry.listSessions")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listSessions")
		Meta("openapi:extension:x-speakeasy-name-override", "listSessions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListSessions", "type": "query"}`)
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

	Method("getToolUsageSummary", func() {
		Description("Get target-aware MCP and tool usage metrics")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(GetToolUsageSummaryPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetToolUsageSummaryResult)

		HTTP(func() {
			POST("/rpc/telemetry.getToolUsageSummary")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getToolUsageSummary")
		Meta("openapi:extension:x-speakeasy-name-override", "getToolUsageSummary")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetToolUsageSummary", "type": "query"}`)
	})

	Method("listToolUsageTraces", func() {
		Description("List target-aware MCP and tool usage traces")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(ListToolUsageTracesPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListToolUsageTracesResult)

		HTTP(func() {
			POST("/rpc/telemetry.listToolUsageTraces")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listToolUsageTraces")
		Meta("openapi:extension:x-speakeasy-name-override", "listToolUsageTraces")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListToolUsageTraces", "type": "query"}`)
	})

	Method("getToolUsageFilterOptions", func() {
		Description("Get filter options for target-aware MCP and tool usage metrics")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(GetToolUsageFilterOptionsPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(GetToolUsageFilterOptionsResult)

		HTTP(func() {
			POST("/rpc/telemetry.getToolUsageFilterOptions")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getToolUsageFilterOptions")
		Meta("openapi:extension:x-speakeasy-name-override", "getToolUsageFilterOptions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetToolUsageFilterOptions", "type": "query"}`)
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

var ListSessionsPayload = Type("ListSessionsPayload", func() {
	Description("Payload for listing org-scoped chat sessions")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-26T10:00:00Z")
	})
	Attribute("filters", ArrayOf(QueryFilter), "Optional filters; all filters are ANDed together.")
	Attribute("sort_by", String, "Measure used to rank sessions. Defaults to total_cost.", func() {
		Enum("total_cost", "total_tokens", "total_input_tokens", "total_output_tokens", "tool_call_count", "message_count", "duration_seconds")
		Default("total_cost")
	})
	Attribute("limit", Int, "Number of sessions to return (1-1000)", func() {
		Minimum(1)
		Maximum(1000)
		Default(50)
	})
	Attribute("cursor", String, "Opaque cursor for pagination")

	Required("from", "to")
})

var ListSessionsResult = Type("ListSessionsResult", func() {
	Description("Result of listing org-scoped chat sessions")

	Attribute("sessions", ArrayOf(SessionSummaryType), "List of chat session summaries")
	Attribute("next_cursor", String, "Cursor for next page")

	Required("sessions")
})

var SessionSummaryType = Type("SessionSummary", func() {
	Description("Org-scoped summary information for a chat session")

	Attribute("gram_chat_id", String, "Chat session ID")
	Attribute("project_id", String, "Project ID that emitted this chat session")
	Attribute("user_email", String, "User email associated with this chat session")
	Attribute("hook_source", String, "Client or agent surface associated with this chat session")
	Attribute("model", String, "LLM model used in this chat session")
	Attribute("title", String, "Chat title, when the session resolves to a named chat")
	Attribute("start_time_unix_nano", String, "Earliest log timestamp in Unix nanoseconds (string for JS int64 precision)")
	Attribute("end_time_unix_nano", String, "Latest log timestamp in Unix nanoseconds (string for JS int64 precision)")
	Attribute("duration_seconds", Float64, "Chat session duration in seconds")
	Attribute("message_count", Int64, "Number of LLM completion messages in this chat session")
	Attribute("tool_call_count", Int64, "Number of tool calls in this chat session")
	Attribute("total_input_tokens", Int64, "Total input tokens used")
	Attribute("total_output_tokens", Int64, "Total output tokens used")
	Attribute("total_tokens", Int64, "Total tokens used")
	Attribute("total_cost", Float64, "Total cost in USD")
	Attribute("status", String, "Chat session status", func() {
		Enum("success", "error")
	})

	Required(
		"gram_chat_id",
		"project_id",
		"start_time_unix_nano",
		"end_time_unix_nano",
		"duration_seconds",
		"message_count",
		"tool_call_count",
		"total_input_tokens",
		"total_output_tokens",
		"total_tokens",
		"total_cost",
		"status",
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
	Attribute("user_ids", ArrayOf(String), "Optional list of user identifiers to include. Matches user_id for internal searches and external_user_id for external searches.")
	Attribute("event_source", String, "Optional event source filter (e.g. 'hook'). When set, only rows with a matching event_source are included.")
	Attribute("hook_source", String, "Optional hook source filter (e.g. 'cursor', 'claude-code').")
	Attribute("account_type", String, "Optional account type filter ('team' or 'personal').")
	Attribute("external_org_id", String, "Optional filter to a single AI account by its provider org id (the per-account discriminator); scopes results to that one account.")

	Required("from", "to")
})

var SearchUsersPayload = Type("SearchUsersPayload", func() {
	Description("Payload for searching user usage summaries")

	Attribute("filter", SearchUsersFilter, "Filter criteria for the search")
	Attribute("user_type", String, "Type of user identifier to group by", func() {
		Enum("internal", "external")
	})
	Attribute("group_by", String, "Grouping dimension for results", func() {
		Enum("employee", "role")
		Default("employee")
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

	Attribute("users", ArrayOf(UserSummaryType), "List of user usage summaries (populated when group_by=employee)")
	Attribute("roles", ArrayOf(RoleSummaryType), "List of role usage summaries (populated when group_by=role)")
	Attribute("next_cursor", String, "Cursor for next page")

	Required("users")
})

var RoleSummaryType = Type("RoleSummary", func() {
	Description("Aggregated usage summary for a role")

	Attribute("role_id", String, "Role identifier extracted from role URN")
	Attribute("role_name", String, "Human-readable role name")
	Attribute("user_count", Int, "Number of users with this role")
	Attribute("total_cost", Float64, "Total cost across all users with this role")
	Attribute("cost_per_user", Float64, "Average cost per user (total_cost / user_count)")
	Attribute("total_input_tokens", Int64, "Sum of input tokens across all users")
	Attribute("total_output_tokens", Int64, "Sum of output tokens across all users")
	Attribute("total_tokens", Int64, "Sum of all tokens across all users")
	Attribute("total_chats", Int64, "Total chat sessions across all users")

	Required(
		"role_id",
		"role_name",
		"user_count",
		"total_cost",
		"cost_per_user",
		"total_input_tokens",
		"total_output_tokens",
		"total_tokens",
		"total_chats",
	)
})

var UserSummaryType = Type("UserSummary", func() {
	Description("Aggregated usage summary for a single user")

	Attribute("user_id", String, "User identifier (user_id or external_user_id depending on group_by)")
	Attribute("user_email", String, "User email associated with this usage, when present")

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
	Attribute("hook_sources", ArrayOf(HookSourceUsage), "Per-hook-source usage breakdown")

	// Distinct AI account types observed for this user in the window ('team',
	// 'personal'). Lets the employees list flag who is also driving
	// personal-account usage. Empty for older telemetry without the dimension.
	Attribute("account_types", ArrayOf(String), "Distinct account types observed for this user ('team', 'personal')")

	// Registered AI accounts for this user from the user_accounts directory
	// (identity, not windowed telemetry). Each (provider, email) is a distinct
	// account, so a user can hold several across providers. Drives the per-user
	// accounts breakdown on the employees list.
	Attribute("accounts", ArrayOf(UserAccountType), "Linked AI accounts for this user (team and personal, across providers)")

	Required(
		"user_id",
		"user_email",
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
		"hook_sources",
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

var HookSourceUsage = Type("HookSourceUsage", func() {
	Description("Hook source usage statistics")

	Attribute("source", String, "Hook source (from attributes.gram.hook.source)")
	Attribute("event_count", Int64, "Total hook events for this source")

	Required("source", "event_count")
})

var UserAccountType = Type("UserAccount", func() {
	Description("A linked AI account for a user. The identity is (provider, email): the same email registered on two providers is two distinct accounts.")

	Attribute("id", String, "Account record id (user_accounts.id); used to scope chat/session views to this account")
	Attribute("provider", String, "AI provider the account belongs to ('anthropic', 'openai', 'cursor')")
	Attribute("email", String, "Email associated with the account; may differ from the user's work email for personal accounts")
	Attribute("account_type", String, "'team' (enterprise) or 'personal' (individual); empty when not yet classified")
	Attribute("external_org_id", String, "Provider org id for this account; the per-account discriminator used to scope telemetry to this one account")
	Attribute("last_seen_unix_nano", String, "Latest activity timestamp for this account in Unix nanoseconds")

	Required("provider")
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
	Attribute("event_source", String, "Optional event source filter (e.g. 'hook')")
	Attribute("hook_source", String, "Optional hook source filter (e.g. 'cursor', 'claude-code')")
	Attribute("account_type", String, "Optional account type filter ('team' or 'personal')")
	Attribute("external_org_id", String, "Optional filter to a single AI account by its provider org id; scopes metrics to that one account")

	Required("from", "to")
})

var GetUserMetricsSummaryResult = Type("GetUserMetricsSummaryResult", func() {
	Description("Result of user metrics summary query")

	Attribute("metrics", ProjectSummaryType, "Aggregated metrics for the user")

	Required("metrics")
})

// Employee data flow graph types

var GetEmployeeDataFlowGraphPayload = Type("GetEmployeeDataFlowGraphPayload", func() {
	Description("Payload for getting an employee-level MCP data flow graph")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("user_id", String, "User ID to get the graph for (mutually exclusive with external_user_id)")
	Attribute("external_user_id", String, "External user ID to get the graph for (mutually exclusive with user_id)")
	Attribute("account_type", String, "Optional account type filter ('team' or 'personal')")
	Attribute("external_org_id", String, "Optional filter to a single AI account by its provider org id; scopes the graph to that one account")

	Required("from", "to")
})

var GetEmployeeDataFlowGraphResult = Type("GetEmployeeDataFlowGraphResult", func() {
	Description("Result of employee data flow graph query")

	Attribute("nodes", ArrayOf(EmployeeDataFlowNode), "Graph nodes grouped by tier")
	Attribute("edges", ArrayOf(EmployeeDataFlowEdge), "Weighted graph edges between adjacent populated tiers")

	Required("nodes", "edges")
})

var EmployeeDataFlowNode = Type("EmployeeDataFlowNode", func() {
	Description("A node in the employee data flow graph")

	Attribute("id", String, "Stable node ID")
	Attribute("tier", String, "Graph tier. Origin nodes identify the hostname or client context that started the call, not the MCP server URL.", func() {
		Enum("origin", "client", "server", "tool")
	})
	Attribute("label", String, "Display label")
	Attribute("total_calls", Int64, "Total calls involving this node")
	Attribute("server_class", String, "Server classification, present for MCP server nodes", func() {
		Enum("gram", "external", "local")
	})

	Required("id", "tier", "label", "total_calls")
})

var EmployeeDataFlowEdge = Type("EmployeeDataFlowEdge", func() {
	Description("A weighted edge in the employee data flow graph")

	Attribute("id", String, "Stable edge ID")
	Attribute("source", String, "Source node ID")
	Attribute("target", String, "Target node ID")
	Attribute("call_count", Int64, "Total calls represented by this edge")
	Attribute("success_count", Int64, "Successful calls represented by this edge")
	Attribute("failure_count", Int64, "Failed or blocked calls represented by this edge")

	Required("id", "source", "target", "call_count", "success_count", "failure_count")
})

// Observability Overview types

// queryDimensions is the allowlist of dimensions that telemetry.query may
// group by or filter on. Each maps to a safe column expression in the repo
// layer; "role"/"group" are multi-valued (arrayJoin to group, has() to
// filter). Clients can never supply arbitrary JSON paths or SQL.
var queryDimensions = []any{
	"department_name",
	"job_title",
	"employee_type",
	"division_name",
	"cost_center_name",
	"email",
	"model",
	"hook_source",  // consuming surface (claude-code, cowork, cursor, ...)
	"account_type", // AI account classification (team | personal | unclassified)
	"provider",     // AI provider for the account (anthropic | openai | cursor)
	"billing_mode", // metered (real cost) | flat_rate (estimate) | unknown
	"query_source",
	"skill_name",
	"agent_name",
	"mcp_server_name",
	"mcp_tool_name",
	"role",
	"group",
	"project_id",
}

// queryMeasures is the allowlist of measures available for ranking (sort_by).
// Every measure is always returned in QueryMeasures regardless of this choice.
var queryMeasures = []any{
	"total_cost",
	"total_tokens",
	"total_input_tokens",
	"total_output_tokens",
	"cache_read_input_tokens",
	"cache_creation_input_tokens",
	"total_tool_calls",
	"total_chats",
}

var QueryPayload = Type("QueryPayload", func() {
	Description("Payload for a generic org-scoped analytics query")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-26T10:00:00Z")
	})
	Attribute("group_by", String, "Optional dimension to break results down by. When omitted, a single aggregate row/series for the whole slice is returned.", func() {
		Enum(queryDimensions...)
		Example("department_name")
	})
	Attribute("filters", ArrayOf(QueryFilter), "Optional filters; all filters are ANDed together.")
	Attribute("granularity_seconds", Int64, "Optional timeseries bucket size in seconds. Defaults to an interval derived from the time range and is floored to 3600 (the source data is bucketed hourly).")
	Attribute("top_n", Int, "When group_by is set, keep at most this many groups (ranked by sort_by); the remainder are rolled into an 'Other' group. Defaults to 10.", func() {
		Default(10)
		Minimum(1)
	})
	Attribute("sort_by", String, "Measure used to rank groups for top_n. Defaults to total_cost.", func() {
		Enum(queryMeasures...)
		Default("total_cost")
	})

	Required("from", "to")
})

var QueryFilter = Type("QueryFilter", func() {
	Description("A single filter predicate on an allowlisted dimension")

	Attribute("dimension", String, "Dimension to filter on", func() {
		Enum(queryDimensions...)
	})
	Attribute("values", ArrayOf(String), "Match if the dimension equals any of these values (IN semantics; for multi-valued dimensions like role/group, matches if any element is present).", func() {
		MinLength(1)
	})

	Required("dimension", "values")
})

var QueryRiskTokensPayload = Type("QueryRiskTokensPayload", func() {
	Description("Payload for the org-scoped token-by-risk breakdown query")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-26T10:00:00Z")
	})
	Attribute("project_id", String, "Optional project to scope to; defaults to every project in the organization.", func() {
		Format(FormatUUID)
	})

	Required("from", "to")
})

var RiskTokensPoint = Type("RiskTokensPoint", func() {
	Description("One UTC day of token usage split by risk involvement")

	Attribute("bucket_time_unix_nano", String, "Bucket start time in Unix nanoseconds (string for JS precision)")
	Attribute("risky_tokens", Int64, "Tokens from sessions with at least one active risk finding created in the query window")
	Attribute("total_tokens", Int64, "All session tokens in the bucket")

	Required("bucket_time_unix_nano", "risky_tokens", "total_tokens")
})

var QueryRiskTokensResult = Type("QueryRiskTokensResult", func() {
	Description("Result of the token-by-risk breakdown query")

	Attribute("interval_seconds", Int64, "Timeseries bucket width in seconds. Always 86400 — the source aggregate is bucketed daily.")
	Attribute("points", ArrayOf(RiskTokensPoint), "Gap-filled daily buckets in ascending time order")

	Required("interval_seconds", "points")
})

var MessageTokenStatsPayload = Type("MessageTokenStatsPayload", func() {
	Description("Payload for the org-scoped message-level token stats query")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-26T10:00:00Z")
	})
	Attribute("project_id", String, "Optional project to scope to; defaults to every project in the organization.", func() {
		Format(FormatUUID)
	})

	Required("from", "to")
})

var MessageTokenStatsPoint = Type("MessageTokenStatsPoint", func() {
	Description("One UTC day of message-level token stats")

	Attribute("bucket_time_unix_nano", String, "Bucket start time in Unix nanoseconds (string for JS precision)")
	Attribute("risky_message_tokens", Int64, "Tokens in messages carrying at least one active risk finding")
	Attribute("tool_message_tokens", Int64, "Tokens in tool-call messages")

	Required("bucket_time_unix_nano", "risky_message_tokens", "tool_message_tokens")
})

var MessageTokenStatsResult = Type("MessageTokenStatsResult", func() {
	Description("Result of the message-level token stats query")

	Attribute("interval_seconds", Int64, "Timeseries bucket width in seconds. Always 86400 — the stats are bucketed daily.")
	Attribute("points", ArrayOf(MessageTokenStatsPoint), "Gap-filled daily buckets in ascending time order")

	Required("interval_seconds", "points")
})

var QueryMeasures = Type("QueryMeasures", func() {
	Description("Aggregated measure values for a group or time bucket")

	Attribute("total_cost", Float64, "Total cost in USD")
	Attribute("total_input_tokens", Int64, "Sum of input tokens")
	Attribute("total_output_tokens", Int64, "Sum of output tokens")
	Attribute("total_tokens", Int64, "Sum of all tokens")
	Attribute("cache_read_input_tokens", Int64, "Sum of cache read input tokens")
	Attribute("cache_creation_input_tokens", Int64, "Sum of cache creation input tokens")
	Attribute("total_tool_calls", Int64, "Total number of tool calls")
	Attribute("total_chats", Int64, "Number of distinct chat sessions")

	Required(
		"total_cost",
		"total_input_tokens",
		"total_output_tokens",
		"total_tokens",
		"cache_read_input_tokens",
		"cache_creation_input_tokens",
		"total_tool_calls",
		"total_chats",
	)
})

var QueryRow = Type("QueryRow", func() {
	Description("One row of the grouped table: measures aggregated over the full time range for a single group value.")

	Attribute("group_value", String, "The dimension value for this row. Empty string when no group_by was requested; 'Other' for the rolled-up remainder beyond top_n.")
	Attribute("measures", QueryMeasures, "Aggregated measures for this group")
	Attribute("dimension_values", MapOf(String, ArrayOf(String)), "Distinct values of every allowlisted dimension other than the group_by dimension, observed within this group. Keyed by dimension identifier (the same keys used for group_by/filters, e.g. when grouping by department_name: 'email' -> [...], 'job_title' -> [...], 'role' -> [...]). Empty values are omitted and each list is capped.")

	Required("group_value", "measures", "dimension_values")
})

var QueryPoint = Type("QueryPoint", func() {
	Description("A single time bucket within a series")

	Attribute("bucket_time_unix_nano", String, "Bucket start time in Unix nanoseconds (string for JS precision)")
	Attribute("measures", QueryMeasures, "Aggregated measures for this bucket")

	Required("bucket_time_unix_nano", "measures")
})

var QuerySeries = Type("QuerySeries", func() {
	Description("A gap-filled timeseries for a single group value (one line on the chart).")

	Attribute("group_value", String, "The dimension value for this series. Empty string when no group_by was requested; 'Other' for the rolled-up remainder beyond top_n.")
	Attribute("points", ArrayOf(QueryPoint), "Time buckets in ascending order, gap-filled with zeros.")

	Required("group_value", "points")
})

var QueryResult = Type("QueryResult", func() {
	Description("Result of a generic analytics query: a grouped table and a matching per-group timeseries over the same data slice.")

	Attribute("group_by", String, "Echoes the requested group_by dimension; empty when none was requested.")
	Attribute("interval_seconds", Int64, "The timeseries bucket interval in seconds.")
	Attribute("table", ArrayOf(QueryRow), "Grouped totals over the full time range, ordered by sort_by descending.")
	Attribute("timeseries", ArrayOf(QuerySeries), "One series per group value (aligned with table rows), each gap-filled.")

	Required("group_by", "interval_seconds", "table", "timeseries")
})

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
	Attribute("user_id", String, "Optional internal user ID filter")
	Attribute("external_user_id", String, "Optional external user ID filter")
	Attribute("api_key_id", String, "Optional API key ID filter")
	Attribute("toolset_slug", String, "Optional toolset/MCP server slug filter")
	Attribute("remote_mcp_server_id", String, "Optional Remote MCP server ID filter", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_id", String, "Optional MCP server ID filter (fronting server; spans both remote-backed and toolset-backed activity)", func() {
		Format(FormatUUID)
	})
	Attribute("event_source", String, "Optional event source filter (e.g. 'hook')")
	Attribute("hook_source", String, "Optional hook source filter (e.g. 'cursor', 'claude-code')")
	Attribute("account_type", String, "Optional account type filter ('team' or 'personal')")
	Attribute("external_org_id", String, "Optional filter to a single AI account by its provider org id; scopes the overview to that one account")
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
		Enum("api_key", "user", "internal_user", "agent")
	})
	Attribute("event_source", String, "Optional event source filter for the option list")

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

var ToolUsageTargetType = Type("ToolUsageTargetType", String, func() {
	Description("Tool usage target type")
	Enum("hosted_mcp_server", "tunneled_mcp_server", "shadow_mcp_server", "local_tool", "skill")
})

var ToolUsageTargetKind = Type("ToolUsageTargetKind", String, func() {
	Description("Tool usage aggregation target kind")
	Enum("server", "local_tools", "skill")
})

var ToolUsageUserKind = Type("ToolUsageUserKind", String, func() {
	Description("Tool usage user identity kind")
	Enum("email", "external_user_id", "user_id", "unknown")
})

var ToolUsageUserFilter = Type("ToolUsageUserFilter", func() {
	Description("Typed user identity filter")
	Attribute("kind", ToolUsageUserKind, "Type of user identity represented by the filter key")
	Attribute("key", String, "User identity value to include")
	Required("kind", "key")
})

var ToolUsageFilterOptionType = Type("ToolUsageFilterOptionType", String, func() {
	Description("Tool usage filter option type")
	Enum("hosted_servers", "shadow_servers", "users")
})

var GetToolUsageSummaryPayload = Type("GetToolUsageSummaryPayload", func() {
	Description("Payload for target-aware MCP and tool usage metrics")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("target_types", ArrayOf(ToolUsageTargetType), "Target types to include. Empty means all target types.")
	Attribute("hosted_toolset_slugs", ArrayOf(String), "Hosted MCP toolset slugs to include")
	Attribute("shadow_server_names", ArrayOf(String), "Shadow MCP server names to include")
	Attribute("user_filters", ArrayOf(ToolUsageUserFilter), "Typed user identities to include")
	Attribute("hook_sources", ArrayOf(String), "Hook plugin sources to include. Direct hosted MCP calls have no hook source and are excluded when this filter is set.")
	Attribute("account_type", String, "Optional account type filter ('team' or 'personal').")

	Required("from", "to")
})

var GetToolUsageSummaryResult = Type("GetToolUsageSummaryResult", func() {
	Description("Target-aware MCP and tool usage metrics")

	Attribute("totals", ToolUsageTotals, "Overall usage totals for the selected filters and time range")
	Attribute("targets", ArrayOf(ToolUsageTargetSummary), "Top usage targets for the selected filters and time range")
	Attribute("users", ArrayOf(ToolUsageUserSummary), "Top user identities for the selected filters and time range")
	Attribute("target_time_series", ArrayOf(ToolUsageTargetTimeSeriesPoint), "Time-series usage buckets grouped by target")
	Attribute("user_time_series", ArrayOf(ToolUsageUserTimeSeriesPoint), "Time-series usage buckets grouped by user identity")
	Attribute("users_by_target", ArrayOf(ToolUsageUsersByTargetRow), "Cross-dimensional usage rows grouped by target and user identity")
	Attribute("target_tool_breakdown", ArrayOf(ToolUsageTargetToolBreakdownRow), "Per-tool usage rows grouped by target")

	Required("totals", "targets", "users", "target_time_series", "user_time_series", "users_by_target", "target_tool_breakdown")
})

var ListToolUsageTracesPayload = Type("ListToolUsageTracesPayload", func() {
	Description("Payload for listing target-aware MCP and tool usage traces")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("target_types", ArrayOf(ToolUsageTargetType), "Target types to include. Empty means all target types.")
	Attribute("hosted_toolset_slugs", ArrayOf(String), "Hosted MCP toolset slugs to include")
	Attribute("shadow_server_names", ArrayOf(String), "Shadow MCP server names to include")
	Attribute("user_filters", ArrayOf(ToolUsageUserFilter), "Typed user identities to include")
	Attribute("hook_sources", ArrayOf(String), "Hook plugin sources to include. Direct hosted MCP calls have no hook source and are excluded when this filter is set.")
	Attribute("account_type", String, "Optional account type filter ('team' or 'personal'). 'team' includes unclassified traces.")
	Attribute("query", String, "Free-text attribute search string from the q URL param. Matches useful identifier attributes such as Gram URN, conversation ID, and trigger instance ID.")
	Attribute("filters", ArrayOf(LogFilter), "Arbitrary attribute filter conditions from the af URL param")
	Attribute("cursor", String, "Cursor for pagination")
	Attribute("sort", String, "Sort order", func() {
		Enum("asc", "desc")
		Default("desc")
	})
	Attribute("limit", Int, "Number of traces to return", func() {
		Minimum(1)
		Maximum(1000)
		Default(100)
	})

	Required("from", "to")
})

var ListToolUsageTracesResult = Type("ListToolUsageTracesResult", func() {
	Description("Result of listing target-aware MCP and tool usage traces")

	Attribute("traces", ArrayOf(ToolUsageTraceSummary), "Target-aware tool usage trace rows")
	Attribute("next_cursor", String, "Cursor for next page")

	Required("traces")
})

var ToolUsageTraceSummary = Type("ToolUsageTraceSummary", func() {
	Description("A single target-aware tool usage trace row")

	Attribute("id", String, "Stable row identity for React keys and expansion state")
	Attribute("trace_id", String, "Real OTel trace ID when the grouped logs have one")
	Attribute("log_group", ToolUsageTraceLogGroup, "How the frontend should fetch child logs for this row")
	Attribute("start_time_unix_nano", String, "Earliest log timestamp in Unix nanoseconds as a string for JavaScript integer safety")
	Attribute("log_count", UInt64, "Number of logs in the trace")
	Attribute("gram_urn", String, "Gram URN associated with the trace")
	Attribute("tool_name", String, "Tool name shown in the row")
	Attribute("target_type", ToolUsageTargetType, "Specific kind of tool usage target")
	Attribute("target_kind", ToolUsageTargetKind, "Display grouping for the target")
	Attribute("target_id", String, "Stable target identifier used by filters")
	Attribute("target_label", String, "User-facing target label")
	Attribute("user_key", String, "Stable user identity value")
	Attribute("user_label", String, "User-facing user identity label")
	Attribute("user_kind", ToolUsageUserKind, "Type of user identity represented by the row")
	Attribute("hook_source", String, "Hook plugin source when the row came from hook telemetry")
	Attribute("event_source", String, "Telemetry event source")
	Attribute("http_status_code", Int32, "HTTP status code when available")
	Attribute("hook_status", String, "Hook execution status when the row came from hook telemetry", func() {
		Enum("success", "failure", "blocked", "pending")
	})
	Attribute("block_reason", String, "Hook block reason when hook_status is blocked")
	Attribute("account_type", String, "AI account classification ('team' or 'personal'); empty/absent when unclassified")

	Required("id", "log_group", "start_time_unix_nano", "log_count", "gram_urn", "tool_name", "target_type", "target_kind", "target_id", "target_label", "user_key", "user_label", "user_kind", "event_source")
})

var ToolUsageTraceLogGroupKind = Type("ToolUsageTraceLogGroupKind", String, func() {
	Description("Child-log lookup strategy for a tool usage trace row")
	Enum("trace_id", "correlation_id", "trigger_event_id", "log_id")
})

var ToolUsageTraceLogGroup = Type("ToolUsageTraceLogGroup", func() {
	Description("Descriptor used by the dashboard to fetch child logs for a trace row")

	Attribute("kind", ToolUsageTraceLogGroupKind, "Lookup strategy")
	Attribute("value", String, "Lookup value")

	Required("kind", "value")
})

var GetToolUsageFilterOptionsPayload = Type("GetToolUsageFilterOptionsPayload", func() {
	Description("Payload for target-aware MCP and tool usage filter options")

	Attribute("from", String, "Start time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T10:00:00Z")
	})
	Attribute("to", String, "End time in ISO 8601 format", func() {
		Format(FormatDateTime)
		Example("2025-12-19T11:00:00Z")
	})
	Attribute("option_types", ArrayOf(ToolUsageFilterOptionType), "Filter option types to include. Empty means all option types.")

	Required("from", "to")
})

var GetToolUsageFilterOptionsResult = Type("GetToolUsageFilterOptionsResult", func() {
	Description("Filter options for target-aware MCP and tool usage metrics")

	Attribute("hosted_servers", ArrayOf(ToolUsageHostedServerFilterOption), "Hosted MCP servers with usage in the selected time range")
	Attribute("shadow_servers", ArrayOf(ToolUsageShadowServerFilterOption), "Shadow MCP servers with usage in the selected time range")
	Attribute("users", ArrayOf(ToolUsageUserFilterOption), "User identities with usage in the selected time range")

	Required("hosted_servers", "shadow_servers", "users")
})

var ToolUsageHostedServerFilterOption = Type("ToolUsageHostedServerFilterOption", func() {
	Description("Hosted MCP server filter option with usage in the selected time window")

	Attribute("toolset_slug", String, "Hosted MCP toolset slug")
	Attribute("toolset_name", String, "Hosted MCP toolset display name")
	Attribute("event_count", Int64, "Number of tool usage events observed for the hosted MCP server")

	Required("toolset_slug", "toolset_name", "event_count")
})

var ToolUsageShadowServerFilterOption = Type("ToolUsageShadowServerFilterOption", func() {
	Description("Shadow MCP server filter option with usage in the selected time window")

	Attribute("server_name", String, "Observed Shadow MCP server name")
	Attribute("event_count", Int64, "Number of tool usage events observed for the Shadow MCP server")

	Required("server_name", "event_count")
})

var ToolUsageUserFilterOption = Type("ToolUsageUserFilterOption", func() {
	Description("Tool usage user filter option with usage in the selected time window")

	Attribute("user_key", String, "Stable user identity value used by filters")
	Attribute("user_label", String, "User-facing label for the user identity")
	Attribute("user_kind", ToolUsageUserKind, "Type of user identity represented by the option")
	Attribute("event_count", Int64, "Number of tool usage events observed for the user identity")

	Required("user_key", "user_label", "user_kind", "event_count")
})

var ToolUsageTotals = Type("ToolUsageTotals", func() {
	Description("Target-aware MCP and tool usage totals")

	Attribute("event_count", Int64, "Total number of tool usage events")
	Attribute("success_count", Int64, "Number of successful tool usage events")
	Attribute("failure_count", Int64, "Number of failed tool usage events")
	Attribute("failure_rate", Float64, "Fraction of completed tool usage events that failed")
	Attribute("unique_tools", Int64, "Number of distinct tools observed")
	Attribute("unique_users", Int64, "Number of distinct user identities observed")
	Attribute("unique_targets", Int64, "Number of distinct usage targets observed")

	Required("event_count", "success_count", "failure_count", "failure_rate", "unique_tools", "unique_users", "unique_targets")
})

var ToolUsageTargetSummary = Type("ToolUsageTargetSummary", func() {
	Description("Aggregated tool usage metrics for one target")

	Attribute("target_type", ToolUsageTargetType, "Specific kind of tool usage target")
	Attribute("target_kind", ToolUsageTargetKind, "Display grouping for the target")
	Attribute("target_id", String, "Stable target identifier used by filters and chart grouping")
	Attribute("target_label", String, "User-facing label for the target")
	Attribute("event_count", Int64, "Total number of tool usage events for the target")
	Attribute("unique_tools", Int64, "Number of distinct tools observed for the target")
	Attribute("success_count", Int64, "Number of successful tool usage events for the target")
	Attribute("failure_count", Int64, "Number of failed tool usage events for the target")
	Attribute("failure_rate", Float64, "Fraction of completed tool usage events for the target that failed")

	Required("target_type", "target_kind", "target_id", "target_label", "event_count", "unique_tools", "success_count", "failure_count", "failure_rate")
})

var ToolUsageUserSummary = Type("ToolUsageUserSummary", func() {
	Description("Aggregated tool usage metrics for one user identity")

	Attribute("user_key", String, "Stable user identity value used by filters and chart grouping")
	Attribute("user_label", String, "User-facing label for the user identity")
	Attribute("user_kind", ToolUsageUserKind, "Type of user identity represented by the row")
	Attribute("event_count", Int64, "Total number of tool usage events for the user identity")
	Attribute("unique_tools", Int64, "Number of distinct tools observed for the user identity")
	Attribute("success_count", Int64, "Number of successful tool usage events for the user identity")
	Attribute("failure_count", Int64, "Number of failed tool usage events for the user identity")
	Attribute("failure_rate", Float64, "Fraction of completed tool usage events for the user identity that failed")

	Required("user_key", "user_label", "user_kind", "event_count", "unique_tools", "success_count", "failure_count", "failure_rate")
})

var ToolUsageTargetTimeSeriesPoint = Type("ToolUsageTargetTimeSeriesPoint", func() {
	Description("A time-series bucket for one tool usage target")

	Attribute("bucket_start_ns", String, "Bucket start time in Unix nanoseconds as a string for JavaScript integer safety")
	Attribute("target_type", ToolUsageTargetType, "Specific kind of tool usage target")
	Attribute("target_kind", ToolUsageTargetKind, "Display grouping for the target")
	Attribute("target_id", String, "Stable target identifier used by filters and chart grouping")
	Attribute("target_label", String, "User-facing label for the target")
	Attribute("event_count", Int64, "Number of tool usage events in the bucket")
	Attribute("failure_count", Int64, "Number of failed tool usage events in the bucket")

	Required("bucket_start_ns", "target_type", "target_kind", "target_id", "target_label", "event_count", "failure_count")
})

var ToolUsageUserTimeSeriesPoint = Type("ToolUsageUserTimeSeriesPoint", func() {
	Description("A time-series bucket for one tool usage user identity")

	Attribute("bucket_start_ns", String, "Bucket start time in Unix nanoseconds as a string for JavaScript integer safety")
	Attribute("user_key", String, "Stable user identity value used by filters and chart grouping")
	Attribute("user_label", String, "User-facing label for the user identity")
	Attribute("user_kind", ToolUsageUserKind, "Type of user identity represented by the row")
	Attribute("event_count", Int64, "Number of tool usage events in the bucket")
	Attribute("failure_count", Int64, "Number of failed tool usage events in the bucket")

	Required("bucket_start_ns", "user_key", "user_label", "user_kind", "event_count", "failure_count")
})

var ToolUsageUsersByTargetRow = Type("ToolUsageUsersByTargetRow", func() {
	Description("Aggregated tool usage metrics for one target and user identity")

	Attribute("target_type", ToolUsageTargetType, "Specific kind of tool usage target")
	Attribute("target_kind", ToolUsageTargetKind, "Display grouping for the target")
	Attribute("target_id", String, "Stable target identifier used by filters and chart grouping")
	Attribute("target_label", String, "User-facing label for the target")
	Attribute("user_key", String, "Stable user identity value used by filters and chart grouping")
	Attribute("user_label", String, "User-facing label for the user identity")
	Attribute("user_kind", ToolUsageUserKind, "Type of user identity represented by the row")
	Attribute("event_count", Int64, "Total number of tool usage events for the target and user identity")
	Attribute("failure_count", Int64, "Number of failed tool usage events for the target and user identity")

	Required("target_type", "target_kind", "target_id", "target_label", "user_key", "user_label", "user_kind", "event_count", "failure_count")
})

var ToolUsageTargetToolBreakdownRow = Type("ToolUsageTargetToolBreakdownRow", func() {
	Description("Aggregated tool usage metrics for one target and tool")

	Attribute("target_type", ToolUsageTargetType, "Specific kind of tool usage target")
	Attribute("target_kind", ToolUsageTargetKind, "Display grouping for the target")
	Attribute("target_id", String, "Stable target identifier used by filters and chart grouping")
	Attribute("target_label", String, "User-facing label for the target")
	Attribute("tool_name", String, "Observed tool name")
	Attribute("event_count", Int64, "Total number of tool usage events for the target and tool")
	Attribute("success_count", Int64, "Number of successful tool usage events for the target and tool")
	Attribute("failure_count", Int64, "Number of failed tool usage events for the target and tool")
	Attribute("failure_rate", Float64, "Fraction of completed tool usage events for the target and tool that failed")

	Required("target_type", "target_kind", "target_id", "target_label", "tool_name", "event_count", "success_count", "failure_count", "failure_rate")
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

	Required(
		"trace_id",
		"start_time_unix_nano",
		"log_count",
		"gram_urn",
	)
})
