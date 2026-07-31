package passthroughmcp

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("passthroughMcp", func() {
	Description("Managing pass-through (unproxied) MCP servers. These are vendor MCP servers that Gram lists and can attach to a plugin but never proxies, so there is no OAuth callback or upstream allowlisting involved. Restricted to Speakeasy staff.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createServer", func() {
		Description("Create a new pass-through MCP server. Restricted to callers whose email is on the speakeasy.com or speakeasyapi.dev domain.")

		Payload(func() {
			Extend(CreateServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(PassthroughMcpServer)

		HTTP(func() {
			POST("/rpc/passthroughMcp.createServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createPassthroughMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "createServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreatePassthroughMcpServer"}`)
	})

	Method("listServers", func() {
		Description("List all pass-through MCP servers for a project")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListServersResult)

		HTTP(func() {
			GET("/rpc/passthroughMcp.listServers")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listPassthroughMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "listServers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PassthroughMcpServers"}`)
	})

	Method("getServer", func() {
		Description("Get a pass-through MCP server by ID or slug. Exactly one of id or slug must be provided.")

		Payload(func() {
			Attribute("id", String, "The ID of the pass-through MCP server. Mutually exclusive with slug.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "The slug of the pass-through MCP server. Mutually exclusive with id.")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(PassthroughMcpServer)

		HTTP(func() {
			GET("/rpc/passthroughMcp.getServer")
			Param("id")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getPassthroughMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "getServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetPassthroughMcpServer"}`)
	})

	Method("deleteServer", func() {
		Description("Delete a pass-through MCP server")

		Payload(func() {
			Attribute("id", String, "The ID of the pass-through MCP server to delete")
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/passthroughMcp.deleteServer")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deletePassthroughMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeletePassthroughMcpServer"}`)
	})
})

var CreateServerForm = Type("CreatePassthroughMcpServerForm", func() {
	Description("Form for creating a new pass-through MCP server")

	Attribute("name", String, "Optional human-readable name for the pass-through MCP server. Empty values are stored as null.")
	Attribute("url", String, "The URL of the vendor's MCP server. Displayed to admins and customers only; Gram never fetches it.", func() {
		Format(FormatURI)
	})
	Attribute("description", String, "Optional description shown alongside the server.")

	Required("url")
})

var PassthroughMcpServer = Type("PassthroughMcpServer", func() {
	Meta("struct:pkg:path", "types")

	Description("A pass-through (unproxied) MCP server configuration")

	Attribute("id", String, "The ID of the pass-through MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID this pass-through MCP server belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Optional human-readable name for the pass-through MCP server")
	Attribute("slug", String, "URL-friendly slug derived from the URL and ID.")
	Attribute("url", String, "The URL of the vendor's MCP server", func() {
		Format(FormatURI)
	})
	Attribute("description", String, "Optional description shown alongside the server.")
	Attribute("created_at", String, func() {
		Description("When the pass-through MCP server was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the pass-through MCP server was last updated")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "url", "created_at", "updated_at")
})

var ListServersResult = Type("ListPassthroughMcpServersResult", func() {
	Description("Result type for listing pass-through MCP servers")

	Attribute("passthrough_mcp_servers", ArrayOf(PassthroughMcpServer))
	Required("passthrough_mcp_servers")
})
