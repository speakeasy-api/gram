package toolsets

import (
	"github.com/speakeasy-api/gram/design/security"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("toolsets", func() {
	Description("Managed toolsets for gram AI consumers.")
	Security(security.Session, security.ProjectSlug)

	Method("createToolset", func() {
		Description("Create a new toolset with associated tools")

		Payload(func() {
			Extend(CreateToolsetForm)
			security.SessionPayload()
		})

		Result(ToolsetDetails)

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

		Result(ToolsetDetails)

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
			Attribute("slug", String, "The slug of the toolset")
			Required("slug")
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
			Attribute("slug", String, "The slug of the toolset")
			Required("slug")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ToolsetDetails)

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
})

var CreateToolsetForm = Type("CreateToolsetForm", func() {
	Attribute("name", String, "The name of the toolset")
	Attribute("description", String, "Description of the toolset")
	Attribute("http_tool_names", ArrayOf(String), "List of HTTP tool names to include")
	Attribute("default_environment_slug", String, "The slug of the environment to use as the default for the toolset")
	security.ProjectPayload()
	Required("name")
})

var ListToolsetsResult = Type("ListToolsetsResult", func() {
	Attribute("toolsets", ArrayOf(ToolsetDetails), "The list of toolsets")
	Required("toolsets")
})

var UpdateToolsetForm = Type("UpdateToolsetForm", func() {
	Attribute("slug", String, "The slug of the toolset to update")
	Attribute("name", String, "The new name of the toolset")
	Attribute("description", String, "The new description of the toolset")
	Attribute("default_environment_slug", String, "The slug of the environment to use as the default for the toolset")
	Attribute("http_tool_names", ArrayOf(String), "List of HTTP tool names to include")
	security.ProjectPayload()
	Required("slug")
})
