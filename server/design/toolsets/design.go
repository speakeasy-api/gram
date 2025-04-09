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

		Result(Toolset)

		HTTP(func() {
			POST("/rpc/toolsets.create")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

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

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListToolsets"}`)
	})

	Method("updateToolset", func() {
		Description("Update a toolset's properties including name, description, and HTTP tools")

		Payload(func() {
			Extend(UpdateToolsetForm)
			security.SessionPayload()
		})

		Result(Toolset)

		HTTP(func() {
			Param("slug")
			POST("/rpc/toolsets.update/{slug}")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

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
			DELETE("/rpc/toolsets.delete/{slug}")
			Response(StatusNoContent)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteToolset"}`)
	})

	Method("getToolsetDetails", func() {
		Description("Get detailed information about a toolset including full HTTP tool definitions")

		Payload(func() {
			Attribute("slug", String, "The slug of the toolset")
			Required("slug")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ToolsetDetails)

		HTTP(func() {
			Param("slug")
			security.SessionHeader()
			security.ProjectHeader()
			GET("/rpc/toolsets.get/{slug}")
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Toolset"}`)
	})
})

var CreateToolsetForm = Type("CreateToolsetForm", func() {
	Attribute("name", String, "The name of the toolset")
	Attribute("description", String, "Description of the toolset")
	Attribute("http_tool_names", ArrayOf(String), "List of HTTP tool names to include")
	Attribute("default_environment_id", String, "The ID of the environment to use as the default for the toolset")
	security.ProjectPayload()
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
	Attribute("http_tool_names", ArrayOf(String), "List of HTTP tool names included in this toolset")
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
	Attribute("slug", String, "The slug of the toolset to update")
	Attribute("name", String, "The new name of the toolset")
	Attribute("description", String, "The new description of the toolset")
	Attribute("default_environment_id", String, "The ID of the environment to use as the default for the toolset")
	Attribute("http_tool_names", ArrayOf(String), "List of HTTP tool names to include")
	security.ProjectPayload()
	Required("slug")
})
