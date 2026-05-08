package usersessionclients

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("userSessionClients", func() {
	Description("Operator visibility into DCR'd MCP clients (user_session_clients). Read + revoke; registrations are written by /mcp/{slug}/register.")
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

	Method("revokeUserSessionClient", func() {
		Description("Soft-delete a user_session_client. Future tokens minted for this client_id are rejected; existing live user_sessions keep working until they hit expires_at.")

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

	Description("A user_session_client (DCR'd MCP client). client_secret_hash is never returned.")

	Attribute("id", String, "The user_session_client id.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_id", String, "The owning user_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("client_id", String, "DCR-issued client_id.")
	Attribute("client_name", String, "Display name from the registration request.")
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

	Required("id", "user_session_issuer_id", "client_id", "client_name", "redirect_uris", "client_id_issued_at", "created_at", "updated_at")
})

var ListUserSessionClientsResult = Type("ListUserSessionClientsResult", func() {
	Description("Result type for listing user_session_clients.")

	Attribute("items", ArrayOf(UserSessionClient))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})
