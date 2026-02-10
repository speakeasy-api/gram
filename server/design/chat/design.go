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
		})

		Result(ListChatsResult)

		HTTP(func() {
			GET("/rpc/chat.list")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listChats")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListChats"}`)
	})

	Method("loadChat", func() {
		Description("Load a chat by its ID")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
			Attribute("id", String, "The ID of the chat")
			Required("id")
		})

		Result(Chat)

		HTTP(func() {
			GET("/rpc/chat.load")
			Param("id")
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
		Description("Load a chat by its ID")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()
		})

		Result(func() {
			Attribute("credits_used", Float64, "The number of credits remaining")
			Attribute("monthly_credits", Int, "The number of monthly credits")
			Required("credits_used", "monthly_credits")
		})

		HTTP(func() {
			GET("/rpc/chat.creditUsage")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "creditUsage")
		Meta("openapi:extension:x-speakeasy-name-override", "creditUsage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetCreditUsage"}`)
	})

	Method("listChatsWithResolutions", func() {
		Description("List all chats for a project with their resolutions")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ChatSessionsTokenPayload()

			Attribute("external_user_id", String, "Filter by external user ID")
			Attribute("resolution_status", String, "Filter by resolution status")
			Attribute("limit", Int, "Number of results per page", func() {
				Default(50)
				Minimum(1)
				Maximum(100)
			})
			Attribute("offset", Int, "Pagination offset", func() {
				Default(0)
				Minimum(0)
			})
		})

		Result(ListChatsWithResolutionsResult)

		HTTP(func() {
			GET("/rpc/chat.listChatsWithResolutions")
			Param("external_user_id")
			Param("resolution_status")
			Param("limit")
			Param("offset")
			security.SessionHeader()
			security.ProjectHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listChatsWithResolutions")
		Meta("openapi:extension:x-speakeasy-name-override", "listChatsWithResolutions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListChatsWithResolutions", "type": "query"}`)
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
	Required("chats")
})

var ChatOverview = Type("ChatOverview", func() {
	Attribute("id", String, "The ID of the chat")
	Attribute("title", String, "The title of the chat")
	Attribute("user_id", String, "The ID of the user who created the chat")
	Attribute("external_user_id", String, "The ID of the external user who created the chat")
	Attribute("num_messages", Int, "The number of messages in the chat")
	Attribute("created_at", String, func() {
		Description("When the chat was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the chat was last updated.")
		Format(FormatDateTime)
	})

	Required("id", "title", "num_messages", "created_at", "updated_at")
})

var Chat = Type("Chat", func() {
	Extend(ChatOverview)
	Attribute("messages", ArrayOf(ChatMessage), "The list of messages in the chat")

	Required("messages")
})

var ChatMessage = Type("ChatMessage", func() {
	Attribute("id", String, "The ID of the message")
	Attribute("role", String, "The role of the message")
	Attribute("content", String, "The content of the message", func() {
		Meta("struct:field:type", "json.RawMessage", "encoding/json")
	})
	Attribute("model", String, "The model that generated the message")
	Attribute("tool_call_id", String, "The tool call ID of the message")
	Attribute("tool_calls", String, "The tool calls in the message as a JSON blob")
	Attribute("finish_reason", String, "The finish reason of the message")
	Attribute("user_id", String, "The ID of the user who created the message")
	Attribute("external_user_id", String, "The ID of the external user who created the message")
	Attribute("created_at", String, func() {
		Description("When the message was created.")
		Format(FormatDateTime)
	})

	Required("id", "role", "model", "created_at")
})

var ChatResolution = Type("ChatResolution", func() {
	Description("Resolution information for a chat")

	Attribute("id", String, "Resolution ID", func() {
		Format(FormatUUID)
	})
	Attribute("user_goal", String, "User's intended goal")
	Attribute("resolution", String, "Resolution status")
	Attribute("resolution_notes", String, "Notes about the resolution")
	Attribute("score", Int, "Score 0-100")
	Attribute("created_at", String, "When resolution was created", func() {
		Format(FormatDateTime)
	})
	Attribute("message_ids", ArrayOf(String), "Message IDs associated with this resolution", func() {
		Example([]string{"abc-123", "def-456"})
	})

	Required("id", "user_goal", "resolution", "resolution_notes", "score", "created_at", "message_ids")
})

var ChatOverviewWithResolutions = Type("ChatOverviewWithResolutions", func() {
	Description("Chat overview with embedded resolution data")

	Extend(ChatOverview)

	Attribute("resolutions", ArrayOf(ChatResolution), "List of resolutions for this chat")

	Required("resolutions")
})

var ListChatsWithResolutionsResult = Type("ListChatsWithResolutionsResult", func() {
	Description("Result of listing chats with resolutions")

	Attribute("chats", ArrayOf(ChatOverviewWithResolutions), "List of chats with resolutions")
	Attribute("total", Int, "Total number of chats (before pagination)")

	Required("chats", "total")
})
