package toolmetrics

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("logs", func() {
	Description("Call logs for a toolset.")

	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listLogs", func() {
		Description("List call logs for a toolset.")
		Security(security.ByKey, security.ProjectSlug)
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(ListToolLogsRequest)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListToolLogResponse)

		HTTP(func() {
			GET("/rpc/logs.list")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()

			Param("tool_id")
			Param("ts_start")
			Param("ts_end")
			Param("cursor")
			Param("status")
			Param("server_name")
			Param("tool_name")
			Param("tool_type")
			Param("tool_urns")
			Param("per_page")
			Param("direction")
			Param("sort")
		})

		Meta("openapi:operationId", "listToolLogs")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListToolLogs"}`)
	})

	Method("listToolExecutionLogs", func() {
		Description("List structured logs from tool executions.")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})
		Security(security.ByKey, security.ProjectSlug)
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Extend(ListToolExecutionLogsPayload)
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListToolExecutionLogsResult)

		HTTP(func() {
			GET("/rpc/logs.listToolExecutionLogs")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()

			Param("ts_start")
			Param("ts_end")
			Param("deployment_id")
			Param("function_id")
			Param("instance")
			Param("level")
			Param("source")
			Param("cursor")
			Param("per_page")
			Param("direction")
			Param("sort")
		})

		Meta("openapi:operationId", "listToolExecutionLogs")
		Meta("openapi:extension:x-speakeasy-name-override", "listToolExecutionLogs")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ToolExecutionLogs"}`)
	})

})

var ListToolLogsRequest = Type("ListToolLogsRequest", func() {
	Description("Payload for listing tool logs")

	Attribute("tool_id", String, "Tool ID", func() {
		Format(FormatUUID)
	})
	Attribute("ts_start", String, "Start timestamp", func() {
		Format(FormatDateTime)
	})
	Attribute("ts_end", String, "End timestamp", func() {
		Format(FormatDateTime)
	})
	Attribute("cursor", String, "Cursor for pagination", func() {
		Format(FormatUUID)
	})
	Attribute("per_page", Int, "Number of items per page (1-100)", func() {
		Minimum(1)
		Maximum(100)
		Default(20)
	})
	Attribute("direction", String, "Pagination direction", func() {
		Enum("next", "prev")
		Default("next")
	})
	Attribute("sort", String, "Sort order", func() {
		Enum("asc", "desc")
		Default("desc")
	})
	Attribute("status", String, "Status filter", func() {
		Enum("success", "failure")
	})
	Attribute("server_name", String, "Server name filter")
	Attribute("tool_name", String, "Tool name filter")
	Attribute("tool_type", String, "Tool type filter", func() {
		Enum("http", "function", "prompt")
	})
	Attribute("tool_urns", ArrayOf(String), "Tool URNs filter")
})

var ListToolLogResponse = Type("ListToolLogResponse", func() {
	Attribute("logs", ArrayOf(HTTPToolLog))
	Attribute("pagination", PaginationResponse)
	Attribute("enabled", Boolean, "Whether tool metrics are enabled for the organization")
	Required("logs", "pagination", "enabled")
})

var PaginationResponse = Type("PaginationResponse", func() {
	Description("Pagination metadata for list responses")

	Attribute("per_page", Int, "Number of items per page")
	Attribute("has_next_page", Boolean, "Whether there is a next page")
	Attribute("next_page_cursor", String, "Cursor for next page", func() {
		Format(FormatUUID)
	})
})

var ToolType = Type("ToolType", String, func() {
	Enum("http", "function", "prompt")
	Description("Type of tool being logged")
})

var HTTPToolLog = Type("HTTPToolLog", func() {
	Description("HTTP tool request and response log entry")

	// Timestamp
	Attribute("id", String, "Id of the request", func() {
		Format(FormatUUID)
	})

	// Timestamp
	Attribute("ts", String, "Timestamp of the request", func() {
		Format(FormatDateTime)
	})

	// Multi-tenant keys
	Attribute("organization_id", String, "Organization UUID", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "Project UUID", func() {
		Format(FormatUUID)
	})
	Attribute("deployment_id", String, "Deployment UUID", func() {
		Format(FormatUUID)
	})
	Attribute("tool_id", String, "Tool UUID", func() {
		Format(FormatUUID)
	})
	Attribute("tool_urn", String, "Tool URN")
	Attribute("tool_type", ToolType, "Tool type")

	// Correlation
	Attribute("trace_id", String, "Trace ID for correlation")
	Attribute("span_id", String, "Span ID for correlation")

	// Request metadata
	Attribute("http_method", String, "HTTP method")
	Attribute("http_server_url", String, "HTTP Server URL")
	Attribute("http_route", String, "HTTP route")
	Attribute("status_code", Int64, "HTTP status code")
	Attribute("duration_ms", Float64, "Duration in milliseconds")
	Attribute("user_agent", String, "User agent")

	// Request payload
	Attribute("request_headers", MapOf(String, String), "Request headers")
	Attribute("request_body_bytes", Int64, "Request body size in bytes")

	// Response payload
	Attribute("response_headers", MapOf(String, String), "Response headers")
	Attribute("response_body_bytes", Int64, "Response body size in bytes")

	Required(
		"ts",
		"organization_id",
		"deployment_id",
		"tool_id",
		"tool_urn",
		"tool_type",
		"trace_id",
		"span_id",
		"http_server_url",
		"http_method",
		"http_route",
		"status_code",
		"duration_ms",
		"user_agent",
	)
})

var ListToolExecutionLogsPayload = Type("ListToolExecutionLogsPayload", func() {
	Description("Payload for listing tool execution logs")

	Attribute("ts_start", String, "Start timestamp", func() {
		Format(FormatDateTime)
	})
	Attribute("ts_end", String, "End timestamp", func() {
		Format(FormatDateTime)
	})
	Attribute("deployment_id", String, "Deployment ID filter", func() {
		Format(FormatUUID)
	})
	Attribute("function_id", String, "Function ID filter", func() {
		Format(FormatUUID)
	})
	Attribute("instance", String, "Instance filter")
	Attribute("level", String, "Log level filter", func() {
		Enum("debug", "info", "warn", "error")
	})
	Attribute("source", String, "Log source filter", func() {
		Enum("stdout", "stderr")
	})
	Attribute("cursor", String, "Cursor for pagination", func() {
		Format(FormatUUID)
	})
	Attribute("per_page", Int, "Number of items per page (1-100)", func() {
		Minimum(1)
		Maximum(100)
		Default(20)
	})
	Attribute("direction", String, "Pagination direction", func() {
		Enum("next", "prev")
		Default("next")
	})
	Attribute("sort", String, "Sort order", func() {
		Enum("asc", "desc")
		Default("desc")
	})
})

var ListToolExecutionLogsResult = Type("ListToolExecutionLogsResult", func() {
	Description("Result of listing tool execution logs")

	Attribute("logs", ArrayOf(ToolExecutionLog), "List of tool execution logs")
	Attribute("pagination", PaginationResponse, "Pagination metadata")
})

var ToolExecutionLog = Type("ToolExecutionLog", func() {
	Description("Structured log entry from a tool execution")

	Attribute("id", String, "Log entry ID", func() {
		Format(FormatUUID)
	})
	Attribute("timestamp", String, "Timestamp of the log entry", func() {
		Format(FormatDateTime)
	})
	Attribute("instance", String, "Instance identifier")
	Attribute("level", String, "Log level")
	Attribute("source", String, "Log source")
	Attribute("raw_log", String, "Raw log message")
	Attribute("message", String, "Parsed log message")
	Attribute("attributes", String, "JSON-encoded log attributes")
	Attribute("project_id", String, "Project UUID", func() {
		Format(FormatUUID)
	})
	Attribute("deployment_id", String, "Deployment UUID", func() {
		Format(FormatUUID)
	})
	Attribute("function_id", String, "Function UUID", func() {
		Format(FormatUUID)
	})

	Required(
		"id",
		"timestamp",
		"instance",
		"level",
		"source",
		"raw_log",
		"project_id",
		"deployment_id",
		"function_id",
	)
})
