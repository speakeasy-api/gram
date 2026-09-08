package usersessionclients

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("userSessionClients", func() {
	Description("Operator visibility into MCP clients registered against a user-session issuer (user_session_clients). Read + revoke. Registrations are written by /mcp/{slug}/register (RFC 7591 DCR) or resolved from a Client ID Metadata Document on /mcp/{slug}/authorize (CIMD); client_id_metadata_uri distinguishes the two.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("listUserSessionClients", func() {
		Description("List user_session_clients in the caller's project.")

		Payload(func() {
			Attribute("user_session_issuer_id", String, "Filter to clients registered with this issuer.", func() {
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

		Result(ListUserSessionClientsResult)

		HTTP(func() {
			GET("/rpc/userSessionClients.list")
			Param("user_session_issuer_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listUserSessionClients")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionClients"}`)
	})

	Method("getUserSessionClient", func() {
		Description("Get a user_session_client by id.")

		Payload(func() {
			Attribute("id", String, "The user_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UserSessionClient)

		HTTP(func() {
			GET("/rpc/userSessionClients.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getUserSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionClient"}`)
	})

	Method("refreshUserSessionClientCIMD", func() {
		Description("Force a CIMD client's metadata document to be re-read. Purges the stored cache state (expiry + ETag) before re-fetching, so the read is unconditional: a host answering 304 Not Modified cannot re-confirm the copy the operator is discarding. Rejected for DCR clients. Deliberately bypasses the document cache, so it carries a per-client cooldown: a refresh shortly after the last successful read returns rate_limit_exceeded.")

		Payload(func() {
			Attribute("id", String, "The user_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UserSessionClient)

		HTTP(func() {
			POST("/rpc/userSessionClients.refreshCIMD")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "refreshUserSessionClientCIMD")
		Meta("openapi:extension:x-speakeasy-name-override", "refreshCIMD")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RefreshUserSessionClientCIMD"}`)
	})

	Method("revokeUserSessionClient", func() {
		Description("Soft-delete a user_session_client and cascade to the user_sessions it issued. A DCR client stays revoked. A CIMD client does not: its identity is the metadata document URL, so the next /authorize re-resolves that document and registers a fresh row. Durably blocking a CIMD client is admission control's job, not revocation's.")

		Payload(func() {
			Attribute("id", String, "The user_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/userSessionClients.revoke")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeUserSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "revoke")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeUserSessionClient"}`)
	})
})

var UserSessionClient = Type("UserSessionClient", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP client registered against a user-session issuer. client_secret_hash is never returned.")

	Attribute("id", String, "The user_session_client id.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_id", String, "The owning user_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("client_id", String, "The client_id. Minted by Gram for a DCR registration; for a CIMD client it is the metadata document URL and equals client_id_metadata_uri.")
	Attribute("client_id_metadata_uri", String, "When set, the client was resolved from a Client ID Metadata Document (CIMD) hosted at this URL rather than registered via RFC 7591 DCR. Null for DCR clients. The URL is the client's identity, so its origin -- not client_name, which the client chooses -- is the trustworthy label.")
	Attribute("client_id_metadata_fetched_at", String, "When the metadata document was last successfully read. A 304 revalidation counts as a read, so this is not necessarily when the body was last fetched. Null for DCR clients.", func() {
		Format(FormatDateTime)
	})
	Attribute("client_id_metadata_cache_expires_at", String, "When the cached metadata document lapses and the next /authorize revalidates it against the host. Null for DCR clients, and null after a refresh purge until the re-read lands.", func() {
		Format(FormatDateTime)
	})
	Attribute("client_id_metadata_etag", String, "ETag the document host returned on the last full read; sent as If-None-Match when revalidating. Null when the host offers no validator, and null for DCR clients.")
	Attribute("client_name", String, "Display name the client supplied at registration, or the client_name extracted from its metadata document. Client-controlled and unverified; do not present it as an identity.")
	Attribute("credential_kind", String, "What the client must present to authenticate: 'public' (nothing), 'secret' (a client secret), 'key' (an assertion signed by its published key), or 'misconfigured'. Derived by the same rule the token endpoint enforces, so it reads what the client will actually be held to rather than what it declared. 'misconfigured' means the registration contradicts itself and the client cannot authenticate at all.", func() {
		Enum("public", "secret", "key", "misconfigured")
	})
	Attribute("token_endpoint_auth_method", String, "The raw RFC 7591 token_endpoint_auth_method the client declared, for debugging against the spec. Null for a client registered before the value was recorded, which is not the same as declaring 'none' -- credential_kind resolves both cases and is what should be displayed.")
	Attribute("redirect_uris", ArrayOf(String), "Validated on every /authorize.")
	Attribute("client_id_issued_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("client_secret_expires_at", String, "Null when the secret does not expire.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("active_session_count", Int, "How many live user_sessions this client currently holds. Counted the same way the sessions listing's active filter counts: not revoked, and the refresh token has not expired.")

	Required("id", "user_session_issuer_id", "client_id", "client_name", "credential_kind", "redirect_uris", "client_id_issued_at", "created_at", "updated_at", "active_session_count")
})

var ListUserSessionClientsResult = Type("ListUserSessionClientsResult", func() {
	Description("Result type for listing user_session_clients.")

	Attribute("items", ArrayOf(UserSessionClient))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})
