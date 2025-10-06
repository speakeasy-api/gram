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
		Security(security.ByKey)
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()

			Attribute("project_id", String, "Project ID", func() {
				Format(FormatUUID)
			})
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
				Format(FormatDateTime)
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

			Required("tool_id", "project_id")
		})

		Result(ListToolLogResult)

		HTTP(func() {
			GET("/rpc/logs.list")

			Response(StatusOK)

			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()

			Param("project_id")
			Param("tool_id")
			Param("ts_start")
			Param("ts_end")
			Param("cursor")
			Param("per_page")
			Param("direction")
			Param("sort")
		})

		Meta("openapi:operationId", "listToolLogs")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListToolLogs"}`)
	})

})

var ListToolLogResult = Type("ListToolLogResult", func() {
	Attribute("logs", ArrayOf(HTTPToolLog))
	Required("logs")
})

var PaginationParams = Type("PaginationParams", func() {
	Description("Pagination parameters for listing logs")

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
		Enum("ASC", "DESC")
		Default("DESC")
	})
	Attribute("cursor", String, "Cursor for pagination", func() {
		Format(FormatDateTime)
	})
})

var ToolType = Type("ToolType", String, func() {
	Enum("http", "function", "prompt")
	Description("Type of tool being logged")
})

var HTTPToolLog = Type("HTTPToolLog", func() {
	Description("HTTP tool request and response log entry")

	// Timestamp
	Attribute("ts", String, "Timestamp of the request", func() {
		Format(FormatDateTime)
	})

	// Required multi-tenant keys
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
	Attribute("http_route", String, "HTTP route")
	Attribute("status_code", UInt32, "HTTP status code")
	Attribute("duration_ms", Float64, "Duration in milliseconds")
	Attribute("user_agent", String, "User agent")
	Attribute("client_ipv4", String, "Client IPv4 address")

	// Request payload
	Attribute("request_headers", MapOf(String, String), "Request headers")
	Attribute("request_body", String, "Request body")
	Attribute("request_body_skip", String, "Reason for skipping request body")
	Attribute("request_body_bytes", UInt64, "Request body size in bytes")

	// Response payload
	Attribute("response_headers", MapOf(String, String), "Response headers")
	Attribute("response_body", String, "Response body")
	Attribute("response_body_skip", String, "Reason for skipping response body")
	Attribute("response_body_bytes", UInt64, "Response body size in bytes")

	Required(
		"ts",
		"organization_id",
		"project_id",
		"deployment_id",
		"tool_id",
		"tool_urn",
		"tool_type",
		"trace_id",
		"span_id",
		"http_method",
		"http_route",
		"status_code",
		"duration_ms",
		"user_agent",
		"client_ipv4",
	)
})
