package usersessions

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("userSessions", func() {
	Description("Operator visibility into issued user_sessions. List + revoke; sessions are written by /mcp/{slug}/token.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("listUserSessions", func() {
		Description("List issued user_sessions in the caller's project. refresh_token_hash is never returned.")

		Payload(func() {
			Attribute("subject_urn", String, "Exact-match filter on subject URN.")
			Attribute("user_session_issuer_id", String, "Filter by user_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Attribute("cursor", String, "Pagination cursor: id of the last item from the previous page.", func() {
				Format(FormatUUID)
			})
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListUserSessionsResult)

		HTTP(func() {
			GET("/rpc/userSessions.list")
			Param("subject_urn")
			Param("user_session_issuer_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listUserSessions")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessions"}`)
	})

	Method("revokeUserSession", func() {
		Description("Push the session's jti into the revocation cache and soft-delete the row.")

		Payload(func() {
			Attribute("id", String, "The user_session id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/userSessions.revoke")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeUserSession")
		Meta("openapi:extension:x-speakeasy-name-override", "revoke")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeUserSession"}`)
	})
})

var UserSession = Type("UserSession", func() {
	Meta("struct:pkg:path", "types")

	Description("An issued user_session record. refresh_token_hash is never returned.")

	Attribute("id", String, "The user_session id.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_id", String, "The issuing user_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("subject_urn", String, "The session's subject URN (user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>).")
	Attribute("jti", String, "Current access-token JTI; used by the revocation path.")
	Attribute("refresh_expires_at", String, "Next refresh deadline.", func() {
		Format(FormatDateTime)
	})
	Attribute("expires_at", String, "Terminal session expiry; ceiling on refresh_expires_at.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "user_session_issuer_id", "subject_urn", "jti", "refresh_expires_at", "expires_at", "created_at", "updated_at")
})

var ListUserSessionsResult = Type("ListUserSessionsResult", func() {
	Description("Result type for listing user_sessions.")

	Attribute("items", ArrayOf(UserSession))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})
