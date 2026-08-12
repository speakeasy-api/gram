package unproxiedmcp

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("unproxiedMcp", func() {
	Description("Managing unproxied MCP servers. These are vendor MCP servers that Speakeasy lists and can attach to a plugin but never proxies, so there is no OAuth callback or upstream allowlisting involved.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createServer", func() {
		Description("Create a new unproxied MCP server. Restricted to callers whose email is on the speakeasy.com or speakeasyapi.dev domain.")

		Payload(func() {
			Extend(CreateServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UnproxiedMcpServer)

		HTTP(func() {
			POST("/rpc/unproxiedMcp.createServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createUnproxiedMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "createServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateUnproxiedMcpServer"}`)
	})

	Method("listServers", func() {
		Description("List all unproxied MCP servers for a project")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListServersResult)

		HTTP(func() {
			GET("/rpc/unproxiedMcp.listServers")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listUnproxiedMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "listServers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UnproxiedMcpServers"}`)
	})

	Method("getServer", func() {
		Description("Get an unproxied MCP server by ID or slug. Exactly one of id or slug must be provided.")

		Payload(func() {
			Attribute("id", String, "The ID of the unproxied MCP server. Mutually exclusive with slug.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "The slug of the unproxied MCP server. Mutually exclusive with id.")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UnproxiedMcpServer)

		HTTP(func() {
			GET("/rpc/unproxiedMcp.getServer")
			Param("id")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getUnproxiedMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "getServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetUnproxiedMcpServer"}`)
	})

	Method("listTools", func() {
		Description("Best-effort discovery of the tools available on the vendor's MCP server. Connects live to the server's URL and issues an MCP tools/list call; the result is never cached and the connection is never reused for actual tool execution.")

		Payload(func() {
			Attribute("id", String, "The ID of the unproxied MCP server", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListToolsResult)

		HTTP(func() {
			GET("/rpc/unproxiedMcp.listTools")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listUnproxiedMcpServerTools")
		Meta("openapi:extension:x-speakeasy-name-override", "listTools")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UnproxiedMcpServerTools"}`)
	})

	Method("deleteServer", func() {
		Description("Delete an unproxied MCP server")

		Payload(func() {
			Attribute("id", String, "The ID of the unproxied MCP server to delete")
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/unproxiedMcp.deleteServer")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteUnproxiedMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteUnproxiedMcpServer"}`)
	})
})

var CreateServerForm = Type("CreateUnproxiedMcpServerForm", func() {
	Description("Form for creating a new unproxied MCP server")

	Attribute("name", String, "Optional human-readable name for the unproxied MCP server. Empty values are stored as null.")
	Attribute("url", String, "The URL of the vendor's MCP server. Speakeasy never proxies tool calls through it; the only outbound requests Speakeasy ever makes to it are fetching a favicon and, on request, a live tool listing.", func() {
		Format(FormatURI)
	})
	Attribute("description", String, "Optional description shown alongside the server.")

	Required("url")
})

var UnproxiedMcpServer = Type("UnproxiedMcpServer", func() {
	Meta("struct:pkg:path", "types")

	Description("An unproxied MCP server configuration")

	Attribute("id", String, "The ID of the unproxied MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID this unproxied MCP server belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Optional human-readable name for the unproxied MCP server")
	Attribute("slug", String, "URL-friendly slug derived from the URL and ID.")
	Attribute("url", String, "The URL of the vendor's MCP server", func() {
		Format(FormatURI)
	})
	Attribute("description", String, "Optional description shown alongside the server.")
	Attribute("created_at", String, func() {
		Description("When the unproxied MCP server was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the unproxied MCP server was last updated")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "url", "created_at", "updated_at")
})

var ListServersResult = Type("ListUnproxiedMcpServersResult", func() {
	Description("Result type for listing unproxied MCP servers")

	Attribute("unproxied_mcp_servers", ArrayOf(UnproxiedMcpServer))
	Required("unproxied_mcp_servers")
})

var UnproxiedMcpServerTool = Type("UnproxiedMcpServerTool", func() {
	Description("A tool discovered on the vendor's MCP server")

	Attribute("name", String, "Tool name")
	Attribute("description", String, "Tool description")

	Required("name")
})

var ListToolsResult = Type("ListUnproxiedMcpServerToolsResult", func() {
	Description("Result of a live tool-discovery probe against an unproxied MCP server")

	Attribute("status", String, "Outcome of the discovery attempt", func() {
		Enum("success", "auth_required", "unreachable", "error")
	})
	Attribute("tools", ArrayOf(UnproxiedMcpServerTool))
	Attribute("message", String, "Human-readable detail, present for non-success statuses")

	Required("status", "tools")
})
