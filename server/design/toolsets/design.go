package toolsets

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("toolsets", func() {
	Description("Managed toolsets for gram AI consumers.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("createToolset", func() {
		Description("Create a new toolset with associated tools")

		Payload(func() {
			Extend(CreateToolsetForm)
			security.SessionPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			POST("/rpc/toolsets.create")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createToolset")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateToolset"}`)
	})

	Method("listToolsets", func() {
		Description("List all toolsets for a project")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListToolsetsResult)

		HTTP(func() {
			GET("/rpc/toolsets.list")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listToolsets")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListToolsets"}`)
	})

	Method("updateToolset", func() {
		Description("Update a toolset's properties including name, description, and HTTP tools")

		Payload(func() {
			Extend(UpdateToolsetForm)
			security.SessionPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			Param("slug")
			POST("/rpc/toolsets.update")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateToolset")
		Meta("openapi:extension:x-speakeasy-name-override", "updateBySlug")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateToolset"}`)
	})

	Method("deleteToolset", func() {
		Description("Delete a toolset by its ID")

		Payload(func() {
			Required("slug")
			Attribute("slug", shared.Slug, "The slug of the toolset")
			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			Param("slug")
			security.SessionHeader()
			security.ProjectHeader()
			DELETE("/rpc/toolsets.delete")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteToolset")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteBySlug")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteToolset"}`)
	})

	Method("getToolset", func() {
		Description("Get detailed information about a toolset including full HTTP tool definitions")

		Payload(func() {
			Required("slug")
			Attribute("slug", shared.Slug, "The slug of the toolset")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			GET("/rpc/toolsets.get")
			Param("slug")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getToolset")
		Meta("openapi:extension:x-speakeasy-name-override", "getBySlug")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Toolset"}`)
	})

	Method("checkMCPSlugAvailability", func() {
		Description("Check if a MCP slug is available")

		Payload(func() {
			Required("slug")
			Attribute("slug", shared.Slug, "The slug to check")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(Boolean)

		HTTP(func() {
			GET("/rpc/toolsets.checkMCPSlugAvailability")
			Param("slug")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "checkMCPSlugAvailability")
		Meta("openapi:extension:x-speakeasy-name-override", "checkMCPSlugAvailability")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CheckMCPSlugAvailability"}`)
	})

	Method("addExternalOAuthServer", func() {
		Description("Associate an external OAuth server with a toolset")

		Payload(func() {
			Extend(AddExternalOAuthServerForm)
			security.SessionPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			Param("slug")
			POST("/rpc/toolsets.addExternalOAuthServer")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "addExternalOAuthServer")
		Meta("openapi:extension:x-speakeasy-name-override", "addExternalOAuthServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AddExternalOAuthServer"}`)
	})

	Method("removeOAuthServer", func() {
		Description("Remove OAuth server association from a toolset")

		Payload(func() {
			Required("slug")
			Attribute("slug", shared.Slug, "The slug of the toolset")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			Param("slug")
			POST("/rpc/toolsets.removeOAuthServer")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "removeOAuthServer")
		Meta("openapi:extension:x-speakeasy-name-override", "removeOAuthServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoveOAuthServer"}`)
	})
})

var CreateToolsetForm = Type("CreateToolsetForm", func() {
	Attribute("name", String, "The name of the toolset")
	Attribute("description", String, "Description of the toolset")
	Attribute("http_tool_names", ArrayOf(String), "List of HTTP tool names to include")
	Attribute("default_environment_slug", shared.Slug, "The slug of the environment to use as the default for the toolset")
	security.ProjectPayload()
	Required("name")
})

var ListToolsetsResult = Type("ListToolsetsResult", func() {
	Attribute("toolsets", ArrayOf(shared.ToolsetEntry), "The list of toolsets")
	Required("toolsets")
})

var UpdateToolsetForm = Type("UpdateToolsetForm", func() {
	Attribute("slug", shared.Slug, "The slug of the toolset to update")
	Attribute("name", String, "The new name of the toolset")
	Attribute("description", String, "The new description of the toolset")
	Attribute("default_environment_slug", shared.Slug, "The slug of the environment to use as the default for the toolset")
	Attribute("http_tool_names", ArrayOf(String), "List of HTTP tool names to include")
	Attribute("prompt_template_names", ArrayOf(String), "List of prompt template names to include")
	Attribute("mcp_enabled", Boolean, "Whether the toolset is enabled for MCP")
	Attribute("mcp_slug", shared.Slug, "The slug of the MCP to use for the toolset")
	Attribute("mcp_is_public", Boolean, "Whether the toolset is public in MCP")
	Attribute("custom_domain_id", String, "The ID of the custom domain to use for the toolset")
	security.ProjectPayload()
	Required("slug")
})

var AddExternalOAuthServerForm = Type("AddExternalOAuthServerForm", func() {
	Attribute("slug", shared.Slug, "The slug of the toolset to update")
	Attribute("external_oauth_server", shared.ExternalOAuthServerForm, "The external OAuth server data to create and associate with the toolset")
	security.ProjectPayload()
	Required("slug", "external_oauth_server")
})
