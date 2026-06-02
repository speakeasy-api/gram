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

	Method("sendMessage", func() {
		Description("Send a message from the dashboard to an assistant as the calling user. The reply is delivered asynchronously; poll the returned chat to read it.")

		Payload(func() {
			Attribute("assistant_id", String, "The assistant to send the message to.", func() {
				Format(FormatUUID)
			})
			Attribute("message", String, "The user's message text.", func() {
				MinLength(1)
				MaxLength(10000)
			})
			Attribute("correlation_id", String, "Conversation key the message is threaded under. Send the user id for one continuing thread per user, or a fresh value to start a new conversation.", func() {
				MinLength(1)
				MaxLength(255)
			})
			Attribute("idempotency_key", String, "Stable key the client mints once per message so retries dedupe instead of enqueuing twice. A new key is generated server-side when omitted.", func() {
				MaxLength(255)
			})
			Required("assistant_id", "message", "correlation_id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(SendMessageResult)

		HTTP(func() {
			POST("/rpc/assistants.sendMessage")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "sendAssistantMessage")
		Meta("openapi:extension:x-speakeasy-name-override", "sendMessage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SendAssistantMessage"}`)
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

var SendMessageResult = Type("SendMessageResult", func() {
	Attribute("chat_id", String, "The chat to poll for the assistant's reply.", func() {
		Format(FormatUUID)
	})
	Attribute("thread_id", String, "The assistant thread the message was enqueued on.", func() {
		Format(FormatUUID)
	})
	Attribute("accepted", Boolean, "Whether the message was accepted and enqueued for processing.")
	Required("chat_id", "thread_id", "accepted")
})
