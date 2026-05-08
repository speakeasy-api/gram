package mcpendpoints

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

var _ = Service("mcpEndpoints", func() {
	Description("Managing MCP endpoints, the url-friendly slug identifiers that address MCP servers.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createMcpEndpoint", func() {
		Description("Create a new MCP endpoint for an MCP server")

		Payload(func() {
			Extend(CreateMcpEndpointForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpEndpoint)

		HTTP(func() {
			POST("/rpc/mcpEndpoints.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createMcpEndpoint")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateMcpEndpoint"}`)
	})

	Method("getMcpEndpoint", func() {
		Description("Get an MCP endpoint by id or by (custom_domain_id, slug). Provide either id, or slug with an optional custom_domain_id — not both.")

		Payload(func() {
			Attribute("id", String, "The ID of the MCP endpoint", func() {
				Format(FormatUUID)
			})
			Attribute("custom_domain_id", String, "The ID of the custom domain the endpoint slug is registered under. Omit to look up a platform-domain endpoint.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", McpEndpointSlug, "The slug to look up")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpEndpoint)

		HTTP(func() {
			GET("/rpc/mcpEndpoints.get")
			Param("id")
			Param("custom_domain_id")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getMcpEndpoint")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetMcpEndpoint"}`)
	})

	Method("listMcpEndpoints", func() {
		Description("List MCP endpoints for a project. Optionally filter to only those associated with a specific MCP server.")

		Payload(func() {
			Attribute("mcp_server_id", String, "Optional filter: only return endpoints associated with this MCP server.", func() {
				Format(FormatUUID)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListMcpEndpointsResult)

		HTTP(func() {
			GET("/rpc/mcpEndpoints.list")
			Param("mcp_server_id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMcpEndpoints")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpEndpoints"}`)
	})

	Method("updateMcpEndpoint", func() {
		Description("Update an MCP endpoint. This is a full-record replace: fields omitted from the request become null on the stored record. The id, mcp_server_id, and slug fields are required.")

		Payload(func() {
			Extend(UpdateMcpEndpointForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpEndpoint)

		HTTP(func() {
			POST("/rpc/mcpEndpoints.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateMcpEndpoint")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateMcpEndpoint"}`)
	})

	Method("deleteMcpEndpoint", func() {
		Description("Delete an MCP endpoint")

		Payload(func() {
			Attribute("id", String, "The ID of the MCP endpoint to delete", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/mcpEndpoints.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteMcpEndpoint")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteMcpEndpoint"}`)
	})
})

var McpEndpointSlug = Type("McpEndpointSlug", String, func() {
	Description("A url-friendly label (up to 128 characters) that addresses an MCP server through a slug-based URL. Platform-domain slugs (no custom domain) must be prefixed with the organization slug.")
	Pattern(constants.SlugPattern)
	MaxLength(128)
	Meta("struct:pkg:path", "types")
})

var CreateMcpEndpointForm = Type("CreateMcpEndpointForm", func() {
	Description("Form for creating a new MCP endpoint. Platform-domain endpoint slugs (no custom_domain_id) must be prefixed with the organization slug.")

	Attribute("custom_domain_id", String, "The ID of the custom domain to register the endpoint slug under. Omit for a platform-domain endpoint.", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_id", String, "The ID of the MCP server this endpoint addresses", func() {
		Format(FormatUUID)
	})
	Attribute("slug", McpEndpointSlug, "The slug")

	Required("mcp_server_id", "slug")
})

var UpdateMcpEndpointForm = Type("UpdateMcpEndpointForm", func() {
	Description("Form for updating an MCP endpoint. This is a full-record replace: the custom_domain_id field omitted from the request becomes null on the stored record. Platform-domain endpoint slugs (no custom_domain_id) must be prefixed with the organization slug.")

	Attribute("id", String, "The ID of the MCP endpoint to update", func() {
		Format(FormatUUID)
	})
	Attribute("custom_domain_id", String, "The ID of the custom domain to register the endpoint slug under. Omit to move the endpoint to a platform domain.", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_id", String, "The ID of the MCP server this endpoint addresses", func() {
		Format(FormatUUID)
	})
	Attribute("slug", McpEndpointSlug, "The slug")

	Required("id", "mcp_server_id", "slug")
})

var McpEndpoint = Type("McpEndpoint", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP endpoint: a url-friendly slug identifier that addresses an MCP server.")

	Attribute("id", String, "The ID of the MCP endpoint", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID this MCP endpoint belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("custom_domain_id", String, "The ID of the custom domain this endpoint slug is registered under. Null for platform-domain endpoints.", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_id", String, "The ID of the MCP server this endpoint addresses", func() {
		Format(FormatUUID)
	})
	Attribute("slug", McpEndpointSlug, "The slug")
	Attribute("created_at", String, func() {
		Description("When the MCP endpoint was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the MCP endpoint was last updated")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "mcp_server_id", "slug", "created_at", "updated_at")
})

var ListMcpEndpointsResult = Type("ListMcpEndpointsResult", func() {
	Description("Result type for listing MCP endpoints")

	Attribute("mcp_endpoints", ArrayOf(McpEndpoint))
	Required("mcp_endpoints")
})
