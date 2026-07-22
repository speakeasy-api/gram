package externalkeys

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// The externalKeys service exposes organization-scoped CRUD over external_keys
// (externally-managed KMS keys Gram signs with, backed by an external
// credential). Writes are per-provider and strongly typed; reads use a generic,
// supertype-only list plus per-provider typed detail endpoints. Verification of
// the key against the cloud provider is intentionally out of scope here.
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
		Description("Replace an AWS KMS external key's configuration. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to update.", func() {
				Format(FormatUUID)
			})
			Extend(CreateAwsKmsKeyForm)
			Required("id")
			security.SessionPayload()
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
		Description("Replace a GCP KMS external key's configuration. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to update.", func() {
				Format(FormatUUID)
			})
			Extend(CreateGcpKmsKeyForm)
			Required("id")
			security.SessionPayload()
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

	Method("deleteAwsKmsKey", func() {
		Description("Soft-delete an AWS KMS external key by ID. Requires org:admin.")

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
		Description("Soft-delete a GCP KMS external key by ID. Requires org:admin.")

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
