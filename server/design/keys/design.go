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

	Method("updateKeyPolicy", func() {
		Description("Replace an ordinary key's own policy: the mcp:connect and mcp:auth grants, and their blocks, its server and auth client access is evaluated against. Agent keys keep their delegated grants. Requires org admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the key.", func() { Format(FormatUUID) })
			Attribute("policy_grants", ArrayOf(APIKeyPolicyGrant), "The key's complete policy. An empty list removes every grant.", func() { MaxLength(100) })
			Required("id", "policy_grants")
			security.SessionPayload()
		})
		Result(KeyModel)

		HTTP(func() {
			POST("/rpc/keys.updatePolicy")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateAPIKeyPolicy")
		Meta("openapi:extension:x-speakeasy-name-override", "updatePolicy")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateAPIKeyPolicy"}`)
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
	Attribute("project_id", String, "Optional project binding. Omit for an organization-wide key; independent of permission scopes and the Gram-Project header.", func() {
		Format(FormatUUID)
	})
	Attribute("scopes", ArrayOf(String), "Legacy transport scopes. Omitted or empty defaults to consumer for ordinary keys; agent keys require no scopes.")
	Attribute("agent_id", String, "First-class agent subject. Omit for an ordinary API key.", func() { Format(FormatUUID) })
	Attribute("delegated_grants_version", Int, "Delegated policy format version. Required for agent keys.", func() { Minimum(1) })
	Attribute("requested_grants", ArrayOf(agentsdesign.PolicyGrantForm), "Exact allow grants approved for an agent credential")
	Attribute("expires_at", String, "Agent credential expiry; defaults to 90 days and cannot exceed one year", func() { Format(FormatDateTime) })
	Attribute("policy_grants", ArrayOf(APIKeyPolicyGrant), "Optional policy for an ordinary key: mcp:connect and mcp:auth grants, and mcp:blocked_connect and mcp:blocked_auth blocks, that its server and auth client access is evaluated against. Not yet enforced. Requires org admin.", func() { MaxLength(100) })
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

var APIKeyPolicySelector = Type("APIKeyPolicySelector", func() {
	Description("What an API key policy grant covers: servers (mcp) or auth clients (mcp_auth_client).")
	Required("resource_kind", "resource_id")
	Attribute("resource_kind", String, "mcp for mcp:connect grants, mcp_auth_client for mcp:auth grants", func() { Enum("mcp", "mcp_auth_client") })
	Attribute("resource_id", String, "A server or auth client ID, or '*' for every server in project_id. mcp:auth grants must name their client.")
	Attribute("project_id", String, "The project the servers are in; required when resource_id is '*', and the key's own project when it is bound to one.", func() { Format(FormatUUID) })
	Attribute("client_type", String, "Auth client type filter (mcp:auth only).", func() {
		Enum("service_account", "static_credential", "pass_through", "ema", "*")
	})
})

var APIKeyPolicyGrant = Type("APIKeyPolicyGrant", func() {
	Required("scope", "selector")
	Attribute("scope", String, "mcp:connect, mcp:auth, or a block: mcp:blocked_connect, mcp:blocked_auth", func() {
		Enum("mcp:connect", "mcp:blocked_connect", "mcp:auth", "mcp:blocked_auth")
	})
	Attribute("selector", APIKeyPolicySelector)
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
	Attribute("policy_grants", ArrayOf(APIKeyPolicyGrant), "An ordinary key's own policy")
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
