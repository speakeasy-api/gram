// Package externalcredentials declares the adminExternalCredentials Goa
// service: the platform-admin (Speakeasy-only) surface for curating
// "platform" external_credentials records (organization_id IS NULL AND
// project_id IS NULL) shared across every organization. Implemented on the
// existing *externalcredentials.Service; reuses the existing form/result types.
package externalcredentials

import (
	. "goa.design/goa/v3/dsl"

	extcred "github.com/speakeasy-api/gram/server/design/externalcredentials"
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("adminExternalCredentials", func() {
	Description("Platform-admin management of platform external_credentials — how Gram authenticates into a cloud provider to reach a platform KMS key. Shared across every organization (organization_id NULL, project_id NULL). Speakeasy-staff only; every method requires the platform-admin flag.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("createGcpIamPlatformCredential", func() {
		Description("Create a platform GCP IAM external credential (organization_id NULL, project_id NULL). Requires platform admin.")

		Payload(func() {
			Extend(CreatePlatformGcpIamCredentialForm)
			security.SessionPayload()
		})

		Result(extcred.GcpIamCredential)

		HTTP(func() {
			POST("/rpc/adminExternalCredentials.createGcpIam")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createGcpIamPlatformCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "createGcpIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateGcpIamPlatformCredential"}`)
	})

	Method("listPlatformExternalCredentials", func() {
		Description("List the platform external credentials (provider-independent summary). Optionally filter by provider. Requires platform admin.")

		Payload(func() {
			Attribute("provider", String, "Only return credentials for this provider.", func() {
				Enum("aws_iam", "gcp_iam")
			})
			security.SessionPayload()
		})

		Result(extcred.ListExternalCredentialsResult)

		HTTP(func() {
			GET("/rpc/adminExternalCredentials.list")
			Param("provider")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listPlatformExternalCredentials")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListPlatformExternalCredentials"}`)
	})

	Method("updateGcpIamPlatformCredential", func() {
		Description("Replace a platform GCP IAM external credential's configuration. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to update.", func() {
				Format(FormatUUID)
			})
			Extend(CreatePlatformGcpIamCredentialForm)
			Required("id")
			security.SessionPayload()
		})

		Result(extcred.GcpIamCredential)

		HTTP(func() {
			POST("/rpc/adminExternalCredentials.updateGcpIam")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateGcpIamPlatformCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "updateGcpIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateGcpIamPlatformCredential"}`)
	})

	Method("getGcpIamPlatformCredential", func() {
		Description("Get a platform GCP IAM external credential by ID. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to get.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(extcred.GcpIamCredential)

		HTTP(func() {
			GET("/rpc/adminExternalCredentials.getGcpIam")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getGcpIamPlatformCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "getGcpIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetGcpIamPlatformCredential"}`)
	})

	Method("verifyGcpIamPlatformCredential", func() {
		Description("Run a live 'who am I' probe against a platform GCP IAM credential's resolved identity and report the effective principal. Ephemeral: nothing is persisted. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to verify.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(VerifyPlatformCredentialResult)

		HTTP(func() {
			POST("/rpc/adminExternalCredentials.verifyGcpIam")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "verifyGcpIamPlatformCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "verifyGcpIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "VerifyGcpIamPlatformCredential"}`)
	})

	Method("deleteGcpIamPlatformCredential", func() {
		Description("Soft-delete a platform GCP IAM external credential by ID. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to delete.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/adminExternalCredentials.deleteGcpIam")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteGcpIamPlatformCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteGcpIam")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteGcpIamPlatformCredential"}`)
	})
})

// CreatePlatformGcpIamCredentialForm is the input for creating a platform GCP
// IAM credential. The authentication approach is inferred from which fields are
// provided:
//
//   - wif_* triple set: Workload Identity Federation (impersonate_service_account
//     is an optional hop).
//   - impersonate_service_account only: direct impersonation.
//   - none: Gram's ambient attached identity.
//
// The platform tier keeps all three because these records describe Gram's own
// infrastructure identity. The organization tier accepts impersonation only —
// see externalcredentials.CreateGcpIamCredentialForm for why.
var CreatePlatformGcpIamCredentialForm = Type("CreatePlatformGcpIamCredentialForm", func() {
	Attribute("name", String, "A human-readable name for the credential.")
	Attribute("impersonate_service_account", String, "The service account Gram impersonates. Set alone for direct impersonation, or as the hop alongside the wif_* fields.")
	Attribute("wif_pool_id", String, "Workload Identity Federation pool ID. Set together with the other wif_* fields.")
	Attribute("wif_provider_id", String, "Workload Identity Federation provider ID. Set together with the other wif_* fields.")
	Attribute("wif_project_number", String, "GCP project number backing the WIF pool. Set together with the other wif_* fields.")

	Required("name")
})

// VerifyPlatformCredentialResult is the outcome of a live "who am I" probe
// against a platform credential's resolved cloud identity. It is ephemeral and
// never persisted.
var VerifyPlatformCredentialResult = Type("VerifyPlatformCredentialResult", func() {
	Description("Result of a live 'who am I' probe against a platform credential's resolved identity.")

	Attribute("verified", Boolean, "Whether the credential's identity resolved successfully.")
	Attribute("principal", String, "The effective principal the identity resolves to (e.g. a service-account email). May be empty when the resolution source cannot report it.")
	Attribute("identity_source", String, "How the identity was resolved. Lets the caller tell an in-cluster attached identity apart from local Application Default Credentials or an impersonated service account.", func() {
		Enum("metadata_server", "application_default_credentials", "impersonation")
	})
	Attribute("detail", String, "Human-readable detail about the probe outcome (e.g. why the principal is unavailable, or the failure reason).")

	Required("verified")
})
