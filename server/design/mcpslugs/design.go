package mcpslugs

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

var _ = Service("mcpSlugs", func() {
	Description("Managing MCP slugs, the url-friendly identifiers that address MCP frontends.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createMcpSlug", func() {
		Description("Create a new MCP slug for an MCP frontend")

		Payload(func() {
			Extend(CreateMcpSlugForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpSlug)

		HTTP(func() {
			POST("/rpc/mcpSlugs.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createMcpSlug")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateMcpSlug"}`)
	})

	Method("getMcpSlug", func() {
		Description("Get an MCP slug by id or by (custom_domain_id, slug). Provide either id, or slug with an optional custom_domain_id — not both.")

		Payload(func() {
			Attribute("id", String, "The ID of the MCP slug", func() {
				Format(FormatUUID)
			})
			Attribute("custom_domain_id", String, "The ID of the custom domain the slug is registered under. Omit to look up a platform-domain slug.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", McpSlugString, "The slug to look up")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpSlug)

		HTTP(func() {
			GET("/rpc/mcpSlugs.get")
			Param("id")
			Param("custom_domain_id")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getMcpSlug")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetMcpSlug"}`)
	})

	Method("listMcpSlugs", func() {
		Description("List MCP slugs for a project. Optionally filter to only those associated with a specific MCP frontend.")

		Payload(func() {
			Attribute("mcp_frontend_id", String, "Optional filter: only return slugs associated with this MCP frontend.", func() {
				Format(FormatUUID)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListMcpSlugsResult)

		HTTP(func() {
			GET("/rpc/mcpSlugs.list")
			Param("mcp_frontend_id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMcpSlugs")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpSlugs"}`)
	})

	Method("updateMcpSlug", func() {
		Description("Update an MCP slug. This is a full-record replace: fields omitted from the request become null on the stored record. The id, mcp_frontend_id, and slug fields are required.")

		Payload(func() {
			Extend(UpdateMcpSlugForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpSlug)

		HTTP(func() {
			POST("/rpc/mcpSlugs.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateMcpSlug")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateMcpSlug"}`)
	})

	Method("deleteMcpSlug", func() {
		Description("Delete an MCP slug")

		Payload(func() {
			Attribute("id", String, "The ID of the MCP slug to delete", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/mcpSlugs.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteMcpSlug")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteMcpSlug"}`)
	})
})

var McpSlugString = Type("McpSlugString", String, func() {
	Description("A url-friendly label (up to 128 characters) that addresses an MCP frontend through a slug-based URL. Platform-domain slugs (no custom domain) must be prefixed with the organization slug.")
	Pattern(constants.SlugPattern)
	MaxLength(128)
	Meta("struct:pkg:path", "types")
})

var CreateMcpSlugForm = Type("CreateMcpSlugForm", func() {
	Description("Form for creating a new MCP slug. Platform-domain slugs (no custom_domain_id) must be prefixed with the organization slug.")

	Attribute("custom_domain_id", String, "The ID of the custom domain to register the slug under. Omit for a platform-domain slug.", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_frontend_id", String, "The ID of the MCP frontend this slug addresses", func() {
		Format(FormatUUID)
	})
	Attribute("slug", McpSlugString, "The slug")

	Required("mcp_frontend_id", "slug")
})

var UpdateMcpSlugForm = Type("UpdateMcpSlugForm", func() {
	Description("Form for updating an MCP slug. This is a full-record replace: the custom_domain_id field omitted from the request becomes null on the stored record. Platform-domain slugs (no custom_domain_id) must be prefixed with the organization slug.")

	Attribute("id", String, "The ID of the MCP slug to update", func() {
		Format(FormatUUID)
	})
	Attribute("custom_domain_id", String, "The ID of the custom domain to register the slug under. Omit to move the slug to a platform domain.", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_frontend_id", String, "The ID of the MCP frontend this slug addresses", func() {
		Format(FormatUUID)
	})
	Attribute("slug", McpSlugString, "The slug")

	Required("id", "mcp_frontend_id", "slug")
})

var McpSlug = Type("McpSlug", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP slug: a url-friendly identifier that addresses an MCP frontend.")

	Attribute("id", String, "The ID of the MCP slug", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID this MCP slug belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("custom_domain_id", String, "The ID of the custom domain this slug is registered under. Null for platform-domain slugs.", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_frontend_id", String, "The ID of the MCP frontend this slug addresses", func() {
		Format(FormatUUID)
	})
	Attribute("slug", McpSlugString, "The slug")
	Attribute("created_at", String, func() {
		Description("When the MCP slug was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the MCP slug was last updated")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "mcp_frontend_id", "slug", "created_at", "updated_at")
})

var ListMcpSlugsResult = Type("ListMcpSlugsResult", func() {
	Description("Result type for listing MCP slugs")

	Attribute("mcp_slugs", ArrayOf(McpSlug))
	Required("mcp_slugs")
})
