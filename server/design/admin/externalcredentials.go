package admin

import (
	. "goa.design/goa/v3/dsl"

	extcred "github.com/speakeasy-api/gram/server/design/externalcredentials"
	platformec "github.com/speakeasy-api/gram/server/design/platformadmin/externalcredentials"
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
			Extend(platformec.CreatePlatformGcpIamCredentialForm)
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
		Description("Replace a platform GCP IAM external credential's name and configuration. Refused while managed signing keys still sign through it.")

		Payload(func() {
			Attribute("id", String, "The ID of the credential to update.", func() {
				Format(FormatUUID)
			})
			Extend(platformec.CreatePlatformGcpIamCredentialForm)
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

		Result(platformec.VerifyPlatformCredentialResult)

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
		Description("Soft-delete a platform GCP IAM external credential. Refused while managed signing keys or live issuer keys still sign through it. Deleting a missing credential succeeds.")

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
