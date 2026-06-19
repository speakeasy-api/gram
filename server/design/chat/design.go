package toolsets

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("chat", func() {
	Description("Managed chats for gram AI consumers.")

	Security(security.Session, security.ProjectSlug)
	Security(security.ChatSessionsToken)

	shared.DeclareErrorResponses()

	Method("listChats", func() {
		Description("List all chats for a project")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
			Attribute("search", String, "Search query (searches chat ID, user ID, and title)")
			Attribute("external_user_id", String, "Filter by external user ID")
			Attribute("assistant_id", String, "Filter to chats produced by this assistant", func() {
				Format(FormatUUID)
			})
			Attribute("has_risk", String, "Filter by whether chat has risk findings: 'true', 'false', or empty for no filter.", func() {
				Enum("", "true", "false")
			})
			Attribute("from", String, "Filter chats last active after this timestamp (ISO 8601)", func() {
				Format(FormatDateTime)
			})
			Attribute("to", String, "Filter chats last active before this timestamp (ISO 8601)", func() {
				Format(FormatDateTime)
			})
			Attribute("limit", Int, "Number of results per page", func() {
				Default(50)
				Minimum(1)
				Maximum(100)
			})
			Attribute("offset", Int, "Pagination offset", func() {
				Default(0)
				Minimum(0)
			})
			Attribute("sort_by", String, "Field to sort by", func() {
				Enum("last_message_timestamp", "num_messages")
				Default("last_message_timestamp")
			})
			Attribute("sort_order", String, "Sort order", func() {
				Enum("asc", "desc")
				Default("desc")
			})
		})

		Result(ListChatsResult)

		HTTP(func() {
			GET("/rpc/chat.list")
			Param("search")
			Param("external_user_id")
			Param("assistant_id")
			Param("has_risk")
			Param("from")
			Param("to")
			Param("limit")
			Param("offset")
			Param("sort_by")
			Param("sort_order")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listChats")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListChats", "type": "query"}`)
	})

	Method("loadChat", func() {
		Description("Load a chat by its ID. Messages are paginated one generation per request; omit `generation` to receive the latest generation.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
			Attribute("id", String, "The ID of the chat")
			Attribute("generation", Int, "Generation to load. A generation is an immutable snapshot of the chat transcript: a new one is opened whenever the conversation is compacted or an earlier message is edited, while normal turns append to the current generation. Generations are numbered from 0 (oldest) up to `max_generation` (latest). Omit this attribute to receive the latest generation, or page through history by walking from `max_generation` down to 0.", func() {
				Minimum(0)
			})
			Required("id")
		})

		Result(Chat)

		HTTP(func() {
			GET("/rpc/chat.load")
			Param("id")
			Param("generation")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "loadChat")
		Meta("openapi:extension:x-speakeasy-name-override", "load")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "LoadChat"}`)
	})

	Method("generateTitle", func() {
		Description("Generate a title for a chat based on its messages")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
			Attribute("id", String, "The ID of the chat")
			Required("id")
		})

		Result(func() {
			Attribute("title", String, "The generated title")
			Required("title")
		})

		HTTP(func() {
			POST("/rpc/chat.generateTitle")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "generateTitle")
		Meta("openapi:extension:x-speakeasy-name-override", "generateTitle")
	})

	Method("creditUsage", func() {
		// credit usage is counted at the organization level, no project slug is required.
		Security(security.Session)

		Description("Get the total number of chat credits and usage for the current billing period")

		Payload(func() {
			security.SessionPayload()
		})

		Result(func() {
			Attribute("credits_used", Float64, "The number of credits remaining")
			Attribute("monthly_credits", Int, "The number of monthly credits")
			Required("credits_used", "monthly_credits")
		})

		HTTP(func() {
			GET("/rpc/chat.creditUsage")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "creditUsage")
		Meta("openapi:extension:x-speakeasy-name-override", "creditUsage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetCreditUsage"}`)
	})

	Method("deleteChat", func() {
		Description("Soft-delete a chat by its ID")
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("id", String, "The ID of the chat to delete")
			Required("id")
		})

		HTTP(func() {
			DELETE("/rpc/chat.delete")
			Param("id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteChat")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
	})

	Method("submitFeedback", func() {
		Description("Submit user feedback for a chat (success/failure)")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
			Attribute("id", String, "The ID of the chat")
			Attribute("feedback", String, "User feedback: success or failure", func() {
				Enum("success", "failure")
			})
			Required("id", "feedback")
		})

		Result(func() {
			Attribute("success", Boolean, "Whether the feedback was submitted successfully")
			Required("success")
		})

		HTTP(func() {
			POST("/rpc/chat.submitFeedback")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "submitFeedback")
		Meta("openapi:extension:x-speakeasy-name-override", "submitFeedback")
	})
})

var ListChatsResult = Type("ListChatsResult", func() {
	Attribute("chats", ArrayOf(ChatOverview), "The list of chats")
	Attribute("total", Int, "Total number of chats (before pagination)")
	Required("chats", "total")
})

var ChatOverview = Type("ChatOverview", func() {
	Attribute("id", String, "The ID of the chat")
	Attribute("title", String, "The title of the chat")
	Attribute("user_id", String, "The ID of the user who created the chat")
	Attribute("external_user_id", String, "The ID of the external user who created the chat")
	Attribute("num_messages", Int, "The number of messages in the chat")
	Attribute("source", String, "The source of the chat: Elements, Playground, ClaudeCode (inferred from messages)")
	Attribute("created_at", String, func() {
		Description("When the chat was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the chat was last updated.")
		Format(FormatDateTime)
	})
	Attribute("total_input_tokens", Int64, "Total input tokens used in this chat")
	Attribute("total_output_tokens", Int64, "Total output tokens used in this chat")
	Attribute("total_tokens", Int64, "Total tokens (input + output) used in this chat")
	Attribute("total_cost", Float64, "Total cost in USD for this chat")
	Attribute("last_message_timestamp", String, func() {
		Description("When the last message in the chat was created.")
		Format(FormatDateTime)
	})
	Attribute("risk_findings_count", Int, "Number of risk findings recorded against messages in this chat (project-scoped, found=true). Only populated by endpoints that join risk data; absent elsewhere.")

	Required("id", "title", "num_messages", "created_at", "updated_at", "last_message_timestamp")
})

var Chat = Type("Chat", func() {
	Extend(ChatOverview)
	Attribute("messages", ArrayOf(ChatMessage), "The list of messages in the chat for the returned generation")
	Attribute("generation", Int, "The generation that this response's messages belong to. A generation is an immutable snapshot of the transcript; a new one is opened on compaction or message edits, while normal turns append to the current one.")
	Attribute("max_generation", Int, "The highest generation number present for this chat. To load the full history, walk from `max_generation` down to 0, requesting each generation in turn.")
	Attribute("agent_usage", AgentUsage, "Agent-specific usage enrichment for the chat, when available.")

	Required("messages", "generation", "max_generation")
})

var ChatMessage = Type("ChatMessage", func() {
	Attribute("id", String, "The ID of the message")
	Attribute("role", String, "The role of the message")
	Attribute("content", Any, "The content of the message — string for plain text, array for multimodal/tool-call content parts, null for assistant messages that only carry tool_calls", func() {
		Meta("struct:field:type", "json.RawMessage", "encoding/json")
	})
	Attribute("model", String, "The model that generated the message")
	Attribute("tool_call_id", String, "The tool call ID of the message")
	Attribute("tool_calls", String, "The tool calls in the message as a JSON blob")
	Attribute("finish_reason", String, "The finish reason of the message")
	Attribute("prompt_id", String, "The agent prompt/turn ID associated with this message, when available.")
	Attribute("user_id", String, "The ID of the user who created the message")
	Attribute("external_user_id", String, "The ID of the external user who created the message")
	Attribute("created_at", String, func() {
		Description("When the message was created.")
		Format(FormatDateTime)
	})
	Attribute("generation", Int, "Conversation generation — bumps on compaction or edit divergence")

	Required("id", "role", "model", "created_at", "generation")
})

var AgentUsage = Type("AgentUsage", func() {
	Attribute("type", String, "The agent usage payload discriminator.", func() {
		Enum("claude")
	})
	Attribute("claude", ClaudeAgentUsage, "Claude Code usage details.")

	Required("type")
})

var ClaudeAgentUsage = Type("ClaudeAgentUsage", func() {
	Attribute("turns", ArrayOf(ClaudeTurnUsage), "Per-prompt Claude usage turns ordered by start time.")
	Attribute("tools", ArrayOf(ClaudeToolUsage), "Per-tool Claude usage keyed by tool_use_id.")

	Required("turns", "tools")
})

var ClaudeTurnUsage = Type("ClaudeTurnUsage", func() {
	Attribute("prompt_id", String, "Claude prompt.id that correlates events for one user turn.")
	Attribute("start_time_unix_nano", String, "Earliest OTEL log timestamp in this turn, as Unix nanoseconds.")
	Attribute("end_time_unix_nano", String, "Latest OTEL log timestamp in this turn, as Unix nanoseconds.")
	Attribute("request_count", Int64, "Number of Claude API request events in this turn.")
	Attribute("input_tokens", Int64, "Input tokens used by this turn.")
	Attribute("output_tokens", Int64, "Output tokens used by this turn.")
	Attribute("cache_read_tokens", Int64, "Cache read tokens used by this turn.")
	Attribute("cache_creation_tokens", Int64, "Cache creation tokens used by this turn.")
	Attribute("total_tokens", Int64, "Total tokens used by this turn.")
	Attribute("cost_usd", Float64, "Total USD cost for this turn.")
	Attribute("cost_micros", Int64, "Total cost for this turn in micros of a USD.")
	Attribute("models", ArrayOf(String), "Distinct model names used by this turn.")
	Attribute("query_sources", ArrayOf(String), "Distinct Claude query sources used by this turn.")

	Required("prompt_id", "start_time_unix_nano", "end_time_unix_nano", "request_count", "input_tokens", "output_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens", "cost_usd", "cost_micros", "models", "query_sources")
})

var ClaudeToolUsage = Type("ClaudeToolUsage", func() {
	Attribute("tool_use_id", String, "Claude tool_use_id that correlates the tool call and result.")
	Attribute("prompt_id", String, "Claude prompt.id for the turn that used this tool.")
	Attribute("tool_name", String, "Tool name reported by Claude Code.")
	Attribute("input_size_bytes", Int64, "Serialized tool input size in bytes.")
	Attribute("result_size_bytes", Int64, "Serialized tool result size in bytes.")

	Required("tool_use_id", "prompt_id", "tool_name", "input_size_bytes", "result_size_bytes")
})
