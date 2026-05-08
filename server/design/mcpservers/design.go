package mcpservers

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("mcpServers", func() {
	Description("Managing MCP servers, which configure authentication, environment, and backend selection for an MCP server.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createMcpServer", func() {
		Description("Create a new MCP server")

		Payload(func() {
			Extend(CreateMcpServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpServer)

		HTTP(func() {
			POST("/rpc/mcpServers.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateMcpServer"}`)
	})

	Method("getMcpServer", func() {
		Description("Get an MCP server by ID")

		Payload(func() {
			Attribute("id", String, "The ID of the MCP server", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpServer)

		HTTP(func() {
			GET("/rpc/mcpServers.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetMcpServer"}`)
	})

	Method("listMcpServers", func() {
		Description("List all MCP servers for a project")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListMcpServersResult)

		HTTP(func() {
			GET("/rpc/mcpServers.list")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpServers"}`)
	})

	Method("updateMcpServer", func() {
		Description("Update an MCP server. This is a full-record replace: fields omitted from the request become null on the stored record. The id and visibility fields are required; exactly one of remote_mcp_server_id or toolset_id must be provided; at most one of external_oauth_server_id or oauth_proxy_server_id may be provided.")

		Payload(func() {
			Extend(UpdateMcpServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpServer)

		HTTP(func() {
			POST("/rpc/mcpServers.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateMcpServer"}`)
	})

	Method("deleteMcpServer", func() {
		Description("Delete an MCP server")

		Payload(func() {
			Attribute("id", String, "The ID of the MCP server to delete", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/mcpServers.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteMcpServer"}`)
	})
})

var McpServerVisibility = Type("McpServerVisibility", String, func() {
	Description("The visibility of an MCP server")
	Enum("disabled", "private", "public")
	Meta("struct:pkg:path", "types")
})

var CreateMcpServerForm = Type("CreateMcpServerForm", func() {
	Description("Form for creating a new MCP server. Exactly one of remote_mcp_server_id or toolset_id must be provided. At most one of external_oauth_server_id or oauth_proxy_server_id may be provided.")

	Attribute("environment_id", String, "The ID of the environment to associate with the server", func() {
		Format(FormatUUID)
	})
	Attribute("external_oauth_server_id", String, "The ID of the external OAuth server to associate with the server", func() {
		Format(FormatUUID)
	})
	Attribute("oauth_proxy_server_id", String, "The ID of the OAuth proxy server to associate with the server", func() {
		Format(FormatUUID)
	})
	Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, "The ID of the toolset to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", McpServerVisibility, "The visibility of the server")

	Required("visibility")
})

var UpdateMcpServerForm = Type("UpdateMcpServerForm", func() {
	Description("Form for updating an MCP server. This is a full-record replace: fields omitted from the request become null on the stored record. Exactly one of remote_mcp_server_id or toolset_id must be provided; at most one of external_oauth_server_id or oauth_proxy_server_id may be provided.")

	Attribute("id", String, "The ID of the MCP server to update", func() {
		Format(FormatUUID)
	})
	Attribute("environment_id", String, "The ID of the environment to associate with the server", func() {
		Format(FormatUUID)
	})
	Attribute("external_oauth_server_id", String, "The ID of the external OAuth server to associate with the server", func() {
		Format(FormatUUID)
	})
	Attribute("oauth_proxy_server_id", String, "The ID of the OAuth proxy server to associate with the server", func() {
		Format(FormatUUID)
	})
	Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, "The ID of the toolset to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", McpServerVisibility, "The visibility of the server")

	Required("id", "visibility")
})

var McpServer = Type("McpServer", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP server configuration: authentication, environment, and backend selection for an MCP server.")

	Attribute("id", String, "The ID of the MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID this MCP server belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("environment_id", String, "The ID of the environment associated with the server", func() {
		Format(FormatUUID)
	})
	Attribute("external_oauth_server_id", String, "The ID of the external OAuth server associated with the server", func() {
		Format(FormatUUID)
	})
	Attribute("oauth_proxy_server_id", String, "The ID of the OAuth proxy server associated with the server", func() {
		Format(FormatUUID)
	})
	Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server used as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, "The ID of the toolset used as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", McpServerVisibility, "The visibility of the server")
	Attribute("created_at", String, func() {
		Description("When the MCP server was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the MCP server was last updated")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "visibility", "created_at", "updated_at")
})

var ListMcpServersResult = Type("ListMcpServersResult", func() {
	Description("Result type for listing MCP servers")

	Attribute("mcp_servers", ArrayOf(McpServer))
	Required("mcp_servers")
})
