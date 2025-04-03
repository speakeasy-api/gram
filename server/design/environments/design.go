package environments

import (
	"github.com/speakeasy-api/gram/design/security"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("environments", func() {
	Description("Managing toolset environments.")
	Security(security.Session, security.ProjectSlug)

	Method("createEnvironment", func() {
		Description("Create a new environment")

		Payload(func() {
			Extend(CreateEnvironmentForm)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(Environment)

		HTTP(func() {
			POST("/rpc/environments.create")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateEnvironment"}`)
	})

	Method("listEnvironments", func() {
		Description("List all environments for an organization")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListEnvironmentsResult)

		HTTP(func() {
			GET("/rpc/environments.list")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListEnvironments"}`)
	})

	Method("updateEnvironment", func() {
		Description("Update an environment")

		Payload(func() {
			Extend(UpdateEnvironmentForm)
			Attribute("id", String, "The ID of the environment to update")
			Required("id")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(Environment)

		HTTP(func() {
			POST("/rpc/environments.update/{id}")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateEnvironment"}`)
	})

	Method("deleteEnvironment", func() {
		Description("Delete an environment")

		Payload(func() {
			Attribute("id", String, "The ID of the environment to delete")
			Required("id")
			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/environments.delete/{id}")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteEnvironment"}`)
	})
})

var Environment = Type("Environment", func() {
	Description("Model representing an environment")

	Attribute("id", String, "The ID of the environment")
	Attribute("organization_id", String, "The organization ID this environment belongs to")
	Attribute("project_id", String, "The project ID this environment belongs to")
	Attribute("name", String, "The name of the environment")
	Attribute("slug", String, "The slug identifier for the environment")
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
	Description("A single environment entry")

	Attribute("name", String, "The name of the environment variable")
	Attribute("value", String, "The value of the environment variable")
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

var EnvironmentEntryInput = Type("EnvironmentEntryInput", func() {
	Description("A single environment entry")

	Attribute("name", String, "The name of the environment variable")
	Attribute("value", String, "The value of the environment variable")

	Required("name", "value")
})

var CreateEnvironmentForm = Type("CreateEnvironmentForm", func() {
	Description("Form for creating a new environment")

	Attribute("organization_id", String, "The organization ID this environment belongs to")
	Attribute("name", String, "The name of the environment")
	Attribute("description", String, "Optional description of the environment")
	Attribute("entries", ArrayOf(EnvironmentEntryInput), "List of environment variable entries")

	Required("organization_id", "name", "entries")
})

var UpdateEnvironmentForm = Type("UpdateEnvironmentForm", func() {
	Description("Form for updating an environment")

	Attribute("environment_id", String, "The ID of the environment to update")
	Attribute("entries_to_update", ArrayOf(EnvironmentEntryInput), "List of environment entries to update or create")
	Attribute("entries_to_remove", ArrayOf(String), "List of environment entry names to remove")

	Required("environment_id", "entries_to_update", "entries_to_remove")
})

var ListEnvironmentsResult = Type("ListEnvironmentsResult", func() {
	Description("Result type for listing environments")

	Attribute("environments", ArrayOf(Environment))
	Required("environments")
})
