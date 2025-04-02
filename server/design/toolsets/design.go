package toolsets

import (
	"github.com/speakeasy-api/gram/design/sessions"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("toolsets", func() {
	Description("Managed toolsets for gram AI consumers.")
	Security(sessions.Session)

	Method("createToolset", func() {
		Description("Create a new toolset with associated tools")

		Payload(func() {
			Extend(CreateToolsetForm)
			sessions.SessionPayload()
		})

		Result(Toolset)

		HTTP(func() {
			POST("/rpc/toolsets.create")
			sessions.SessionHeader()
			sessions.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateToolset"}`)
	})

	Method("listToolsets", func() {
		Description("List all toolsets for a project")

		Payload(func() {
			sessions.SessionPayload()
			sessions.ProjectPayload()
		})

		Result(ListToolsetsResult)

		HTTP(func() {
			GET("/rpc/toolsets.list")
			sessions.SessionHeader()
			sessions.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListToolsets"}`)
	})

	Method("updateToolset", func() {
		Description("Update a toolset's properties including name, description, and HTTP tools")

		Payload(func() {
			Extend(UpdateToolsetForm)
			sessions.SessionPayload()
		})

		Result(Toolset)

		HTTP(func() {
			Param("id")
			POST("/rpc/toolsets.update/{id}")
			sessions.SessionHeader()
			sessions.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateToolset"}`)
	})

	Method("deleteToolset", func() {
		Description("Delete a toolset by its ID")

		Payload(func() {
			Attribute("id", String, "The ID of the toolset")
			Required("id")
			sessions.SessionPayload()
			sessions.ProjectPayload()
		})

		HTTP(func() {
			Param("id")
			sessions.SessionHeader()
			sessions.ProjectHeader()
			DELETE("/rpc/toolsets.delete/{id}")
			Response(StatusNoContent)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteToolset"}`)
	})

	Method("getToolsetDetails", func() {
		Description("Get detailed information about a toolset including full HTTP tool definitions")

		Payload(func() {
			Attribute("id", String, "The ID of the toolset")
			Required("id")
			sessions.SessionPayload()
			sessions.ProjectPayload()
		})

		Result(ToolsetDetails)

		HTTP(func() {
			Param("id")
			sessions.SessionHeader()
			sessions.ProjectHeader()
			GET("/rpc/toolsets.get/{id}")
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Toolset"}`)
	})
})

var CreateToolsetForm = Type("CreateToolsetForm", func() {
	Attribute("name", String, "The name of the toolset")
	Attribute("description", String, "Description of the toolset")
	Attribute("http_tool_ids", ArrayOf(String), "List of HTTP tool IDs to include")
	Attribute("default_environment_id", String, "The ID of the environment to use as the default for the toolset")
	sessions.ProjectPayload()
	Required("name")
})

var Toolset = Type("Toolset", func() {
	Attribute("id", String, "The ID of the toolset")
	Attribute("project_id", String, "The project ID this toolset belongs to")
	Attribute("organization_id", String, "The organization ID this toolset belongs to")
	Attribute("name", String, "The name of the toolset")
	Attribute("slug", String, "The slug of the toolset")
	Attribute("description", String, "Description of the toolset")
	Attribute("default_environment_id", String, "The ID of the environment to use as the default for the toolset")
	Attribute("http_tool_ids", ArrayOf(String), "List of HTTP tool IDs included in this toolset")
	Attribute("created_at", String, func() {
		Description("When the toolset was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the toolset was last updated.")
		Format(FormatDateTime)
	})
	Required("id", "project_id", "organization_id", "name", "slug", "created_at", "updated_at")
})

var ListToolsetsResult = Type("ListToolsetsResult", func() {
	Attribute("toolsets", ArrayOf(Toolset), "The list of toolsets")
	Required("toolsets")
})

var UpdateToolsetForm = Type("UpdateToolsetForm", func() {
	Attribute("id", String, "The ID of the toolset to update")
	Attribute("name", String, "The new name of the toolset")
	Attribute("description", String, "The new description of the toolset")
	Attribute("default_environment_id", String, "The ID of the environment to use as the default for the toolset")
	Attribute("http_tool_ids_to_add", ArrayOf(String), "HTTP tool IDs to add to the toolset")
	Attribute("http_tool_ids_to_remove", ArrayOf(String), "HTTP tool IDs to remove from the toolset")
	sessions.ProjectPayload()
	Required("id")
})

var HTTPToolDefinition = Type("HTTPToolDefinition", func() {
	Attribute("id", String, "The ID of the HTTP tool")
	Attribute("name", String, "The name of the tool")
	Attribute("description", String, "Description of the tool")
	Attribute("server_env_var", String, "Environment variable for the server URL")
	Attribute("security_type", String, "Type of security (http:bearer, http:basic, apikey)")
	Attribute("bearer_env_var", String, "Environment variable for bearer token")
	Attribute("apikey_env_var", String, "Environment variable for API key")
	Attribute("username_env_var", String, "Environment variable for username")
	Attribute("password_env_var", String, "Environment variable for password")
	Attribute("http_method", String, "HTTP method for the request")
	Attribute("path", String, "Path for the request")
	Attribute("headers_schema", String, "JSON schema for headers")
	Attribute("queries_schema", String, "JSON schema for query parameters")
	Attribute("pathparams_schema", String, "JSON schema for path parameters")
	Attribute("body_schema", String, "JSON schema for request body")
	Required("id", "name", "description", "server_env_var", "security_type", "http_method", "path")
})

var ToolsetDetails = Type("ToolsetDetails", func() {
	Attribute("id", String, "The ID of the toolset")
	Attribute("project_id", String, "The project ID this toolset belongs to")
	Attribute("organization_id", String, "The organization ID this toolset belongs to")
	Attribute("name", String, "The name of the toolset")
	Attribute("slug", String, "The slug of the toolset")
	Attribute("description", String, "Description of the toolset")
	Attribute("default_environment_id", String, "The ID of the environment to use as the default for the toolset")
	Attribute("http_tools", ArrayOf(HTTPToolDefinition), "The HTTP tools in this toolset")
	Attribute("created_at", String, func() {
		Description("When the toolset was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the toolset was last updated.")
		Format(FormatDateTime)
	})
	Required("id", "project_id", "organization_id", "name", "slug", "http_tools", "created_at", "updated_at")
})
