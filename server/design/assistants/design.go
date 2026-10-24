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

	Method("upgradeAssistantIdentity", func() {
		Description("Give an existing assistant an agent and per-trigger workload identities. By default a new agent is created with access to every MCP server and skill in the project and to administering this assistant. Passing agent_id points the assistant at an existing agent of the project instead; that agent keeps its policy and gains administration of this assistant. Either way the agent is managed like any other agent afterwards. Existing assistants are never upgraded implicitly; repeating the upgrade is safe.")
		Payload(func() {
			Attribute("id", String, "The assistant ID.", func() { Format(FormatUUID) })
			Attribute("agent_id", String, "An existing agent of the project to use instead of creating one. Requires owning the agent or agent:authorize on it.", func() { Format(FormatUUID) })
			Attribute("agent_name", String, "The name of the new agent. Cannot be combined with agent_id.", func() {
				MinLength(1)
				MaxLength(120)
			})
			Required("id")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(shared.Assistant)
		HTTP(func() {
			POST("/rpc/assistants.upgradeIdentity")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "upgradeAssistantIdentity")
		Meta("openapi:extension:x-speakeasy-name-override", "upgradeIdentity")
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
			Attribute("message", String, "The user's message text. May be empty when the turn carries attachments.", func() {
				MaxLength(10000)
			})
			Attribute("chat_id", String, "The conversation to continue (from listChats or a prior sendMessage). Omit to start a new conversation; the server mints and returns a fresh chat id.", func() {
				Format(FormatUUID)
			})
			Attribute("idempotency_key", String, "Stable key the client mints once per message so retries dedupe instead of enqueuing twice. A new key is generated server-side when omitted.", func() {
				MaxLength(255)
			})
			Attribute("skill_ids", ArrayOf(String, func() {
				Format(FormatUUID)
			}), "Project skills to make available for this turn.", func() {
				MaxLength(10)
			})
			Attribute("attachments", ArrayOf(SendMessageAttachment), "Files uploaded through assets.uploadChatAttachment that this turn carries.", func() {
				MaxLength(5)
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

	Method("interruptTurn", func() {
		Description("Stop whatever a conversation is currently generating. Cancels turns still queued on the conversation's thread and interrupts the turn in flight on the assistant runtime, so the reply stops where it is rather than finishing in the background. Idempotent and safe to call when nothing is running: `stopped` is false when the reply had already finished.")

		Payload(func() {
			Attribute("assistant_id", String, "The assistant whose conversation should stop generating.", func() {
				Format(FormatUUID)
			})
			Attribute("chat_id", String, "The conversation to stop, as returned by sendMessage.", func() {
				Format(FormatUUID)
			})
			Required("assistant_id", "chat_id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(InterruptTurnResult)

		HTTP(func() {
			POST("/rpc/assistants.interruptTurn")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "interruptAssistantTurn")
		Meta("openapi:extension:x-speakeasy-name-override", "interruptTurn")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "InterruptAssistantTurn", "type": "mutation"}`)
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

	Method("listExecutionProfiles", func() {
		Description("List the upstream credentials an assistant's turns use on each MCP server it reaches, per root trigger and per whether the turn acts for a user. Each server shows its current choice, or why it has none, and the credentials that can be chosen. Requires assistant read access.")

		Payload(func() {
			Attribute("assistant_id", String, "The assistant ID.", func() {
				Format(FormatUUID)
			})
			Required("assistant_id")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListExecutionProfilesResult)

		HTTP(func() {
			GET("/rpc/assistants.listExecutionProfiles")
			Param("assistant_id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listAssistantExecutionProfiles")
		Meta("openapi:extension:x-speakeasy-name-override", "listExecutionProfiles")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AssistantExecutionProfiles"}`)
	})

	Method("setExecutionProfileChoice", func() {
		Description("Choose the upstream credential an assistant's turns use on one MCP server, for one root trigger and execution context: an auth client, no upstream authentication, or back to the choice the next turn makes on its own. Turns already running keep their choice; later turns use the new one. Turns that act for nobody can use shared credentials only. Choosing grants nothing: each request checks the turn may use the credential. Requires assistant write access.")

		Payload(func() {
			Attribute("assistant_id", String, "The assistant ID.", func() {
				Format(FormatUUID)
			})
			Attribute("trigger_instance_id", String, "The root trigger.", func() {
				Format(FormatUUID)
			})
			Attribute("execution_context", String, "Whether the turns act for the user their event identified.", func() {
				Enum("user_backed", "userless")
			})
			Attribute("mcp_server_id", String, "The MCP server.", func() {
				Format(FormatUUID)
			})
			Attribute("auth_client_id", String, "The auth client to use.", func() {
				Format(FormatUUID)
			})
			Attribute("no_auth", Boolean, "Use no upstream authentication.")
			Attribute("automatic", Boolean, "Remove the choice so the next turn makes it from the server's configuration.")
			Attribute("expected_version", Int64, "The profile version the change was decided from, from a fresh listExecutionProfiles read. A profile that changed since is refused with a conflict.", func() {
				Minimum(0)
			})
			Required("assistant_id", "trigger_instance_id", "execution_context", "mcp_server_id", "expected_version")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(AssistantExecutionProfile)

		HTTP(func() {
			POST("/rpc/assistants.setExecutionProfileChoice")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setAssistantExecutionProfileChoice")
		Meta("openapi:extension:x-speakeasy-name-override", "setExecutionProfileChoice")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetAssistantExecutionProfileChoice"}`)
	})
})

var CreateAssistantForm = Type("CreateAssistantForm", func() {
	Required("name", "model", "instructions", "toolsets")

	Attribute("name", String, "The assistant name.")
	Attribute("model", String, "The model identifier used by the assistant.")
	Attribute("instructions", String, "The system instructions for the assistant.")
	Attribute("toolsets", ArrayOf(shared.AssistantToolsetRef), "Hosted (toolset-backed) MCP servers to attach, by toolset slug. When sent, replaces the assistant's hosted servers.")
	Attribute("mcp_servers", ArrayOf(shared.AssistantMCPServerRef), "MCP servers to attach. When sent, replaces the assistant's servers that are not hosted (toolset-backed). A hosted server listed here is attached if it is not already; attached hosted servers are left unchanged, and toolsets governs them when both are sent.")
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
	Attribute("toolsets", ArrayOf(shared.AssistantToolsetRef), "Hosted (toolset-backed) MCP servers to attach, by toolset slug. When sent, replaces the assistant's hosted servers.")
	Attribute("mcp_servers", ArrayOf(shared.AssistantMCPServerRef), "MCP servers to attach. When sent, replaces the assistant's servers that are not hosted (toolset-backed). A hosted server listed here is attached if it is not already; attached hosted servers are left unchanged, and toolsets governs them when both are sent.")
	Attribute("warm_ttl_seconds", Int, "Warm runtime TTL in seconds.")
	Attribute("max_concurrency", Int, "Maximum active warm runtimes.")
	Attribute("runtime_version", Int, "Desired runtime protocol: 2 for the default runtime, 3 for Kit. Switching requires the assistant and all background work to be settled.", func() { Enum(2, 3) })
	Attribute("expected_runtime_version", Int, "The runtime version read before requesting a switch. Required with runtime_version; conflicts require a fresh read.", func() { Enum(2, 3) })
	Attribute("status", String, "The assistant status.", func() {
		Enum("active", "paused")
	})
})

var SendMessageAttachment = Type("SendMessageAttachment", func() {
	Required("asset_id")

	Attribute("asset_id", String, "The chat attachment asset returned by assets.uploadChatAttachment.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The file name to show the assistant. Falls back to the stored asset name.", func() {
		MaxLength(255)
	})
})

var ListAssistantsResult = Type("ListAssistantsResult", func() {
	Attribute("assistants", ArrayOf(shared.Assistant), "Assistants for the current project.")
	Required("assistants")
})

var InterruptTurnResult = Type("InterruptTurnResult", func() {
	Attribute("stopped", Boolean, "Whether the call stopped anything. False means nothing was generating — the reply had already finished, or the conversation never started a turn.")
	Attribute("interrupted", Boolean, "Whether a turn in flight on the assistant runtime was cancelled.")
	Attribute("cancelled_queued", Int, "How many turns were dropped from the conversation's queue before any runtime claimed them.")
	Required("stopped", "interrupted", "cancelled_queued")
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

var ExecutionProfileOption = Type("ExecutionProfileOption", func() {
	Description("A credential a server offers that the profile's turns can use.")
	Attribute("auth_client_id", String, "The auth client.", func() { Format(FormatUUID) })
	Attribute("name", String, "The auth client's name.")
	Attribute("client_type", String, "The auth client's type.")
	Required("auth_client_id", "name", "client_type")
})

var ExecutionProfileServer = Type("ExecutionProfileServer", func() {
	Description("One MCP server an assistant's turns reach through Speakeasy.")
	Attribute("mcp_server_id", String, "The MCP server.", func() { Format(FormatUUID) })
	Attribute("chosen", Boolean, "Whether the server has a choice.")
	Attribute("auth_client_id", String, "The chosen auth client. Absent with a choice means no upstream authentication.", func() { Format(FormatUUID) })
	Attribute("source", String, "How the choice was made: admin, issuance, backfill, or automatic for one the next turn makes on its own.")
	Attribute("gap_reason", String, "Why a server without a choice gets none on its own.", func() {
		Enum("selection_required", "personal_requires_user", "no_client", "setup_incomplete", "upstream_auth_undecided")
	})
	Attribute("options", ArrayOf(ExecutionProfileOption), "The credentials that can be chosen.")
	Required("mcp_server_id", "chosen", "options")
})

var AssistantExecutionProfile = Type("AssistantExecutionProfile", func() {
	Description("The upstream credentials an assistant's turns under one root trigger and execution context use.")
	Attribute("trigger_instance_id", String, "The root trigger.", func() { Format(FormatUUID) })
	Attribute("trigger_name", String, "The root trigger's name.")
	Attribute("execution_context", String, "Whether the turns act for the user their event identified.", func() {
		Enum("user_backed", "userless")
	})
	Attribute("version", Int64, "The latest version; 0 before the first turn or change.")
	Attribute("servers", ArrayOf(ExecutionProfileServer), "Each server the turns reach.")
	Required("trigger_instance_id", "trigger_name", "execution_context", "version", "servers")
})

var ListExecutionProfilesResult = Type("ListExecutionProfilesResult", func() {
	Attribute("profiles", ArrayOf(AssistantExecutionProfile), "Each root trigger and execution context.")
	Required("profiles")
})
