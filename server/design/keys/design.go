package keys

import (
	"github.com/speakeasy-api/gram/design/sessions"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("keys", func() {
	Description("Managing system api keys.")
	Security(sessions.Session)

	Method("createKey", func() {
		Description("Create a new api key")

		Payload(func() {
			Extend(CreateKeyForm)
			sessions.SessionPayload()
		})

		Result(KeyModel)

		HTTP(func() {
			POST("/rpc/keys.create")
			sessions.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateAPIKey"}`)
	})

	Method("listKeys", func() {
		Description("List all api keys for an organization")

		Payload(func() {
			sessions.SessionPayload()
		})

		Result(ListKeysResult)

		HTTP(func() {
			GET("/rpc/keys.list")
			sessions.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListAPIKeys"}`)
	})

	Method("revokeKey", func() {
		Description("Revoke a api key")

		Payload(func() {
			Attribute("id", String, "The ID of the key to revoke")
			Required("id")
			sessions.SessionPayload()
		})

		HTTP(func() {
			Param("id")
			POST("/rpc/keys.revoke/{id}")
			sessions.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeAPIKeys"}`)
	})
})

var CreateKeyForm = Type("CreateKeyForm", func() {
	Required("name", "project_id")

	Attribute("name", String, "The name of the key")
	Required("name")
})

var ListKeysResult = Type("ListKeysResult", func() {
	Attribute("keys", ArrayOf(KeyModel))
	Required("keys")
})

var KeyModel = Type("Key", func() {
	Required("id", "organization_id", "created_by_user_id", "name", "token", "scopes", "created_at", "updated_at")

	Attribute("id", String, "The ID of the key")
	Attribute("organization_id", String, "The organization ID this key belongs to")
	Attribute("project_id", String, "The optional project ID this key is scoped to")
	Attribute("created_by_user_id", String, "The ID of the user who created this key")
	Attribute("name", String, "The name of the key")
	Attribute("token", String, "The API token value")
	Attribute("scopes", ArrayOf(String), "List of permission scopes for this key")
	Attribute("created_at", String, func() {
		Description("The creation date of the key.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the key was last updated.")
		Format(FormatDateTime)
	})
})
