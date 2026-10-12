package jsonwebkeysets

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// The jsonWebKeySets service exposes organization-scoped CRUD over JSON Web Key
// Sets and the publish-before-sign lifecycle of the keys published in them.
// Each set is backed by an organization external key (a customer KMS key); the
// private half never leaves the customer's KMS, and what this service manages
// is the published public keys — each a kid, a lifecycle state, and a public
// JWK document — that verifiers consume.
//
// There is no public JWKS document route here: the document is served
// per-issuer once a set is attached to one (AIS-243).
var _ = Service("jsonWebKeySets", func() {
	Description("Manage organization-level JSON Web Key Sets — published public keys backed by customer KMS keys — and the publish/activate/retire/revoke lifecycle of their keys.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("getOrganizationEnterpriseIssuer", func() {
		Description("Read the organization's enterprise authorization issuer: the permanent issuer URI to register with supported services, its published keys and whether signing is available. Requires org:read.")
		Payload(func() { security.SessionPayload() })
		Result(OrganizationEnterpriseIssuer)
		HTTP(func() {
			GET("/rpc/jsonWebKeySets.getOrganizationEnterpriseIssuer")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getOrganizationEnterpriseIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "getOrganizationEnterpriseIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationEnterpriseIssuer"}`)
	})

	Method("enableOrganizationEnterpriseIssuer", func() {
		Description("Provision the organization's enterprise authorization issuer with a dedicated Speakeasy-managed signing key. Idempotent. Requires org:admin and the SSO entitlement. Does not configure trust at any service or grant access.")
		Payload(func() { security.SessionPayload() })
		Result(OrganizationEnterpriseIssuer)
		HTTP(func() {
			POST("/rpc/jsonWebKeySets.enableOrganizationEnterpriseIssuer")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "enableOrganizationEnterpriseIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "enableOrganizationEnterpriseIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "EnableOrganizationEnterpriseIssuer"}`)
	})

	Method("disableOrganizationEnterpriseIssuer", func() {
		Description("Withdraw the organization's enterprise authorization issuer: every key is revoked, signing stops and cached service tokens are discarded. Enabling again keeps the same issuer URI with a new key. Requires org:admin.")
		Payload(func() { security.SessionPayload() })
		Result(OrganizationEnterpriseIssuer)
		HTTP(func() {
			POST("/rpc/jsonWebKeySets.disableOrganizationEnterpriseIssuer")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "disableOrganizationEnterpriseIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "disableOrganizationEnterpriseIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DisableOrganizationEnterpriseIssuer"}`)
	})

	Method("rotateOrganizationEnterpriseIssuer", func() {
		Description("Rotate the issuer's signing key without changing its URI. The first call publishes a new key and returns rotation_ready_at; call again with the same active_kid after that time to activate it. The previous key stays published briefly so services can still verify it. Requires org:admin.")
		Payload(func() {
			Attribute("active_kid", String, "The key ID currently active, as last read.")
			Required("active_kid")
			security.SessionPayload()
		})
		Result(OrganizationEnterpriseIssuer)
		HTTP(func() {
			POST("/rpc/jsonWebKeySets.rotateOrganizationEnterpriseIssuer")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "rotateOrganizationEnterpriseIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "rotateOrganizationEnterpriseIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RotateOrganizationEnterpriseIssuer"}`)
	})

	Method("replaceOrganizationEnterpriseIssuerKey", func() {
		Description("Compromise response: revoke one issuer key immediately and, if it was active, activate a fresh key at once. The issuer URI does not change; services stop accepting the revoked key once their key cache refreshes. Requires org:admin.")
		Payload(func() {
			Attribute("kid", String, "The key ID to revoke.")
			Required("kid")
			security.SessionPayload()
		})
		Result(OrganizationEnterpriseIssuer)
		HTTP(func() {
			POST("/rpc/jsonWebKeySets.replaceOrganizationEnterpriseIssuerKey")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "replaceOrganizationEnterpriseIssuerKey")
		Meta("openapi:extension:x-speakeasy-name-override", "replaceOrganizationEnterpriseIssuerKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ReplaceOrganizationEnterpriseIssuerKey"}`)
	})

	Method("createSet", func() {
		Description("Create a JSON Web Key Set backed by an organization external key, minting and publishing the set's first key straight to active. Reads the backing key's public half from the customer's KMS and refuses when the key's real algorithm disagrees with the one recorded against it. Rate limited per organization. Requires org:admin.")

		// Declared here rather than in shared.DeclareErrorResponses because only the
		// rate-limited endpoints can return it, and putting it in the shared set
		// would type a 429 onto every method of every service.
		Error(string(oops.CodeRateLimitExceeded), func() { Description(oops.CodeRateLimitExceeded.UserMessage()) })

		Payload(func() {
			Extend(CreateJsonWebKeySetForm)
			security.SessionPayload()
		})

		Result(JsonWebKeySet)

		HTTP(func() {
			POST("/rpc/jsonWebKeySets.create")
			security.SessionHeader()
			Response(StatusOK)
			Response(string(oops.CodeRateLimitExceeded), StatusTooManyRequests, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "createJsonWebKeySet")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateJsonWebKeySet"}`)
	})

	Method("updateSet", func() {
		Description("Update a JSON Web Key Set's name and backing external key. Requires org:admin. Both fields are replaced, not patched. Re-pointing the backing key is how rotation begins: point the set at the new external key, then publish a key from it. Already-published keys are unaffected — each keeps signing with the external key it was minted from.")

		Payload(func() {
			Attribute("id", String, "The ID of the key set to update.", func() {
				Format(FormatUUID)
			})
			Extend(UpdateJsonWebKeySetForm)
			Required("id")
			security.SessionPayload()
			// Named explicitly: the derived name for this body is the generic
			// "UpdateSetRequestBody", which says nothing about the resource once
			// hoisted into the SDK's flat model namespace.
			Meta("openapi:typename", "UpdateJsonWebKeySetRequestBody")
		})

		Result(JsonWebKeySet)

		HTTP(func() {
			POST("/rpc/jsonWebKeySets.update")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateJsonWebKeySet")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateJsonWebKeySet"}`)
	})

	Method("listSets", func() {
		Description("List the organization's JSON Web Key Sets. Requires org:read.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListJsonWebKeySetsResult)

		HTTP(func() {
			GET("/rpc/jsonWebKeySets.list")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listJsonWebKeySets")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListJsonWebKeySets"}`)
	})

	Method("getSet", func() {
		Description("Get a JSON Web Key Set by ID. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The ID of the key set to get.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(JsonWebKeySet)

		HTTP(func() {
			GET("/rpc/jsonWebKeySets.get")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getJsonWebKeySet")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetJsonWebKeySet"}`)
	})

	Method("getSetDeletePreflight", func() {
		Description("Authoritative impact summary for deleting a JSON Web Key Set: the remote_session_clients still referencing it. deleteSet returns a conflict for exactly the sets this reports a non-zero client_count for. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The ID of the key set to summarize.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(JsonWebKeySetDeletePreflight)

		HTTP(func() {
			GET("/rpc/jsonWebKeySets.getDeletePreflight")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getJsonWebKeySetDeletePreflight")
		Meta("openapi:extension:x-speakeasy-name-override", "getDeletePreflight")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "JsonWebKeySetDeletePreflight"}`)
	})

	Method("deleteSet", func() {
		Description("Soft-delete a JSON Web Key Set by ID, withdrawing every key still published in it in the same operation. Refused with a conflict while any live remote_session_client still references the set — detach it there first; see getDeletePreflight. Requires org:admin. Tokens signed with the set's keys stop verifying, so treat this as decommissioning the set's whole trust anchor rather than tidying up.")

		Payload(func() {
			Attribute("id", String, "The ID of the key set to delete.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/jsonWebKeySets.delete")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteJsonWebKeySet")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteJsonWebKeySet"}`)
	})

	Method("listKeys", func() {
		Description("List a JSON Web Key Set's published keys, newest first. Revoked keys drop out of the default listing; pass include_revoked to see the set's full revocation history. Requires org:read.")

		Payload(func() {
			Attribute("set_id", String, "The ID of the key set whose keys to list.", func() {
				Format(FormatUUID)
			})
			Attribute("include_revoked", Boolean, "Also return revoked keys.", func() {
				Default(false)
			})
			Required("set_id")
			security.SessionPayload()
		})

		Result(ListJsonWebKeysResult)

		HTTP(func() {
			GET("/rpc/jsonWebKeySets.listKeys")
			Param("set_id")
			Param("include_revoked")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listJsonWebKeys")
		Meta("openapi:extension:x-speakeasy-name-override", "listKeys")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListJsonWebKeys"}`)
	})

	Method("publishKey", func() {
		Description("Mint and publish a new key from the set's current backing external key. The key is published as pending — visible to verifiers so their caches warm up — unless the set has no active key, in which case it activates immediately. Publishing the same backing key again while its kid is present in the set (including revoked) is refused as a conflict. Reads the public half from the customer's KMS; rate limited per organization. Requires org:admin.")

		Error(string(oops.CodeRateLimitExceeded), func() { Description(oops.CodeRateLimitExceeded.UserMessage()) })

		Payload(func() {
			Attribute("set_id", String, "The ID of the key set to publish into.", func() {
				Format(FormatUUID)
			})
			Required("set_id")
			security.SessionPayload()
		})

		Result(JsonWebKey)

		HTTP(func() {
			POST("/rpc/jsonWebKeySets.publishKey")
			Param("set_id")
			security.SessionHeader()
			Response(StatusOK)
			Response(string(oops.CodeRateLimitExceeded), StatusTooManyRequests, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "publishJsonWebKey")
		Meta("openapi:extension:x-speakeasy-name-override", "publishKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PublishJsonWebKey"}`)
	})

	Method("activateKey", func() {
		Description("Make a published key the set's active signing key, retiring the previously active key in the same operation. The key must be pending or retired; activating the already-active key is a no-op. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to activate.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(JsonWebKey)

		HTTP(func() {
			POST("/rpc/jsonWebKeySets.activateKey")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "activateJsonWebKey")
		Meta("openapi:extension:x-speakeasy-name-override", "activateKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ActivateJsonWebKey"}`)
	})

	Method("retireKey", func() {
		Description("Take the set's active key out of signing use without withdrawing it: the key stays published so tokens already signed with it keep verifying. The key must be active. This is the graceful wind-down; use revoke when the key must stop verifying too. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to retire.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(JsonWebKey)

		HTTP(func() {
			POST("/rpc/jsonWebKeySets.retireKey")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "retireJsonWebKey")
		Meta("openapi:extension:x-speakeasy-name-override", "retireKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RetireJsonWebKey"}`)
	})

	Method("revokeKey", func() {
		Description("Withdraw a published key entirely: it leaves the published set and tokens signed with it stop verifying. This is the compromise response, not the graceful wind-down — use retire for that. A revoked kid can never be republished into the set. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to revoke.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(JsonWebKey)

		HTTP(func() {
			POST("/rpc/jsonWebKeySets.revokeKey")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeJsonWebKey")
		Meta("openapi:extension:x-speakeasy-name-override", "revokeKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeJsonWebKey"}`)
	})
})

// OrganizationEnterpriseIssuer is an organization's public trust anchor, never a signing capability.
var OrganizationEnterpriseIssuer = Type("OrganizationEnterpriseIssuer", func() {
	Attribute("available", Boolean, "Whether issuer provisioning is available for this organization.")
	Attribute("enabled", Boolean, "Whether the organization's issuer is provisioned.")
	Attribute("signing_enabled", Boolean, "Whether Speakeasy is currently allowed to sign with the issuer on this deployment.")
	Attribute("issuer", String, "Issuer URI to register with supported services. It never changes.")
	Attribute("jwks_uri", String, "Public verification keys.")
	Attribute("keys", ArrayOf(OrganizationEnterpriseIssuerKey), "Published keys, newest first.")
	Attribute("rotation_ready_at", String, "When a pending rotation can be activated.", func() {
		Format(FormatDateTime)
	})
	Required("available", "enabled", "signing_enabled", "keys")
})

// OrganizationEnterpriseIssuerKey is one published issuer key.
var OrganizationEnterpriseIssuerKey = Type("OrganizationEnterpriseIssuerKey", func() {
	Attribute("kid", String, "Key ID.")
	Attribute("state", String, "Lifecycle state.", func() {
		Enum("pending", "active", "retired")
	})
	Required("kid", "state")
})
