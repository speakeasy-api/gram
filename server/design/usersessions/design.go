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
		Description("Mint a user_session on behalf of the authenticated dashboard user, bound to an issuer-gated audience: a toolset (/mcp), a remote MCP server (/x/mcp), or a meta MCP server (/mcp). Exactly one of toolset_id, mcp_server_id, or meta_mcp_server_id must be provided. The minted JWT matches the shape /token would emit after a successful OAuth dance, so the runtime MCP gateway validates it through the same path as a real MCP client's bearer.")

		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Attribute("toolset_id", String, "Bind the JWT to this toolset's /mcp/{slug} audience. Mutually exclusive with the other targets; exactly one must be set. Must be issuer-gated and live in the caller's project.", func() {
				Format(FormatUUID)
			})
			Attribute("mcp_server_id", String, "Bind the JWT to this remote MCP server's user_session_issuer audience (the /x/mcp convention, since remote servers have no toolset). Mutually exclusive with the other targets; exactly one must be set. Must be issuer-gated and live in the caller's project.", func() {
				Format(FormatUUID)
			})
			Attribute("meta_mcp_server_id", String, "Bind the JWT to this meta MCP server's user_session_issuer audience. Mutually exclusive with the other targets; exactly one must be set. Must be issuer-gated and live in the caller's project.", func() {
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
	Attribute("user_session_client_id", String, "The user_session_client this session was issued through. Null for sessions with no bound client. Unlike client_name, this identifies the registration unambiguously, so it is what a per-client drill-down should match on.", func() {
		Format(FormatUUID)
	})
	Attribute("client_name", String, "Name of the MCP client that established the session, if known. Client-controlled and unverified; do not present it as an identity.")
	Attribute("client_id_metadata_uri", String, "Set when the client that established this session was resolved from a Client ID Metadata Document (CIMD) hosted at this URL, rather than registered via RFC 7591 DCR. Null for DCR clients and for sessions with no bound client.")
	Attribute("subject_type", String, "Subject kind: 'user', 'apikey', or 'anonymous'.")
	Attribute("subject_display_name", String, "Resolved human-readable name of the subject, if known.")
	Attribute("subject_photo_url", String, "Avatar URL for the subject when it resolves to a Gram user with one. Null for API key and anonymous subjects, and for users who have no photo.")
	Attribute("revoked_at", String, "When the session was revoked, if it has been.", func() {
		Format(FormatDateTime)
	})
	Attribute("last_used_at", String, "When this session last carried an MCP request. Recorded on the request path and coalesced to a five-minute resolution, so treat it as accurate to within that. Null means the session has not been used since the column was introduced — unknown, not never.", func() {
		Format(FormatDateTime)
	})
	Attribute("upstreams", ArrayOf(UserSessionUpstream), "The upstream providers Gram holds tokens for on this session's subject, through the same issuer. Empty when the session reaches only Gram-native tools. A session can have several: an issuer may have more than one remote_session_client attached.")

	Required("id", "user_session_issuer_id", "subject_urn", "jti", "refresh_expires_at", "expires_at", "created_at", "updated_at", "issuer_slug", "subject_type", "upstreams")
})

// UserSessionUpstream is the outbound leg of a brokered connection. A
// user_session says an agent can reach Gram; this says what Gram can reach on
// that subject's behalf. The two are joined on (subject_urn,
// user_session_issuer_id), which both tables carry.
var UserSessionUpstream = Type("UserSessionUpstream", func() {
	Meta("struct:pkg:path", "types")

	Description("An upstream remote_session held for the same subject and issuer as the user_session that carries it. Token material is never returned.")

	Attribute("remote_session_id", String, "The remote_session id. Target for revoke and force-refresh.", func() {
		Format(FormatUUID)
	})
	Attribute("remote_session_client_id", String, "The remote_session_client the session was minted against.", func() {
		Format(FormatUUID)
	})
	Attribute("remote_session_issuer_id", String, "The remote_session_issuer the client belongs to.", func() {
		Format(FormatUUID)
	})
	Attribute("issuer_slug", String, "Display slug of the upstream provider, e.g. 'mcp.linear.app'.")
	Attribute("access_expires_at", String, "Upstream access-token expiry. Null when the upstream issued a non-expiring token.", func() {
		Format(FormatDateTime)
	})
	Attribute("refresh_expires_at", String, "Upstream refresh-token expiry. Null when the session holds no refresh token or the upstream issued a non-expiring one.", func() {
		Format(FormatDateTime)
	})
	Attribute("authorization_expires_at", String, "Absolute upstream authorization deadline. Unlike refresh_expires_at, exchanging a token does not extend this.", func() {
		Format(FormatDateTime)
	})
	Attribute("has_refresh_token", Boolean, "Whether a refresh grant is held. Gates 'refresh now'; refresh_expires_at is insufficient because an upstream may issue a non-expiring refresh token.")
	Attribute("auto_refresh", Boolean, "Whether the subject opted this connection into automated keepalive.")
	Attribute("last_used_at", String, "When this upstream token was last spent on a proxied call. Same five-minute resolution as the inbound leg, so the two are directly comparable.", func() {
		Format(FormatDateTime)
	})
	Attribute("scopes", ArrayOf(String), "Scopes held by this upstream session.")

	Required("remote_session_id", "remote_session_client_id", "remote_session_issuer_id", "issuer_slug", "has_refresh_token", "auto_refresh", "scopes")
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
