package chatsessions

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("chatSessions", func() {
	Description("Manages chat session tokens for client-side authentication")

	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("chat")
	})

	shared.DeclareErrorResponses()

	Method("create", func() {
		Description("Creates a new chat session token")

		Payload(func() {
			Attribute("user_identifier", String, "Optional free-form user identifier")
			Attribute("expires_after", Int, "Token expiration in seconds (max / default 3600)", func() {
				Minimum(1)
				Maximum(3600)
				Default(3600)
			})
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("client_token", String, "JWT token for chat session")
			Attribute("expires_after", Int, "Token expiration in seconds")
			Attribute("user_identifier", String, "User identifier if provided")
			Attribute("status", String, "Session status")
			Required("client_token", "expires_after", "status")
		})

		HTTP(func() {
			POST("/rpc/chatSessions.create")
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
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/chatSessions.revoke")
			Param("token")
			security.ByKeyHeader()
			security.ProjectHeader()

			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "revokeChatSession")
		Meta("openapi:extension:x-speakeasy-name-override", "revoke")
	})
})
