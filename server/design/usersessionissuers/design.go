package usersessionissuers

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("userSessionIssuers", func() {
	Description("Manage user_session_issuer records — Gram-side authorization-server configuration that issues user sessions for an MCP server.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createUserSessionIssuer", func() {
		Description("Create a new user_session_issuer.")

		Payload(func() {
			Extend(CreateUserSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UserSessionIssuer)

		HTTP(func() {
			POST("/rpc/userSessionIssuers.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateUserSessionIssuer"}`)
	})

	Method("updateUserSessionIssuer", func() {
		Description("Update fields on an existing user_session_issuer.")

		Payload(func() {
			Extend(UpdateUserSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UserSessionIssuer)

		HTTP(func() {
			POST("/rpc/userSessionIssuers.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateUserSessionIssuer"}`)
	})

	Method("listUserSessionIssuers", func() {
		Description("List user_session_issuers in the caller's project.")

		Payload(func() {
			Attribute("cursor", String, "Pagination cursor: id of the last item from the previous page.", func() {
				Format(FormatUUID)
			})
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListUserSessionIssuersResult)

		HTTP(func() {
			GET("/rpc/userSessionIssuers.list")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listUserSessionIssuers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionIssuers"}`)
	})

	Method("getUserSessionIssuer", func() {
		Description("Get a user_session_issuer by id or by slug. Provide exactly one.")

		Payload(func() {
			Attribute("id", String, "The user_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "The user_session_issuer slug.")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UserSessionIssuer)

		HTTP(func() {
			GET("/rpc/userSessionIssuers.get")
			Param("id")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionIssuer"}`)
	})

	Method("deleteUserSessionIssuer", func() {
		Description("Soft-delete a user_session_issuer. Cascades to dependent user_sessions, user_session_consents, and remote_session_clients.")

		Payload(func() {
			Attribute("id", String, "The user_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/userSessionIssuers.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteUserSessionIssuer"}`)
	})
})

var CreateUserSessionIssuerForm = Type("CreateUserSessionIssuerForm", func() {
	Description("Form for creating a user_session_issuer.")

	Attribute("slug", String, "Project-unique slug.")
	Attribute("authn_challenge_mode", String, "How multi-remote authn challenges are presented: chain | interactive.", func() {
		Enum("chain", "interactive")
	})
	Attribute("session_duration_hours", Int, "Issued user session lifetime, in hours.")

	Required("slug", "authn_challenge_mode", "session_duration_hours")
})

var UpdateUserSessionIssuerForm = Type("UpdateUserSessionIssuerForm", func() {
	Description("Form for updating a user_session_issuer. All non-id fields are optional patches.")

	Attribute("id", String, "The user_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("slug", String, "Rename the slug.")
	Attribute("authn_challenge_mode", String, "chain | interactive.", func() {
		Enum("chain", "interactive")
	})
	Attribute("session_duration_hours", Int, "Issued user session lifetime, in hours.")

	Required("id")
})

var UserSessionIssuer = Type("UserSessionIssuer", func() {
	Meta("struct:pkg:path", "types")

	Description("A user_session_issuer record.")

	Attribute("id", String, "The user_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The owning project id.", func() {
		Format(FormatUUID)
	})
	Attribute("slug", String, "Project-unique slug.")
	Attribute("authn_challenge_mode", String, "chain | interactive.")
	Attribute("session_duration_hours", Int, "Issued user session lifetime, in hours.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "project_id", "slug", "authn_challenge_mode", "session_duration_hours", "created_at", "updated_at")
})

var ListUserSessionIssuersResult = Type("ListUserSessionIssuersResult", func() {
	Description("Result type for listing user_session_issuers.")

	Attribute("items", ArrayOf(UserSessionIssuer))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})
