package mcpfrontends

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("mcpFrontends", func() {
	Description("Managing MCP frontends, which configure authentication, environment, and backend selection for an MCP server.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createMcpFrontend", func() {
		Description("Create a new MCP frontend")

		Payload(func() {
			Extend(CreateMcpFrontendForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpFrontend)

		HTTP(func() {
			POST("/rpc/mcpFrontends.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createMcpFrontend")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateMcpFrontend"}`)
	})

	Method("getMcpFrontend", func() {
		Description("Get an MCP frontend by ID")

		Payload(func() {
			Attribute("id", String, "The ID of the MCP frontend", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpFrontend)

		HTTP(func() {
			GET("/rpc/mcpFrontends.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getMcpFrontend")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetMcpFrontend"}`)
	})

	Method("listMcpFrontends", func() {
		Description("List all MCP frontends for a project")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListMcpFrontendsResult)

		HTTP(func() {
			GET("/rpc/mcpFrontends.list")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMcpFrontends")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpFrontends"}`)
	})

	Method("updateMcpFrontend", func() {
		Description("Update an MCP frontend. This is a full-record replace: fields omitted from the request become null on the stored record. The id and visibility fields are required; exactly one of remote_mcp_server_id or toolset_id must be provided; at most one of external_oauth_server_id or oauth_proxy_server_id may be provided.")

		Payload(func() {
			Extend(UpdateMcpFrontendForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpFrontend)

		HTTP(func() {
			POST("/rpc/mcpFrontends.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateMcpFrontend")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateMcpFrontend"}`)
	})

	Method("deleteMcpFrontend", func() {
		Description("Delete an MCP frontend")

		Payload(func() {
			Attribute("id", String, "The ID of the MCP frontend to delete", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/mcpFrontends.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteMcpFrontend")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteMcpFrontend"}`)
	})
})

var McpFrontendVisibility = Type("McpFrontendVisibility", String, func() {
	Description("The visibility of an MCP frontend")
	Enum("disabled", "private", "public")
	Meta("struct:pkg:path", "types")
})

var CreateMcpFrontendForm = Type("CreateMcpFrontendForm", func() {
	Description("Form for creating a new MCP frontend. Exactly one of remote_mcp_server_id or toolset_id must be provided. At most one of external_oauth_server_id or oauth_proxy_server_id may be provided.")

	Attribute("environment_id", String, "The ID of the environment to associate with the frontend", func() {
		Format(FormatUUID)
	})
	Attribute("external_oauth_server_id", String, "The ID of the external OAuth server to associate with the frontend", func() {
		Format(FormatUUID)
	})
	Attribute("oauth_proxy_server_id", String, "The ID of the OAuth proxy server to associate with the frontend", func() {
		Format(FormatUUID)
	})
	Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, "The ID of the toolset to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", McpFrontendVisibility, "The visibility of the frontend")

	Required("visibility")
})

var UpdateMcpFrontendForm = Type("UpdateMcpFrontendForm", func() {
	Description("Form for updating an MCP frontend. This is a full-record replace: fields omitted from the request become null on the stored record. Exactly one of remote_mcp_server_id or toolset_id must be provided; at most one of external_oauth_server_id or oauth_proxy_server_id may be provided.")

	Attribute("id", String, "The ID of the MCP frontend to update", func() {
		Format(FormatUUID)
	})
	Attribute("environment_id", String, "The ID of the environment to associate with the frontend", func() {
		Format(FormatUUID)
	})
	Attribute("external_oauth_server_id", String, "The ID of the external OAuth server to associate with the frontend", func() {
		Format(FormatUUID)
	})
	Attribute("oauth_proxy_server_id", String, "The ID of the OAuth proxy server to associate with the frontend", func() {
		Format(FormatUUID)
	})
	Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, "The ID of the toolset to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", McpFrontendVisibility, "The visibility of the frontend")

	Required("id", "visibility")
})

var McpFrontend = Type("McpFrontend", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP frontend configuration: authentication, environment, and backend selection for an MCP server.")

	Attribute("id", String, "The ID of the MCP frontend", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID this MCP frontend belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("environment_id", String, "The ID of the environment associated with the frontend", func() {
		Format(FormatUUID)
	})
	Attribute("external_oauth_server_id", String, "The ID of the external OAuth server associated with the frontend", func() {
		Format(FormatUUID)
	})
	Attribute("oauth_proxy_server_id", String, "The ID of the OAuth proxy server associated with the frontend", func() {
		Format(FormatUUID)
	})
	Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server used as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, "The ID of the toolset used as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", McpFrontendVisibility, "The visibility of the frontend")
	Attribute("created_at", String, func() {
		Description("When the MCP frontend was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the MCP frontend was last updated")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "visibility", "created_at", "updated_at")
})

var ListMcpFrontendsResult = Type("ListMcpFrontendsResult", func() {
	Description("Result type for listing MCP frontends")

	Attribute("mcp_frontends", ArrayOf(McpFrontend))
	Required("mcp_frontends")
})
