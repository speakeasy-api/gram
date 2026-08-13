package externalkeys

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// The externalKeys service exposes organization-scoped CRUD over external_keys
// (externally-managed KMS keys Gram signs with, backed by an external
// credential). Writes are per-provider and strongly typed; reads use a generic,
// supertype-only list plus per-provider typed detail endpoints. Verification
// against the cloud provider is per-provider too, and exists only for GCP: an
// AWS key cannot be probed because Gram holds no AWS identity to assume a
// customer role from.
var _ = Service("externalKeys", func() {
	Description("Manage organization-level external keys — externally-managed AWS or GCP KMS keys Gram signs with.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("createAwsKmsKey", func() {
		Description("Create an AWS KMS external key. Requires org:admin.")

		Payload(func() {
			Extend(CreateAwsKmsKeyForm)
			security.SessionPayload()
		})

		Result(AwsKmsKey)

		HTTP(func() {
			POST("/rpc/externalKeys.createAwsKms")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createAwsKmsKey")
		Meta("openapi:extension:x-speakeasy-name-override", "createAwsKms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateAwsKmsKey"}`)
	})

	Method("updateAwsKmsKey", func() {
		Description("Update an AWS KMS external key's name, backing credential and customer grant reference. Requires org:admin. These three fields are replaced, not patched: omitting the optional customer_grant_reference clears it. The key ARN and algorithm are immutable: an external key identifies exactly one signable key permanently, so changing what the key is means deleting it and creating a new one. The backing credential stays editable because repairing the path to a key does not change the key material Gram signs with.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to update.", func() {
				Format(FormatUUID)
			})
			Extend(UpdateAwsKmsKeyForm)
			Required("id")
			security.SessionPayload()
			// Named explicitly for the same reason as updateGcpKmsKey below: the
			// two payloads are structurally identical and Goa deduplicates
			// request bodies by shape.
			Meta("openapi:typename", "UpdateAwsKmsKeyRequestBody")
		})

		Result(AwsKmsKey)

		HTTP(func() {
			POST("/rpc/externalKeys.updateAwsKms")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateAwsKmsKey")
		Meta("openapi:extension:x-speakeasy-name-override", "updateAwsKms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateAwsKmsKey"}`)
	})

	Method("createGcpKmsKey", func() {
		Description("Create a GCP KMS external key. Requires org:admin.")

		Payload(func() {
			Extend(CreateGcpKmsKeyForm)
			security.SessionPayload()
		})

		Result(GcpKmsKey)

		HTTP(func() {
			POST("/rpc/externalKeys.createGcpKms")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createGcpKmsKey")
		Meta("openapi:extension:x-speakeasy-name-override", "createGcpKms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateGcpKmsKey"}`)
	})

	Method("updateGcpKmsKey", func() {
		Description("Update a GCP KMS external key's name, backing credential and customer grant reference. Requires org:admin. These three fields are replaced, not patched: omitting the optional customer_grant_reference clears it. The resource name and algorithm are immutable: an external key identifies exactly one signable crypto key version permanently, so changing what the key is means deleting it and creating a new one. The backing credential stays editable because repairing the path to a key does not change the key material Gram signs with.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to update.", func() {
				Format(FormatUUID)
			})
			Extend(UpdateGcpKmsKeyForm)
			Required("id")
			security.SessionPayload()
			// The AWS and GCP update payloads are structurally identical, and
			// Goa's OpenAPI emitter deduplicates request bodies by shape (not by
			// description), so without an explicit name both methods share one
			// UpdateAwsKmsKeyRequestBody schema and the generated SDK has
			// updateGcpKms taking an AWS-named body type.
			Meta("openapi:typename", "UpdateGcpKmsKeyRequestBody")
		})

		Result(GcpKmsKey)

		HTTP(func() {
			POST("/rpc/externalKeys.updateGcpKms")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateGcpKmsKey")
		Meta("openapi:extension:x-speakeasy-name-override", "updateGcpKms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateGcpKmsKey"}`)
	})

	Method("listExternalKeys", func() {
		Description("List the organization's external keys (provider-independent summary). Optionally filter by provider. Requires org:read.")

		Payload(func() {
			Attribute("provider", String, "Only return keys for this provider.", func() {
				Enum("aws_kms", "gcp_kms")
			})
			security.SessionPayload()
		})

		Result(ListExternalKeysResult)

		HTTP(func() {
			GET("/rpc/externalKeys.list")
			Param("provider")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listExternalKeys")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListExternalKeys"}`)
	})

	Method("listAwsKmsKeys", func() {
		Description("List the organization's AWS KMS external keys. Requires org:read.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListExternalKeysResult)

		HTTP(func() {
			GET("/rpc/externalKeys.listAwsKms")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listAwsKmsKeys")
		Meta("openapi:extension:x-speakeasy-name-override", "listAwsKms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListAwsKmsKeys"}`)
	})

	Method("listGcpKmsKeys", func() {
		Description("List the organization's GCP KMS external keys. Requires org:read.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListExternalKeysResult)

		HTTP(func() {
			GET("/rpc/externalKeys.listGcpKms")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listGcpKmsKeys")
		Meta("openapi:extension:x-speakeasy-name-override", "listGcpKms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListGcpKmsKeys"}`)
	})

	Method("getAwsKmsKey", func() {
		Description("Get an AWS KMS external key by ID. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to get.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(AwsKmsKey)

		HTTP(func() {
			GET("/rpc/externalKeys.getAwsKms")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAwsKmsKey")
		Meta("openapi:extension:x-speakeasy-name-override", "getAwsKms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetAwsKmsKey"}`)
	})

	Method("getGcpKmsKey", func() {
		Description("Get a GCP KMS external key by ID. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to get.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(GcpKmsKey)

		HTTP(func() {
			GET("/rpc/externalKeys.getGcpKms")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getGcpKmsKey")
		Meta("openapi:extension:x-speakeasy-name-override", "getGcpKms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetGcpKmsKey"}`)
	})

	Method("verifyGcpKmsKey", func() {
		Description("Probe that Gram can reach a GCP KMS external key through its backing credential and use it to sign: read the key's public half, confirm its algorithm matches the one recorded, sign a probe digest, and verify that signature locally against the public half. Performs a real signing operation, which is billed to the key's owner and lands in their Cloud Audit Log. Ephemeral: nothing is persisted. Rate limited per organization. Requires org:admin.")

		// Declared here rather than in shared.DeclareErrorResponses because only the
		// rate-limited endpoints can return it, and putting it in the shared set
		// would type a 429 onto every method of every service.
		Error(string(oops.CodeRateLimitExceeded), func() { Description(oops.CodeRateLimitExceeded.UserMessage()) })

		Payload(func() {
			Attribute("id", String, "The ID of the key to verify.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(VerifyKmsKeyResult)

		HTTP(func() {
			POST("/rpc/externalKeys.verifyGcpKms")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
			Response(string(oops.CodeRateLimitExceeded), StatusTooManyRequests, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "verifyGcpKmsKey")
		Meta("openapi:extension:x-speakeasy-name-override", "verifyGcpKms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "VerifyGcpKmsKey"}`)
	})

	Method("deleteAwsKmsKey", func() {
		Description("Soft-delete an AWS KMS external key by ID. Requires org:admin. Refused with a conflict while any JSON Web Key Set or published JSON Web Key still references the key, since deleting it would break verification for every already-published kid.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to delete.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/externalKeys.deleteAwsKms")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteAwsKmsKey")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteAwsKms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteAwsKmsKey"}`)
	})

	Method("deleteGcpKmsKey", func() {
		Description("Soft-delete a GCP KMS external key by ID. Requires org:admin. Refused with a conflict while any JSON Web Key Set or published JSON Web Key still references the key, since deleting it would break verification for every already-published kid.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to delete.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/externalKeys.deleteGcpKms")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteGcpKmsKey")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteGcpKms")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteGcpKmsKey"}`)
	})
})
