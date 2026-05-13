package remotesessions

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("remoteSessions", func() {
	Description("Operator visibility into remote_sessions Gram is holding on a principal's behalf. Read + revoke; sessions are written by /mcp/{slug}/remote_login_callback and the silent-refresh path. access_token_encrypted and refresh_token_encrypted are never returned.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("listRemoteSessions", func() {
		Description("List remote_sessions in the caller's project. access_token_encrypted and refresh_token_encrypted are never returned — only metadata (access_expires_at, refresh_expires_at, scopes).")

		Payload(func() {
			Attribute("subject_urn", String, "Exact-match filter on subject URN.")
			Attribute("remote_session_client_id", String, "Filter by remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListRemoteSessionsResult)

		HTTP(func() {
			GET("/rpc/remoteSessions.list")
			Param("subject_urn")
			Param("remote_session_client_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listRemoteSessions")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoteSessions"}`)
	})

	Method("revokeRemoteSession", func() {
		Description("Drop a remote_session row. The next /mcp call by that principal triggers a fresh authn challenge.")

		Payload(func() {
			Attribute("id", String, "The remote_session id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/remoteSessions.revoke")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeRemoteSession")
		Meta("openapi:extension:x-speakeasy-name-override", "revoke")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeRemoteSession"}`)
	})
})

var RemoteSession = Type("RemoteSession", func() {
	Meta("struct:pkg:path", "types")

	Description("A remote_session record — Gram's upstream OAuth session for a (principal, remote_session_client) pair. access_token_encrypted and refresh_token_encrypted are never returned.")

	Attribute("id", String, "The remote_session id.", func() {
		Format(FormatUUID)
	})
	Attribute("subject_urn", String, "The session's subject URN (user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>).")
	Attribute("user_session_issuer_id", String, "The user_session_issuer this session is bound to.", func() {
		Format(FormatUUID)
	})
	Attribute("remote_session_client_id", String, "The remote_session_client this session was minted against.", func() {
		Format(FormatUUID)
	})
	Attribute("access_expires_at", String, "Upstream access-token expiry. Independent of refresh_expires_at.", func() {
		Format(FormatDateTime)
	})
	Attribute("refresh_expires_at", String, "Upstream refresh-token expiry. Null when the session has no refresh token.", func() {
		Format(FormatDateTime)
	})
	Attribute("scopes", ArrayOf(String), "Scopes held by this session.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "subject_urn", "user_session_issuer_id", "remote_session_client_id", "access_expires_at", "scopes", "created_at", "updated_at")
})

var ListRemoteSessionsResult = Type("ListRemoteSessionsResult", func() {
	Description("Result type for listing remote_sessions.")

	Attribute("items", ArrayOf(RemoteSession))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})
