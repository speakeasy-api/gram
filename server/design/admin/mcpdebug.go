package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminMcpDebugStepID = Type("AdminMcpDebugStepId", String, func() {
	Enum("reach_server", "parse_auth_challenge", "fetch_protected_resource_metadata", "fetch_authorization_server_metadata", "detect_auth_method", "initialize", "list_capabilities")
})

var AdminMcpDebugStepStatus = Type("AdminMcpDebugStepStatus", String, func() {
	Enum("passed", "warning", "failed", "skipped")
})

var AdminMcpDebugOutcome = Type("AdminMcpDebugOutcome", String, func() {
	Enum("mcp_available", "authentication_required", "invalid_mcp_response", "unreachable")
})

var AdminMcpDebugAuthMethod = Type("AdminMcpDebugAuthMethod", String, func() {
	Enum("none", "client_id_metadata_document", "dynamic_client_registration", "pre_registered", "non_oauth", "unknown")
})

var AdminMcpDebugStep = Type("AdminMcpDebugStep", func() {
	Description("One step of an MCP debug trace.")

	Attribute("id", AdminMcpDebugStepID, "Stable step identifier.")
	Attribute("title", String, "Human-readable step title.")
	Attribute("status", AdminMcpDebugStepStatus, "Step outcome.")
	Attribute("summary", String, "One-line explanation of what the step found.")
	Attribute("duration_ms", Int64, "Wall-clock time the step took. Absent for steps that did no network work.")
	Attribute("request_url", String, "The URL the step requested, without credentials, query or fragment.")
	Attribute("http_status", Int, "HTTP status the step received, when it made an HTTP request.")
	Attribute("details", MapOf(String, Any), "Structured data the step observed, such as parsed challenge parameters or metadata documents.")

	Required("id", "title", "status", "summary")
})

var AdminMcpDebugTool = Type("AdminMcpDebugTool", func() {
	Description("A tool advertised by the debugged server.")

	Attribute("name", String, "Tool name.")
	Attribute("title", String, "Display title, when advertised.")
	Attribute("description", String, "Tool description.")
	Attribute("input_schema", Any, "JSON Schema for the tool's arguments.")
	Attribute("annotations", Any, "Tool annotation hints as advertised.")

	Required("name")
})

var AdminMcpDebugPrompt = Type("AdminMcpDebugPrompt", func() {
	Description("A prompt advertised by the debugged server.")

	Attribute("name", String, "Prompt name.")
	Attribute("title", String, "Display title, when advertised.")
	Attribute("description", String, "Prompt description.")
	Attribute("arguments", Any, "Prompt arguments as advertised.")

	Required("name")
})

var AdminMcpDebugResource = Type("AdminMcpDebugResource", func() {
	Description("A resource advertised by the debugged server.")

	Attribute("uri", String, "Resource URI.")
	Attribute("name", String, "Resource name.")
	Attribute("title", String, "Display title, when advertised.")
	Attribute("description", String, "Resource description.")
	Attribute("mime_type", String, "Resource MIME type.")

	Required("uri", "name")
})

var AdminMcpDebugReport = Type("AdminMcpDebugReport", func() {
	Description("Result of an MCP debug trace.")

	Attribute("url", String, "The URL that was debugged, without credentials, query or fragment.")
	Attribute("outcome", AdminMcpDebugOutcome, "Overall outcome.")
	Attribute("duration_ms", Int64, "Total wall-clock time of the trace.")
	Attribute("steps", ArrayOf(AdminMcpDebugStep), "Trace steps in execution order.")
	Attribute("auth_method", AdminMcpDebugAuthMethod, "How a client would obtain credentials.")
	Attribute("protocol_version", String, "MCP protocol version negotiated at initialize.")
	Attribute("server_name", String, "Server name from initialize.")
	Attribute("server_version", String, "Server version from initialize.")
	Attribute("instructions", String, "Server instructions from initialize.")
	Attribute("tools", ArrayOf(AdminMcpDebugTool), "Tools advertised by the server. Present only when the server was listed without sign-in.")
	Attribute("prompts", ArrayOf(AdminMcpDebugPrompt), "Prompts advertised by the server. Present only when listed.")
	Attribute("resources", ArrayOf(AdminMcpDebugResource), "Resources advertised by the server. Present only when listed.")

	Required("url", "outcome", "duration_ms", "steps", "auth_method")
})

func mcpDebugMethods() {
	Method("debugMcpUrl", func() {
		Description("Staff-only MCP debugger: trace a remote MCP server URL step by step from Speakeasy's network. Reaches the server, reads its authentication challenge, discovers OAuth protected resource and authorization server metadata, detects how a client would register, and, when no sign-in is required, initializes a session and lists its tools, prompts and resources. Sends no credentials and stores nothing.")

		Payload(func() {
			security.AdminAuthPayload()
			Attribute("url", String, "The remote MCP server URL to debug.", func() {
				Format(FormatURI)
			})
			Required("url")
			Meta("openapi:typename", "AdminDebugMcpUrlRequestBody")
		})

		Result(AdminMcpDebugReport)

		HTTP(func() {
			POST("/admin/mcp.debug")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminDebugMcpUrl")
		Meta("openapi:extension:x-speakeasy-name-override", "debugMcpUrl")
	})

	Method("getMcpVerificationLink", func() {
		Description("Staff-only: the customer dashboard link for verifying a remote MCP server in Speakeasy's own project. When that project already has a server for the URL, the link opens its Playground; otherwise it opens the add-server page with the URL filled in. Reads only; nothing is created until staff confirm in the dashboard.")

		Payload(func() {
			security.AdminAuthPayload()
			Attribute("url", String, "The remote MCP server URL to verify.", func() {
				Format(FormatURI)
			})
			Attribute("name", String, "Optional display name to fill in when the server is added.", func() {
				MaxLength(200)
			})
			Required("url")
			Meta("openapi:typename", "AdminGetMcpVerificationLinkRequestBody")
		})

		Result(AdminMcpVerificationLink)

		HTTP(func() {
			POST("/admin/mcp.verificationLink")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetMcpVerificationLink")
		Meta("openapi:extension:x-speakeasy-name-override", "getMcpVerificationLink")
	})
}

var AdminMcpVerificationLink = Type("AdminMcpVerificationLink", func() {
	Description("Where staff verify a remote MCP server in the customer dashboard.")
	Attribute("url", String, "Customer dashboard link to open in a new tab.")
	Attribute("existing", Boolean, "True when Speakeasy's project already has a server for this URL and the link opens its Playground.")
	Attribute("project_name", String, "Name of the Speakeasy project the server is verified in.")
	Required("url", "existing", "project_name")
})
