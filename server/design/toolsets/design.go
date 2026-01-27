package toolsets

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("toolsets", func() {
	Description("Managed toolsets for gram AI consumers.")

	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})

	shared.DeclareErrorResponses()

	Method("createToolset", func() {
		Description("Create a new toolset with associated tools")

		Payload(func() {
			Extend(CreateToolsetForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			POST("/rpc/toolsets.create")
			security.SessionHeader()
			security.ByKeyHeader()
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
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListToolsetsResult)

		HTTP(func() {
			GET("/rpc/toolsets.list")
			security.SessionHeader()
			security.ByKeyHeader()
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
			security.ByKeyPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			Param("slug")
			POST("/rpc/toolsets.update")
			security.SessionHeader()
			security.ByKeyHeader()
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
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
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
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			GET("/rpc/toolsets.get")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
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
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(Boolean)

		HTTP(func() {
			GET("/rpc/toolsets.checkMCPSlugAvailability")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "checkMCPSlugAvailability")
		Meta("openapi:extension:x-speakeasy-name-override", "checkMCPSlugAvailability")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CheckMCPSlugAvailability"}`)
	})

	Method("cloneToolset", func() {
		Description("Clone an existing toolset with a new name")

		Payload(func() {
			Required("slug")
			Attribute("slug", shared.Slug, "The slug of the toolset to clone")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			POST("/rpc/toolsets.clone")
			Param("slug")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "cloneToolset")
		Meta("openapi:extension:x-speakeasy-name-override", "cloneBySlug")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CloneToolset"}`)
	})

	Method("addExternalOAuthServer", func() {
		Description("Associate an external OAuth server with a toolset")

		Payload(func() {
			Extend(AddExternalOAuthServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			Param("slug")
			POST("/rpc/toolsets.addExternalOAuthServer")
			security.SessionHeader()
			security.ByKeyHeader()
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
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			Param("slug")
			POST("/rpc/toolsets.removeOAuthServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "removeOAuthServer")
		Meta("openapi:extension:x-speakeasy-name-override", "removeOAuthServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoveOAuthServer"}`)
	})

	Method("addOAuthProxyServer", func() {
		Description("Associate an OAuth proxy server with a toolset (admin only)")

		Payload(func() {
			Extend(AddOAuthProxyServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(shared.Toolset)

		HTTP(func() {
			Param("slug")
			POST("/rpc/toolsets.addOAuthProxyServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "addOAuthProxyServer")
		Meta("openapi:extension:x-speakeasy-name-override", "addOAuthProxyServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AddOAuthProxyServer"}`)
	})

	Method("updateSecurityVariableDisplayName", func() {
		Description("Update the display name of a security variable for user-friendly presentation")

		Payload(func() {
			Extend(UpdateSecurityVariableDisplayNameForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(shared.SecurityVariable)

		HTTP(func() {
			POST("/rpc/toolsets.updateSecurityVariableDisplayName")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateSecurityVariableDisplayName")
		Meta("openapi:extension:x-speakeasy-name-override", "updateSecurityVariableDisplayName")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateSecurityVariableDisplayName"}`)
	})
})

var CreateToolsetForm = Type("CreateToolsetForm", func() {
	Attribute("name", String, "The name of the toolset")
	Attribute("description", String, "Description of the toolset")
	Attribute("tool_urns", ArrayOf(String), "List of tool URNs to include in the toolset")
	Attribute("resource_urns", ArrayOf(String), "List of resource URNs to include in the toolset")
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
	Attribute("prompt_template_names", ArrayOf(String), "List of prompt template names to include (note: for actual prompts, not tools)")
	Attribute("tool_urns", ArrayOf(String), "List of tool URNs to include in the toolset")
	Attribute("resource_urns", ArrayOf(String), "List of resource URNs to include in the toolset")
	Attribute("mcp_enabled", Boolean, "Whether the toolset is enabled for MCP")
	Attribute("mcp_slug", shared.Slug, "The slug of the MCP to use for the toolset")
	Attribute("mcp_is_public", Boolean, "Whether the toolset is public in MCP")
	Attribute("custom_domain_id", String, "The ID of the custom domain to use for the toolset")
	Attribute("tool_selection_mode", String, "The mode to use for tool selection")
	security.ProjectPayload()
	Required("slug")
})

var AddExternalOAuthServerForm = Type("AddExternalOAuthServerForm", func() {
	Attribute("slug", shared.Slug, "The slug of the toolset to update")
	Attribute("external_oauth_server", shared.ExternalOAuthServerForm, "The external OAuth server data to create and associate with the toolset")
	security.ProjectPayload()
	Required("slug", "external_oauth_server")
})

var AddOAuthProxyServerForm = Type("AddOAuthProxyServerForm", func() {
	Attribute("slug", shared.Slug, "The slug of the toolset to update")
	Attribute("oauth_proxy_server", shared.OAuthProxyServerForm, "The OAuth proxy server data to create and associate with the toolset")
	security.ProjectPayload()
	Required("slug", "oauth_proxy_server")
})

var UpdateSecurityVariableDisplayNameForm = Type("UpdateSecurityVariableDisplayNameForm", func() {
	Attribute("toolset_slug", shared.Slug, "The slug of the toolset containing the security variable")
	Attribute("security_key", String, func() {
		Description("The security scheme key (e.g., 'BearerAuth', 'ApiKeyAuth') from the OpenAPI spec")
		MaxLength(60)
	})
	Attribute("display_name", String, func() {
		Description("The user-friendly display name. Set to empty string to clear and use the original name.")
		MaxLength(120)
	})
	security.ProjectPayload()
	Required("toolset_slug", "security_key", "display_name")
})
