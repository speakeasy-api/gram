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
		Description("Send a message from the dashboard to an assistant as the calling user. Continue an existing conversation by passing its chat_id (from listChats), or omit chat_id to start a new conversation — the server mints and returns a fresh chat id. The reply is delivered asynchronously; poll the chat service (loadChat) to read it.")

		Payload(func() {
			Attribute("assistant_id", String, "The assistant to send the message to.", func() {
				Format(FormatUUID)
			})
			Attribute("message", String, "The user's message text.", func() {
				MinLength(1)
				MaxLength(10000)
			})
			Attribute("chat_id", String, "The conversation to continue (from listChats or a prior sendMessage). Omit to start a new conversation; the server mints and returns a fresh chat id.", func() {
				Format(FormatUUID)
			})
			Attribute("idempotency_key", String, "Stable key the client mints once per message so retries dedupe instead of enqueuing twice. A new key is generated server-side when omitted.", func() {
				MaxLength(255)
			})
			Required("assistant_id", "message")

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

	Method("getManagedAssistant", func() {
		Description("Get the project's built-in Project Assistant if it exists. Returns 404 when no managed assistant has been provisioned yet — call ensureManagedAssistant to create one.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.Assistant)

		HTTP(func() {
			GET("/rpc/assistants.getManagedAssistant")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getManagedAssistant")
		Meta("openapi:extension:x-speakeasy-name-override", "getManaged")
	})

	Method("ensureManagedAssistant", func() {
		Description("Get the project's built-in Project Assistant, provisioning it on first access. Idempotent — safe to call on every sidebar open.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(shared.Assistant)

		HTTP(func() {
			POST("/rpc/assistants.ensureManagedAssistant")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "ensureManagedAssistant")
		Meta("openapi:extension:x-speakeasy-name-override", "ensureManaged")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "EnsureManagedAssistant", "type": "mutation"}`)
	})
})

var CreateAssistantForm = Type("CreateAssistantForm", func() {
	Required("name", "model", "instructions", "toolsets")

	Attribute("name", String, "The assistant name.")
	Attribute("model", String, "The model identifier used by the assistant.")
	Attribute("instructions", String, "The system instructions for the assistant.")
	Attribute("toolsets", ArrayOf(shared.AssistantToolsetRef), "Toolsets available to the assistant.")
	Attribute("mcp_servers", ArrayOf(shared.AssistantMCPServerRef), "MCP servers attached directly to the assistant (remote- or tunnelled-backed).")
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
	Attribute("mcp_servers", ArrayOf(shared.AssistantMCPServerRef), "MCP servers attached directly to the assistant (remote- or tunnelled-backed).")
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
	Attribute("thread_id", String, "The assistant thread the message was enqueued on, when the ingest produced one.", func() {
		Format(FormatUUID)
	})
	Attribute("accepted", Boolean, "Whether the message was accepted and enqueued for processing.")
	Required("chat_id", "accepted")
})
