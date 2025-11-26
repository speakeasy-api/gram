package environments

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("environments", func() {
	Description("Managing toolset environments.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("createEnvironment", func() {
		Description("Create a new environment")

		Payload(func() {
			Extend(CreateEnvironmentForm)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.Environment)

		HTTP(func() {
			POST("/rpc/environments.create")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createEnvironment")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
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

		Meta("openapi:operationId", "listEnvironments")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListEnvironments"}`)
	})

	Method("updateEnvironment", func() {
		Description("Update an environment")

		Payload(func() {
			Extend(UpdateEnvironmentForm)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.Environment)

		HTTP(func() {
			POST("/rpc/environments.update")
			Param("slug")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateEnvironment")
		Meta("openapi:extension:x-speakeasy-name-override", "updateBySlug")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateEnvironment"}`)
	})

	Method("deleteEnvironment", func() {
		Description("Delete an environment")

		Payload(func() {
			Attribute("slug", shared.Slug, "The slug of the environment to delete")
			Required("slug")
			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/environments.delete")
			Param("slug")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteEnvironment")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteBySlug")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteEnvironment"}`)
	})

	Method("setSourceEnvironmentLink", func() {
		Description("Set (upsert) a link between a source and an environment")

		Payload(func() {
			Attribute("source_kind", SourceKind, "The kind of source (http or function)")
			Attribute("source_slug", String, "The slug of the source")
			Attribute("environment_id", String, "The ID of the environment to link", func() {
				Format(FormatUUID)
			})
			Required("source_kind", "source_slug", "environment_id")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(SourceEnvironmentLink)

		HTTP(func() {
			PUT("/rpc/environments.setSourceLink")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setSourceEnvironmentLink")
		Meta("openapi:extension:x-speakeasy-name-override", "setSourceLink")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetSourceEnvironmentLink"}`)
	})

	Method("deleteSourceEnvironmentLink", func() {
		Description("Delete a link between a source and an environment")

		Payload(func() {
			Attribute("source_kind", SourceKind, "The kind of source (http or function)")
			Attribute("source_slug", String, "The slug of the source")
			Required("source_kind", "source_slug")
			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/environments.deleteSourceLink")
			Param("source_kind")
			Param("source_slug")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteSourceEnvironmentLink")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteSourceLink")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteSourceEnvironmentLink"}`)
	})

	Method("getSourceEnvironment", func() {
		Description("Get the environment linked to a source")

		Payload(func() {
			Attribute("source_kind", SourceKind, "The kind of source (http or function)")
			Attribute("source_slug", String, "The slug of the source")
			Required("source_kind", "source_slug")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.Environment)

		HTTP(func() {
			GET("/rpc/environments.getSourceEnvironment")
			Param("source_kind")
			Param("source_slug")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getSourceEnvironment")
		Meta("openapi:extension:x-speakeasy-name-override", "getBySource")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetSourceEnvironment"}`)
	})

	Method("setToolsetEnvironmentLink", func() {
		Description("Set (upsert) a link between a toolset and an environment")

		Payload(func() {
			Attribute("toolset_id", String, "The ID of the toolset", func() {
				Format(FormatUUID)
			})
			Attribute("environment_id", String, "The ID of the environment to link", func() {
				Format(FormatUUID)
			})
			Required("toolset_id", "environment_id")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ToolsetEnvironmentLink)

		HTTP(func() {
			PUT("/rpc/environments.setToolsetLink")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setToolsetEnvironmentLink")
		Meta("openapi:extension:x-speakeasy-name-override", "setToolsetLink")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetToolsetEnvironmentLink"}`)
	})

	Method("deleteToolsetEnvironmentLink", func() {
		Description("Delete a link between a toolset and an environment")

		Payload(func() {
			Attribute("toolset_id", String, "The ID of the toolset", func() {
				Format(FormatUUID)
			})
			Required("toolset_id")
			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/environments.deleteToolsetLink")
			Param("toolset_id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteToolsetEnvironmentLink")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteToolsetLink")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteToolsetEnvironmentLink"}`)
	})

	Method("getToolsetEnvironment", func() {
		Description("Get the environment linked to a toolset")

		Payload(func() {
			Attribute("toolset_id", String, "The ID of the toolset", func() {
				Format(FormatUUID)
			})
			Required("toolset_id")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.Environment)

		HTTP(func() {
			GET("/rpc/environments.getToolsetEnvironment")
			Param("toolset_id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getToolsetEnvironment")
		Meta("openapi:extension:x-speakeasy-name-override", "getByToolset")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetToolsetEnvironment"}`)
	})
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

	Attribute("slug", shared.Slug, "The slug of the environment to update")
	Attribute("description", String, "The description of the environment")
	Attribute("name", String, "The name of the environment")
	Attribute("entries_to_update", ArrayOf(EnvironmentEntryInput), "List of environment entries to update or create")
	Attribute("entries_to_remove", ArrayOf(String), "List of environment entry names to remove")

	Required("slug", "entries_to_update", "entries_to_remove")
})

var ListEnvironmentsResult = Type("ListEnvironmentsResult", func() {
	Description("Result type for listing environments")

	Attribute("environments", ArrayOf(shared.Environment))
	Required("environments")
})

var SourceKind = Type("SourceKind", String, func() {
	Description("The kind of source that can be linked to an environment")
	Enum("http", "function")
})

var SourceEnvironmentLink = Type("SourceEnvironmentLink", func() {
	Description("A link between a source and an environment")

	Attribute("id", String, "The ID of the source environment link", func() {
		Format(FormatUUID)
	})
	Attribute("source_kind", SourceKind, "The kind of source (http or function)")
	Attribute("source_slug", String, "The slug of the source")
	Attribute("environment_id", String, "The ID of the environment", func() {
		Format(FormatUUID)
	})

	Required("id", "source_kind", "source_slug", "environment_id")
})

var ToolsetEnvironmentLink = Type("ToolsetEnvironmentLink", func() {
	Description("A link between a toolset and an environment")

	Attribute("id", String, "The ID of the toolset environment link", func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, "The ID of the toolset", func() {
		Format(FormatUUID)
	})
	Attribute("environment_id", String, "The ID of the environment", func() {
		Format(FormatUUID)
	})

	Required("id", "toolset_id", "environment_id")
})
