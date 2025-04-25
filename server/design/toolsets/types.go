package toolsets

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/design/shared"
	"github.com/speakeasy-api/gram/design/tools"
)

var ToolsetDetails = Type("ToolsetDetails", func() {
	Attribute("id", String, "The ID of the toolset")
	Attribute("project_id", String, "The project ID this toolset belongs to")
	Attribute("organization_id", String, "The organization ID this toolset belongs to")
	Attribute("name", String, "The name of the toolset")
	Attribute("slug", shared.Slug, "The slug of the toolset")
	Attribute("description", String, "Description of the toolset")
	Attribute("default_environment_slug", shared.Slug, "The slug of the environment to use as the default for the toolset")
	Attribute("relevant_environment_variables", ArrayOf(String), "The environment variables that are relevant to the toolset")
	Attribute("http_tools", ArrayOf(tools.HTTPToolDefinition), "The HTTP tools in this toolset")
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
