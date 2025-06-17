package shared

import (
	. "goa.design/goa/v3/dsl"
)

var OrganizationEntry = Type("OrganizationEntry", func() {
	Attribute("id", String)
	Attribute("name", String)
	Attribute("slug", String)
	Attribute("account_type", String)
	Attribute("projects", ArrayOf(ProjectEntry))
	Attribute("sso_connection_id", String)
	Attribute("user_workspace_slugs", ArrayOf(String))
	Required("id", "name", "slug", "account_type", "projects")
})

var Project = Type("Project", func() {
	Required("id", "name", "slug", "organization_id", "created_at", "updated_at")

	Attribute("id", String, "The ID of the project")
	Attribute("name", String, "The name of the project")
	Attribute("slug", Slug, "The slug of the project")
	Attribute("organization_id", String, "The ID of the organization that owns the project")
	Attribute("created_at", String, func() {
		Description("The creation date of the project.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the project.")
		Format(FormatDateTime)
	})
})

var ProjectEntry = Type("ProjectEntry", func() {
	Attribute("id", String, "The ID of the project")
	Attribute("name", String, "The name of the project")
	Attribute("slug", Slug, "The slug of the project")
	Required("id", "name", "slug")
})
