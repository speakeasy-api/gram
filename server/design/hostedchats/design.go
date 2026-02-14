package hostedchats

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("hostedchats", func() {
	Description("Manages hosted chat instances deployed to chat.getgram.ai.")

	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("create", func() {
		Description("Create a new hosted chat instance.")

		Security(security.ProjectSlug, security.Session)

		Payload(func() {
			Extend(CreateHostedChatForm)
			security.ProjectPayload()
			security.SessionPayload()
		})
		Result(HostedChatResult)

		HTTP(func() {
			POST("/rpc/hostedChats.create")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "createHostedChat")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateHostedChat"}`)
	})

	Method("get", func() {
		Description("Get a hosted chat instance by slug.")

		Security(security.ProjectSlug, security.Session)

		Payload(func() {
			Extend(GetHostedChatForm)
			security.ProjectPayload()
			security.SessionPayload()
		})
		Result(HostedChatResult)

		HTTP(func() {
			GET("/rpc/hostedChats.get")
			security.SessionHeader()
			security.ProjectHeader()
			Param("slug")
		})

		Meta("openapi:operationId", "getHostedChat")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "HostedChat"}`)
	})

	Method("list", func() {
		Description("List hosted chat instances for a project.")

		Security(security.ProjectSlug, security.Session)

		Payload(func() {
			security.ProjectPayload()
			security.SessionPayload()
		})
		Result(ListHostedChatsResult)

		HTTP(func() {
			GET("/rpc/hostedChats.list")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listHostedChats")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListHostedChats"}`)
	})

	Method("update", func() {
		Description("Update a hosted chat instance.")

		Security(security.ProjectSlug, security.Session)

		Payload(func() {
			Extend(UpdateHostedChatForm)
			security.ProjectPayload()
			security.SessionPayload()
		})
		Result(HostedChatResult)

		HTTP(func() {
			POST("/rpc/hostedChats.update")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "updateHostedChat")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateHostedChat"}`)
	})

	Method("delete", func() {
		Description("Delete a hosted chat instance.")

		Security(security.ProjectSlug, security.Session)

		Payload(func() {
			Required("id")
			Attribute("id", String, "The ID of the hosted chat to delete", func() { Format(FormatUUID) })
			security.ProjectPayload()
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/hostedChats.delete")
			security.SessionHeader()
			security.ProjectHeader()
			Param("id")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteHostedChat")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteHostedChat"}`)
	})

	Method("getPublic", func() {
		Description("Get a hosted chat instance by slug for the public chat UI. Requires session auth.")

		Payload(func() {
			Required("chat_slug")
			Attribute("chat_slug", String, "The slug of the hosted chat", func() {
				MaxLength(60)
				MinLength(1)
			})
			security.SessionPayload()
		})
		Result(HostedChatPublicResult)

		HTTP(func() {
			GET("/rpc/hostedChats.getPublic")
			security.SessionHeader()
			Param("chat_slug")
		})

		Meta("openapi:operationId", "getHostedChatPublic")
		Meta("openapi:extension:x-speakeasy-name-override", "getPublic")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "HostedChatPublic"}`)
	})
})

var HostedChat = Type("HostedChat", func() {
	Required("id", "organization_id", "project_id", "name", "slug", "theme_color_scheme", "created_at", "updated_at")

	Attribute("id", String, "The ID of the hosted chat", func() { Format(FormatUUID) })
	Attribute("organization_id", String, "The organization ID")
	Attribute("project_id", String, "The project ID", func() { Format(FormatUUID) })
	Attribute("name", String, "The display name of the hosted chat", func() { MaxLength(100) })
	Attribute("slug", String, "URL-friendly slug for the hosted chat", func() { MaxLength(60) })
	Attribute("mcp_slug", String, "The MCP server slug to connect to", func() { MaxLength(60) })
	Attribute("system_prompt", String, "Custom system prompt for the chat")
	Attribute("welcome_title", String, "Welcome screen title", func() { MaxLength(200) })
	Attribute("welcome_subtitle", String, "Welcome screen subtitle", func() { MaxLength(500) })
	Attribute("theme_color_scheme", String, "Color scheme", func() {
		Enum("light", "dark", "system")
		Default("system")
	})
	Attribute("created_at", String, func() {
		Description("Creation timestamp")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("Last update timestamp")
		Format(FormatDateTime)
	})
})

var CreateHostedChatForm = Type("CreateHostedChatForm", func() {
	Required("name")

	Attribute("name", String, "The display name of the hosted chat", func() {
		MaxLength(100)
		MinLength(1)
	})
	Attribute("slug", String, "Optional custom slug (auto-generated from name if not provided)", func() {
		MaxLength(60)
	})
	Attribute("mcp_slug", String, "The MCP server slug to connect to", func() { MaxLength(60) })
	Attribute("system_prompt", String, "Custom system prompt for the chat")
	Attribute("welcome_title", String, "Welcome screen title", func() { MaxLength(200) })
	Attribute("welcome_subtitle", String, "Welcome screen subtitle", func() { MaxLength(500) })
	Attribute("theme_color_scheme", String, "Color scheme", func() {
		Enum("light", "dark", "system")
		Default("system")
	})
})

var GetHostedChatForm = Type("GetHostedChatForm", func() {
	Required("slug")
	Attribute("slug", String, "The slug of the hosted chat to get", func() {
		MaxLength(60)
		MinLength(1)
	})
})

var UpdateHostedChatForm = Type("UpdateHostedChatForm", func() {
	Required("id")

	Attribute("id", String, "The ID of the hosted chat to update", func() { Format(FormatUUID) })
	Attribute("name", String, "The display name", func() { MaxLength(100) })
	Attribute("mcp_slug", String, "The MCP server slug to connect to", func() { MaxLength(60) })
	Attribute("system_prompt", String, "Custom system prompt")
	Attribute("welcome_title", String, "Welcome screen title", func() { MaxLength(200) })
	Attribute("welcome_subtitle", String, "Welcome screen subtitle", func() { MaxLength(500) })
	Attribute("theme_color_scheme", String, "Color scheme", func() {
		Enum("light", "dark", "system")
	})
})

var HostedChatResult = Type("HostedChatResult", func() {
	Required("hosted_chat")
	Attribute("hosted_chat", HostedChat, "The hosted chat instance")
})

var ListHostedChatsResult = Type("ListHostedChatsResult", func() {
	Required("hosted_chats")
	Attribute("hosted_chats", ArrayOf(HostedChat), "List of hosted chat instances")
})

var HostedChatPublicResult = Type("HostedChatPublicResult", func() {
	Required("hosted_chat")
	Attribute("hosted_chat", HostedChat, "The hosted chat instance")
	Attribute("project_slug", String, "The project slug for constructing MCP URL")
})
