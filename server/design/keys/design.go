package keys

import (
	. "goa.design/goa/v3/dsl"

	agentsdesign "github.com/speakeasy-api/gram/server/design/agents"
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

	Method("rotateKey", func() {
		Description("Rotate an API key. Agent-key rotation replaces immutable delegation and directly revokes the old row.")

		Payload(func() {
			Extend(RotateKeyForm)
			security.SessionPayload()
		})

		Result(KeyModel)

		HTTP(func() {
			POST("/rpc/keys.rotate")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "rotateAPIKey")
		Meta("openapi:extension:x-speakeasy-name-override", "rotate")
	})

	Method("listKeys", func() {
		Description("List all api keys for an organization")

		Payload(func() {
			Attribute("agent_id", String, "When set, list keys for this first-class agent", func() { Format(FormatUUID) })
			security.SessionPayload()
		})

		Result(ListKeysResult)

		HTTP(func() {
			GET("/rpc/keys.list")
			Param("agent_id")
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
	Required("name")

	Attribute("name", String, "The name of the key", func() { MaxLength(255) })
	Attribute("scopes", ArrayOf(String), "Legacy transport scopes. Omitted or empty defaults to consumer for ordinary keys; agent keys require no scopes.")
	Attribute("agent_id", String, "First-class agent subject. Omit for an ordinary API key.", func() { Format(FormatUUID) })
	Attribute("delegated_grants_version", Int, "Delegated policy format version. Required for agent keys.", func() { Minimum(1) })
	Attribute("requested_grants", ArrayOf(agentsdesign.PolicyGrantForm), "Exact allow grants approved for an agent credential")
	Attribute("expires_at", String, "Agent credential expiry; defaults to 90 days and cannot exceed one year", func() { Format(FormatDateTime) })
})

var RotateKeyForm = Type("RotateKeyForm", func() {
	Required("id", "name", "delegated_grants_version", "requested_grants")
	Attribute("id", String, "API key identifier to replace", func() { Format(FormatUUID) })
	Attribute("name", String, "The name of the replacement key", func() { MaxLength(255) })
	Attribute("scopes", ArrayOf(String), "Legacy transport scopes; agent rotation requires an empty array")
	Attribute("delegated_grants_version", Int, "Delegated policy format version", func() { Minimum(1) })
	Attribute("requested_grants", ArrayOf(agentsdesign.PolicyGrantForm), "New exact allow grants approved for the replacement credential")
	Attribute("expires_at", String, "Replacement expiry; defaults to 90 days and cannot exceed one year", func() { Format(FormatDateTime) })
})

var AgentDelegatedGrant = Type("AgentDelegatedGrant", func() {
	Required("scope", "selector")
	Attribute("scope", String)
	Attribute("selector", agentsdesign.PolicySelector)
})

var AgentDelegatedPolicy = Type("AgentDelegatedPolicy", func() {
	Required("requested", "effective")
	Attribute("requested", ArrayOf(AgentDelegatedGrant))
	Attribute("effective", ArrayOf(AgentDelegatedGrant))
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
	Attribute("created_by_user_id", String, "The human creator; immutable authorizer for agent keys")
	Attribute("name", String, "The name of the key")
	Attribute("key_prefix", String, "The store prefix of the api key for recognition")
	Attribute("key", String, "The token of the api key (only returned on key creation or rotation)") // this will only be set on key creation or rotation
	Attribute("scopes", ArrayOf(String), "Legacy transport scopes; always empty for agent keys")
	Attribute("subject_urn", String, "Principal authenticated by this key; agent keys use agent:<uuid>")
	Attribute("delegated_grants", AgentDelegatedPolicy, "Immutable delegated policy approved for this credential")
	Attribute("delegated_grants_version", Int, "Delegated policy format version")
	Attribute("expires_at", String, func() {
		Description("Required expiry for an agent key; legacy keys may not expire.")
		Format(FormatDateTime)
	})
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
