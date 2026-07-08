package externalcredentials

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// The externalCredentials service exposes organization-scoped CRUD over
// external_credentials (how Gram authenticates into a customer's AWS or GCP
// account). Writes are per-provider and strongly typed; reads use a generic,
// supertype-only list plus per-provider typed detail endpoints. Verification of
// the credential against the cloud provider is intentionally out of scope here.
var _ = Service("externalCredentials", func() {
	Description("Manage organization-level external credentials — how Gram authenticates into a customer's AWS or GCP account.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("createAwsIamCredential", func() {
		Description("Create an AWS IAM external credential. Requires org:admin.")

		Payload(func() {
			Extend(CreateAwsIamCredentialForm)
			security.SessionPayload()
		})

		Result(AwsIamCredential)

		HTTP(func() {
			POST("/rpc/externalCredentials.createAwsIam")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createAwsIamCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "createAwsIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateAwsIamCredential"}`)
	})

	Method("updateAwsIamCredential", func() {
		Description("Replace an AWS IAM external credential's configuration. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to update.", func() {
				Format(FormatUUID)
			})
			Extend(CreateAwsIamCredentialForm)
			Required("id")
			security.SessionPayload()
		})

		Result(AwsIamCredential)

		HTTP(func() {
			POST("/rpc/externalCredentials.updateAwsIam")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateAwsIamCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "updateAwsIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateAwsIamCredential"}`)
	})

	Method("createGcpIamCredential", func() {
		Description("Create a GCP IAM external credential. Requires org:admin.")

		Payload(func() {
			Extend(CreateGcpIamCredentialForm)
			security.SessionPayload()
		})

		Result(GcpIamCredential)

		HTTP(func() {
			POST("/rpc/externalCredentials.createGcpIam")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createGcpIamCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "createGcpIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateGcpIamCredential"}`)
	})

	Method("updateGcpIamCredential", func() {
		Description("Replace a GCP IAM external credential's configuration. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to update.", func() {
				Format(FormatUUID)
			})
			Extend(CreateGcpIamCredentialForm)
			Required("id")
			security.SessionPayload()
		})

		Result(GcpIamCredential)

		HTTP(func() {
			POST("/rpc/externalCredentials.updateGcpIam")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateGcpIamCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "updateGcpIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateGcpIamCredential"}`)
	})

	Method("listExternalCredentials", func() {
		Description("List the organization's external credentials (provider-independent summary). Optionally filter by provider. Requires org:read.")

		Payload(func() {
			Attribute("provider", String, "Only return credentials for this provider.", func() {
				Enum("aws_iam", "gcp_iam")
			})
			security.SessionPayload()
		})

		Result(ListExternalCredentialsResult)

		HTTP(func() {
			GET("/rpc/externalCredentials.list")
			Param("provider")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listExternalCredentials")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListExternalCredentials"}`)
	})

	Method("listAwsIamCredentials", func() {
		Description("List the organization's AWS IAM external credentials. Requires org:read.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListExternalCredentialsResult)

		HTTP(func() {
			GET("/rpc/externalCredentials.listAwsIam")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listAwsIamCredentials")
		Meta("openapi:extension:x-speakeasy-name-override", "listAwsIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListAwsIamCredentials"}`)
	})

	Method("listGcpIamCredentials", func() {
		Description("List the organization's GCP IAM external credentials. Requires org:read.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListExternalCredentialsResult)

		HTTP(func() {
			GET("/rpc/externalCredentials.listGcpIam")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listGcpIamCredentials")
		Meta("openapi:extension:x-speakeasy-name-override", "listGcpIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListGcpIamCredentials"}`)
	})

	Method("getAwsIamCredential", func() {
		Description("Get an AWS IAM external credential by ID. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to get.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(AwsIamCredential)

		HTTP(func() {
			GET("/rpc/externalCredentials.getAwsIam")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAwsIamCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "getAwsIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetAwsIamCredential"}`)
	})

	Method("getGcpIamCredential", func() {
		Description("Get a GCP IAM external credential by ID. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to get.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(GcpIamCredential)

		HTTP(func() {
			GET("/rpc/externalCredentials.getGcpIam")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getGcpIamCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "getGcpIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetGcpIamCredential"}`)
	})

	Method("deleteAwsIamCredential", func() {
		Description("Soft-delete an AWS IAM external credential by ID. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to delete.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/externalCredentials.deleteAwsIam")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteAwsIamCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteAwsIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteAwsIamCredential"}`)
	})

	Method("deleteGcpIamCredential", func() {
		Description("Soft-delete a GCP IAM external credential by ID. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to delete.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/externalCredentials.deleteGcpIam")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteGcpIamCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteGcpIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteGcpIamCredential"}`)
	})
})
