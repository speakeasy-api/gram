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
			Attribute("status", String, "Filter by session status.", func() {
				Enum("active", "expired", "revoked", "all")
			})
			Attribute("client_id", String, "Filter by the connecting client id.", func() {
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
			Param("status")
			Param("client_id")
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

	Method("listFacets", func() {
		Description("List available user session facet values (clients, users, servers) in the caller's project.")
		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(ListUserSessionFacetsResult)
		HTTP(func() {
			GET("/rpc/userSessions.listFacets")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "listUserSessionFacets")
		Meta("openapi:extension:x-speakeasy-name-override", "listFacets")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionFacets"}`)
	})

	Method("mintUserSession", func() {
		Description("Mint a user_session on behalf of the authenticated dashboard user, bound to an issuer-gated audience: either a toolset (/mcp) or a remote MCP server (/x/mcp). Exactly one of toolset_id or mcp_server_id must be provided. The minted JWT matches the shape /token would emit after a successful OAuth dance, so the runtime MCP gateway validates it through the same path as a real MCP client's bearer.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Attribute("toolset_id", String, "Bind the JWT to this toolset's /mcp/{slug} audience. Mutually exclusive with mcp_server_id; exactly one must be set. Must be issuer-gated and live in the caller's project.", func() {
				Format(FormatUUID)
			})
			Attribute("mcp_server_id", String, "Bind the JWT to this remote MCP server's user_session_issuer audience (the /x/mcp convention, since remote servers have no toolset). Mutually exclusive with toolset_id; exactly one must be set. Must be issuer-gated and live in the caller's project.", func() {
				Format(FormatUUID)
			})
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("access_token", String, "The minted user-session JWT. Send as `Authorization: Bearer` on MCP requests to the bound /mcp/{slug} (or /x/mcp/{slug}) surface.")
			Attribute("expires_in", Int, "Lifetime of the access token in seconds.")
			Required("access_token", "expires_in")
		})

		HTTP(func() {
			POST("/rpc/userSessions.mint")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "mintUserSession")
		Meta("openapi:extension:x-speakeasy-name-override", "mint")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "MintUserSession"}`)
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
			Param("id")
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
	Attribute("issuer_slug", String, "Slug of the user_session_issuer that gated this session.")
	Attribute("client_name", String, "Name of the MCP client that established the session, if known.")
	Attribute("subject_type", String, "Subject kind: 'user', 'apikey', or 'anonymous'.")
	Attribute("subject_display_name", String, "Resolved human-readable name of the subject, if known.")
	Attribute("revoked_at", String, "When the session was revoked, if it has been.", func() {
		Format(FormatDateTime)
	})

	Required("id", "user_session_issuer_id", "subject_urn", "jti", "refresh_expires_at", "expires_at", "created_at", "updated_at", "issuer_slug", "subject_type")
})

var ListUserSessionsResult = Type("ListUserSessionsResult", func() {
	Description("Result type for listing user_sessions.")

	Attribute("items", ArrayOf(UserSession))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})

var UserSessionFacetOption = Type("UserSessionFacetOption", func() {
	Attribute("value", String, "The facet value used for filtering.")
	Attribute("display_name", String, "The label shown for the facet value.")
	Attribute("count", Int64, "Number of sessions for this facet value.")
	Required("value", "display_name", "count")
})

var ListUserSessionFacetsResult = Type("ListUserSessionFacetsResult", func() {
	Attribute("clients", ArrayOf(UserSessionFacetOption), "Connecting client facets.")
	Attribute("users", ArrayOf(UserSessionFacetOption), "Subject (user) facets.")
	Attribute("servers", ArrayOf(UserSessionFacetOption), "Issuer/server facets.")
	Required("clients", "users", "servers")
})
