package assistants

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("assistants", func() {
	Description("Manage assistants and their runtime configuration.")

	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listAssistants", func() {
		Description("List assistants for the current project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListAssistantsResult)

		HTTP(func() {
			GET("/rpc/assistants.list")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listAssistants")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
	})

	Method("getAssistant", func() {
		Description("Get an assistant by ID.")

		Payload(func() {
			Attribute("id", String, "The assistant ID.", func() {
				Format(FormatUUID)
			})
			Required("id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.Assistant)

		HTTP(func() {
			GET("/rpc/assistants.get")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAssistant")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
	})

	Method("createAssistant", func() {
		Description("Create an assistant.")

		Payload(func() {
			Extend(CreateAssistantForm)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.Assistant)

		HTTP(func() {
			POST("/rpc/assistants.create")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createAssistant")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
	})

	Method("updateAssistant", func() {
		Description("Update an assistant.")

		Payload(func() {
			Extend(UpdateAssistantForm)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.Assistant)

		HTTP(func() {
			POST("/rpc/assistants.update")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateAssistant")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
	})

	Method("deleteAssistant", func() {
		Description("Delete an assistant.")

		Payload(func() {
			Attribute("id", String, "The assistant ID.", func() {
				Format(FormatUUID)
			})
			Required("id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/assistants.delete")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteAssistant")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
	})
})

var CreateAssistantForm = Type("CreateAssistantForm", func() {
	Required("name", "model", "instructions", "toolsets")

	Attribute("name", String, "The assistant name.")
	Attribute("model", String, "The model identifier used by the assistant.")
	Attribute("instructions", String, "The system instructions for the assistant.")
	Attribute("toolsets", ArrayOf(shared.AssistantToolsetRef), "Toolsets available to the assistant.")
	Attribute("warm_ttl_seconds", Int, "Optional warm runtime TTL in seconds.")
	Attribute("max_concurrency", Int, "Optional maximum active warm runtimes.")
	Attribute("status", String, "Optional initial status.", func() {
		Enum("active", "paused")
	})
})

var UpdateAssistantForm = Type("UpdateAssistantForm", func() {
	Required("id")

	Attribute("id", String, "The assistant ID.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The assistant name.")
	Attribute("model", String, "The model identifier used by the assistant.")
	Attribute("instructions", String, "The system instructions for the assistant.")
	Attribute("toolsets", ArrayOf(shared.AssistantToolsetRef), "Toolsets available to the assistant.")
	Attribute("warm_ttl_seconds", Int, "Warm runtime TTL in seconds.")
	Attribute("max_concurrency", Int, "Maximum active warm runtimes.")
	Attribute("status", String, "The assistant status.", func() {
		Enum("active", "paused")
	})
})

var ListAssistantsResult = Type("ListAssistantsResult", func() {
	Attribute("assistants", ArrayOf(shared.Assistant), "Assistants for the current project.")
	Required("assistants")
})
