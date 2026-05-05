package usersessionconsents

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("userSessionConsents", func() {
	Description("Operator visibility into user_session_consents — persistent consent records per (subject, user_session_client). List + revoke.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("listUserSessionConsents", func() {
		Description("List consent records for the caller's project.")

		Payload(func() {
			Attribute("subject_urn", String, "Filter by subject URN.")
			Attribute("user_session_client_id", String, "Filter by user_session_client id.", func() {
				Format(FormatUUID)
			})
			Attribute("user_session_issuer_id", String, "Filter by user_session_issuer id (joins through user_session_clients).", func() {
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

		Result(ListUserSessionConsentsResult)

		HTTP(func() {
			GET("/rpc/userSessionConsents.list")
			Param("subject_urn")
			Param("user_session_client_id")
			Param("user_session_issuer_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listUserSessionConsents")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionConsents"}`)
	})

	Method("revokeUserSessionConsent", func() {
		Description("Withdraw consent. Subsequent authorization requests for matching (subject, user_session_client) pairs re-prompt.")

		Payload(func() {
			Attribute("id", String, "The user_session_consent id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/userSessionConsents.revoke")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeUserSessionConsent")
		Meta("openapi:extension:x-speakeasy-name-override", "revoke")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeUserSessionConsent"}`)
	})
})

var UserSessionConsent = Type("UserSessionConsent", func() {
	Meta("struct:pkg:path", "types")

	Description("A user_session_consent record. Per-client (not per-issuer) consent.")

	Attribute("id", String, "The user_session_consent id.", func() {
		Format(FormatUUID)
	})
	Attribute("subject_urn", String, "The consenting subject URN (user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>).")
	Attribute("user_session_client_id", String, "The user_session_client this consent binds to.", func() {
		Format(FormatUUID)
	})
	Attribute("remote_set_hash", String, "SHA-256 of the sorted list of remote_session_issuer ids on the client's owning issuer at consent time.")
	Attribute("consented_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "subject_urn", "user_session_client_id", "remote_set_hash", "consented_at", "created_at", "updated_at")
})

var ListUserSessionConsentsResult = Type("ListUserSessionConsentsResult", func() {
	Description("Result type for listing user_session_consents.")

	Attribute("items", ArrayOf(UserSessionConsent))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})
