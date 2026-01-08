package chatsessions

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("chatSessions", func() {
	Description("Manages chat session tokens for client-side authentication")

	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("chat")
	})

	shared.DeclareErrorResponses()

	Method("create", func() {
		Description("Creates a new chat session token")

		Payload(func() {
			Required("embed_origin")
			Attribute("user_identifier", String, "Optional free-form user identifier")
			Attribute("embed_origin", String, "The origin from which the token will be used")
			Attribute("expires_after", Int, "Token expiration in seconds (max / default 3600)", func() {
				Minimum(1)
				Maximum(3600)
				Default(3600)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("client_token", String, "JWT token for chat session")
			Attribute("embed_origin", String, "The origin from which the token will be used")
			Attribute("expires_after", Int, "Token expiration in seconds")
			Attribute("user_identifier", String, "User identifier if provided")
			Attribute("status", String, "Session status")
			Required("embed_origin", "client_token", "expires_after", "status")
		})

		HTTP(func() {
			POST("/rpc/chatSessions.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()

			Response(StatusOK)
		})

		Meta("openapi:operationId", "createChatSession")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
	})

	Method("revoke", func() {
		Description("Revokes an existing chat session token")

		Payload(func() {
			Attribute("token", String, "The chat session token to revoke")
			Required("token")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/chatSessions.revoke")
			Param("token")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()

			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "revokeChatSession")
		Meta("openapi:extension:x-speakeasy-name-override", "revoke")
	})
})
