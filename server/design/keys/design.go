package keys

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("keys", func() {
	Description("Managing system api keys.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("createKey", func() {
		Description("Create a new api key")

		Payload(func() {
			Extend(CreateKeyForm)
			security.SessionPayload()
		})

		Result(KeyModel)

		HTTP(func() {
			POST("/rpc/keys.create")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createAPIKey")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateAPIKey"}`)
	})

	Method("listKeys", func() {
		Description("List all api keys for an organization")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListKeysResult)

		HTTP(func() {
			GET("/rpc/keys.list")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listAPIKeys")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListAPIKeys"}`)
	})

	Method("revokeKey", func() {
		Description("Revoke a api key")

		Payload(func() {
			Attribute("id", String, "The ID of the key to revoke")
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			Param("id")
			DELETE("/rpc/keys.revoke")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeAPIKey")
		Meta("openapi:extension:x-speakeasy-name-override", "revokeById")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeAPIKey"}`)
	})

	Method("verifyKey", func() {
		Description("Verify an api key")

		Security(security.ByKey)

		Payload(func() {
			security.ByKeyPayload()
		})
		Result(VerifyKeyResult)

		HTTP(func() {
			GET("/rpc/keys.verify")
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "validateAPIKey")
		Meta("openapi:extension:x-speakeasy-name-override", "validate")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ValidateAPIKey"}`)
	})

})

var CreateKeyForm = Type("CreateKeyForm", func() {
	Required("name", "scopes")

	Attribute("name", String, "The name of the key")
	Attribute("scopes", ArrayOf(String), func() {
		Description("The scopes of the key that determines its permissions.")

		MinLength(1)
	})
})

var ListKeysResult = Type("ListKeysResult", func() {
	Attribute("keys", ArrayOf(KeyModel))
	Required("keys")
})

var KeyModel = Type("Key", func() {
	Required("id", "organization_id", "created_by_user_id", "name", "key_prefix", "scopes", "created_at", "updated_at")

	Attribute("id", String, "The ID of the key")
	Attribute("organization_id", String, "The organization ID this key belongs to")
	Attribute("project_id", String, "The optional project ID this key is scoped to")
	Attribute("created_by_user_id", String, "The ID of the user who created this key")
	Attribute("name", String, "The name of the key")
	Attribute("key_prefix", String, "The store prefix of the api key for recognition")
	Attribute("key", String, "The token of the api key (only returned on key creation)") // this will only be set on key creation
	Attribute("scopes", ArrayOf(String), "List of permission scopes for this key")
	Attribute("created_at", String, func() {
		Description("The creation date of the key.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the key was last updated.")
		Format(FormatDateTime)
	})
	Attribute("last_accessed_at", String, func() {
		Description("When the key was last accessed.")
		Format(FormatDateTime)
	})
})

var VerifyKeyResult = Type("ValidateKeyResult", func() {
	Required("organization", "projects", "scopes")

	Attribute("organization", ValidateKeyOrganization, "The organization the key belongs to")
	Attribute("projects", ArrayOf(ValidateKeyProject), "The projects accessible with this key")
	Attribute("scopes", ArrayOf(String), "List of permission scopes for this key")
})

var ValidateKeyOrganization = Type("ValidateKeyOrganization", func() {
	Required("id", "name", "slug")

	Attribute("id", String, "The ID of the organization")
	Attribute("name", String, "The name of the organization")
	Attribute("slug", String, "The slug of the organization")
})

var ValidateKeyProject = Type("ValidateKeyProject", func() {
	Required("id", "name", "slug")

	Attribute("id", String, "The ID of the project")
	Attribute("name", String, "The name of the project")
	Attribute("slug", String, "The slug of the project")
})
