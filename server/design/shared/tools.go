package shared

import (
	. "goa.design/goa/v3/dsl"
)

var HTTPToolDefinition = Type("HTTPToolDefinition", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The ID of the HTTP tool")
	Attribute("project_id", String, "The ID of the project")
	Attribute("deployment_id", String, "The ID of the deployment")
	Attribute("openapiv3_document_id", String, "The ID of the OpenAPI v3 document")
	Attribute("name", String, "The name of the tool")
	Attribute("summary", String, "Summary of the tool")
	Attribute("description", String, "Description of the tool")
	Attribute("confirm", String, "Confirmation mode for the tool")
	Attribute("confirm_prompt", String, "Prompt for the confirmation")
	Attribute("openapiv3_operation", String, "OpenAPI v3 operation")
	Attribute("tags", ArrayOf(String), "The tags list for this http tool")
	Attribute("security", String, "Security requirements for the underlying HTTP endpoint")
	Attribute("http_method", String, "HTTP method for the request")
	Attribute("path", String, "Path for the request")
	Attribute("schema_version", String, "Version of the schema")
	Attribute("schema", String, "JSON schema for the request")
	Attribute("created_at", String, func() {
		Description("The creation date of the tool.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the tool.")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "deployment_id", "name", "summary", "description", "confirm", "tags", "http_method", "path", "schema", "created_at", "updated_at")
})

var Environment = Type("Environment", func() {
	Meta("struct:pkg:path", "types")

	Description("Model representing an environment")

	Attribute("id", String, "The ID of the environment")
	Attribute("organization_id", String, "The organization ID this environment belongs to")
	Attribute("project_id", String, "The project ID this environment belongs to")
	Attribute("name", String, "The name of the environment")
	Attribute("slug", Slug, "The slug identifier for the environment")
	Attribute("description", String, "The description of the environment")
	Attribute("entries", ArrayOf(EnvironmentEntry), "List of environment entries")
	Attribute("created_at", String, func() {
		Description("The creation date of the environment")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the environment was last updated")
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "project_id", "name", "slug", "entries", "created_at", "updated_at")
})

var EnvironmentEntry = Type("EnvironmentEntry", func() {
	Meta("struct:pkg:path", "types")

	Description("A single environment entry")

	Attribute("name", String, "The name of the environment variable")
	Attribute("value", String, "Redacted values of the environment variable")
	Attribute("created_at", String, func() {
		Description("The creation date of the environment entry")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the environment entry was last updated")
		Format(FormatDateTime)
	})

	Required("name", "value", "created_at", "updated_at")
})
