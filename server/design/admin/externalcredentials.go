package admin

import (
	. "goa.design/goa/v3/dsl"

	extcred "github.com/speakeasy-api/gram/server/design/externalcredentials"
	"github.com/speakeasy-api/gram/server/design/security"
)

// MCP parity: not exposed through Staff Admin MCP. Platform credentials are
// the identity Speakeasy signs with for every organization, so repointing or
// deleting one is incident-sensitive and stays a confirmed dashboard action.
// Not a Platform MCP tool: the records belong to no organization.
func platformExternalCredentialMethods() {
	Method("listPlatformExternalCredentials", func() {
		declareUnavailable()
		Description("List the platform external credentials (organization_id NULL, project_id NULL) that Speakeasy authenticates into a cloud provider with. Optionally filter by provider.")

		Payload(func() {
			Attribute("provider", String, "Only return credentials for this provider.", func() {
				Enum("aws_iam", "gcp_iam")
			})
			security.AdminAuthPayload()
		})

		Result(extcred.ListExternalCredentialsResult)

		HTTP(func() {
			declareUnavailableResponse()
			GET("/admin/externalCredentials.list")
			Param("provider")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminListPlatformExternalCredentials")
		Meta("openapi:extension:x-speakeasy-name-override", "listPlatformExternalCredentials")
	})

	Method("getGcpIamPlatformCredential", func() {
		declareUnavailable()
		Description("Get a platform GCP IAM external credential by ID.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to get.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.AdminAuthPayload()
		})

		Result(extcred.GcpIamCredential)

		HTTP(func() {
			declareUnavailableResponse()
			GET("/admin/externalCredentials.getGcpIam")
			Param("id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetGcpIamPlatformCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "getGcpIamPlatformCredential")
	})

	Method("createGcpIamPlatformCredential", func() {
		declareUnavailable()
		Description("Create a platform GCP IAM external credential. Set the wif_* triple for Workload Identity Federation, impersonate_service_account alone for direct impersonation, or neither for Speakeasy's ambient attached identity.")

		Payload(func() {
			Extend(CreatePlatformGcpIamCredentialForm)
			security.AdminAuthPayload()
		})

		Result(extcred.GcpIamCredential)

		HTTP(func() {
			declareUnavailableResponse()
			POST("/admin/externalCredentials.createGcpIam")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminCreateGcpIamPlatformCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "createGcpIamPlatformCredential")
	})

	Method("updateGcpIamPlatformCredential", func() {
		declareUnavailable()
		Description("Replace a platform GCP IAM external credential's name and configuration. Refused while managed signing keys still sign through it. Changing the impersonated account or wif_* identity is also refused while organization encryption keys or issuer keys still reach KMS through it; renames stay allowed.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to update.", func() {
				Format(FormatUUID)
			})
			Extend(CreatePlatformGcpIamCredentialForm)
			Required("id")
			security.AdminAuthPayload()
		})

		Result(extcred.GcpIamCredential)

		HTTP(func() {
			declareUnavailableResponse()
			POST("/admin/externalCredentials.updateGcpIam")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminUpdateGcpIamPlatformCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "updateGcpIamPlatformCredential")
	})

	Method("verifyGcpIamPlatformCredential", func() {
		declareUnavailable()
		Description("Run a live 'who am I' probe against a platform GCP IAM credential's resolved identity and report the effective principal. Nothing is persisted.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to verify.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.AdminAuthPayload()
		})

		Result(VerifyPlatformCredentialResult)

		HTTP(func() {
			declareUnavailableResponse()
			POST("/admin/externalCredentials.verifyGcpIam")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminVerifyGcpIamPlatformCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "verifyGcpIamPlatformCredential")
	})

	Method("deleteGcpIamPlatformCredential", func() {
		declareUnavailable()
		Description("Soft-delete a platform GCP IAM external credential. Refused while managed signing keys, organization encryption keys or live issuer keys still use it. Deleting a missing credential succeeds.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to delete.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.AdminAuthPayload()
		})

		HTTP(func() {
			declareUnavailableResponse()
			POST("/admin/externalCredentials.deleteGcpIam")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "adminDeleteGcpIamPlatformCredential")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteGcpIamPlatformCredential")
	})
}

// CreatePlatformGcpIamCredentialForm is the input for creating a platform GCP
// IAM credential. The authentication approach is inferred from which fields are
// provided:
//
//   - wif_* triple set: Workload Identity Federation (impersonate_service_account
//     is an optional hop).
//   - impersonate_service_account only: direct impersonation.
//   - none: Speakeasy's ambient attached identity.
//
// The platform tier keeps all three because these records describe Speakeasy's own
// infrastructure identity. The organization tier accepts impersonation only —
// see externalcredentials.CreateGcpIamCredentialForm for why.
var CreatePlatformGcpIamCredentialForm = Type("CreatePlatformGcpIamCredentialForm", func() {
	Attribute("name", String, "A human-readable name for the credential.")
	Attribute("impersonate_service_account", String, "The service account Speakeasy impersonates. Set alone for direct impersonation, or as the hop alongside the wif_* fields.")
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
