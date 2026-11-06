package orgencryption

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// The orgEncryption service manages the key encryption key (KEK) that protects
// an organization's data: registering customer-managed keys, switching the
// organization to one (and completing or rolling back the switch), rotating a
// use case's data encryption key, and resuming after a suspension. A
// Speakeasy-managed KEK is minted on first use by the features that encrypt,
// not through this API.
var _ = Service("orgEncryption", func() {
	Description("Manage the key encryption key that protects an organization's data: Speakeasy-managed by default, or a customer-managed GCP KMS key.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("getConfig", func() {
		Description("Get the organization's encryption configuration. Requires org:read. Not found until a feature has encrypted data for the organization.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(OrgEncryptionConfig)

		HTTP(func() {
			GET("/rpc/orgEncryption.getConfig")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getOrgEncryptionConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "getConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrgEncryptionConfig"}`)
	})

	Method("listKeys", func() {
		Description("List the organization's encryption keys, Speakeasy-managed and customer-managed. Requires org:read.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListOrgEncryptionKeysResult)

		HTTP(func() {
			GET("/rpc/orgEncryption.listKeys")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listOrgEncryptionKeys")
		Meta("openapi:extension:x-speakeasy-name-override", "listKeys")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrgEncryptionKeys"}`)
	})

	Method("createGcpKey", func() {
		Description("Register a customer-managed GCP KMS symmetric key the organization can switch to. Requires org:admin. The key is not used until the organization switches to it.")

		Payload(func() {
			Extend(CreateOrgEncryptionGcpKeyForm)
			security.SessionPayload()
		})

		Result(OrgEncryptionKey)

		HTTP(func() {
			POST("/rpc/orgEncryption.createGcpKey")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createOrgEncryptionGcpKey")
		Meta("openapi:extension:x-speakeasy-name-override", "createGcpKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateOrgEncryptionGcpKey"}`)
	})

	Method("verifyKey", func() {
		Description("Probe that Speakeasy can reach an encryption key and use it: encrypt a probe payload and decrypt it again. Performs real KMS operations, billed to the key's owner and recorded in their audit log. Nothing is persisted. Rate limited per organization. Requires org:admin.")

		Error(string(oops.CodeRateLimitExceeded), func() { Description(oops.CodeRateLimitExceeded.UserMessage()) })

		Payload(func() {
			Attribute("id", String, "The ID of the key to verify.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(VerifyOrgEncryptionKeyResult)

		HTTP(func() {
			POST("/rpc/orgEncryption.verifyKey")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
			Response(string(oops.CodeRateLimitExceeded), StatusTooManyRequests, func() {
				ContentType("application/json")
			})
		})

		Meta("openapi:operationId", "verifyOrgEncryptionKey")
		Meta("openapi:extension:x-speakeasy-name-override", "verifyKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "VerifyOrgEncryptionKey"}`)
	})

	Method("deleteKey", func() {
		Description("Delete a customer-managed encryption key. Requires org:admin. Refused with a conflict while the key is the organization's key, is being switched to or away from, or still wraps any data encryption key, because the data it protects would become unreadable. Speakeasy-managed keys cannot be deleted.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to delete.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/orgEncryption.deleteKey")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteOrgEncryptionKey")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteOrgEncryptionKey"}`)
	})

	Method("switchKey", func() {
		Description("Start switching the organization to a customer-managed key. Requires org:admin. Speakeasy verifies the key, then wraps every data encryption key under it; the previous key keeps everything readable until the switch completes, either through completeSwitch or automatically after the grace period, or rolls back.")

		Payload(func() {
			Attribute("external_key_id", String, "The ID of the registered encryption key to switch to.", func() {
				Format(FormatUUID)
			})
			Required("external_key_id")
			security.SessionPayload()
			Meta("openapi:typename", "SwitchOrgEncryptionKeyRequestBody")
		})

		Result(OrgEncryptionConfig)

		HTTP(func() {
			POST("/rpc/orgEncryption.switchKey")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "switchOrgEncryptionKey")
		Meta("openapi:extension:x-speakeasy-name-override", "switchKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SwitchOrgEncryptionKey"}`)
	})

	Method("completeSwitch", func() {
		Description("Complete an in-flight key switch now instead of waiting for the grace period: drop the previous key's wraps. Requires org:admin. After this the previous key is no longer needed to read the organization's data.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(OrgEncryptionConfig)

		HTTP(func() {
			POST("/rpc/orgEncryption.completeSwitch")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "completeOrgEncryptionKeySwitch")
		Meta("openapi:extension:x-speakeasy-name-override", "completeSwitch")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CompleteOrgEncryptionKeySwitch"}`)
	})

	Method("rollbackSwitch", func() {
		Description("Roll back an in-flight key switch: return to the previous key and drop the wraps made under the new one. Requires org:admin.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(OrgEncryptionConfig)

		HTTP(func() {
			POST("/rpc/orgEncryption.rollbackSwitch")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "rollbackOrgEncryptionKeySwitch")
		Meta("openapi:extension:x-speakeasy-name-override", "rollbackSwitch")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RollbackOrgEncryptionKeySwitch"}`)
	})

	Method("rotateDataKey", func() {
		Description("Replace a use case's data encryption key with a new one. Requires org:admin. Values encrypted before the rotation stay readable and are re-encrypted under the new key when next written.")

		Payload(func() {
			Attribute("use_case", String, "The use case whose data encryption key to rotate.")
			Required("use_case")
			security.SessionPayload()
			Meta("openapi:typename", "RotateOrgEncryptionDataKeyRequestBody")
		})

		Result(OrgEncryptionConfig)

		HTTP(func() {
			POST("/rpc/orgEncryption.rotateDataKey")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "rotateOrgEncryptionDataKey")
		Meta("openapi:extension:x-speakeasy-name-override", "rotateDataKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RotateOrgEncryptionDataKey"}`)
	})

	Method("resume", func() {
		Description("Resume a suspended organization after its key was fixed. Requires org:admin. Speakeasy first proves the key unwraps every data encryption key; the hourly health probe also resumes on its own once the key works again.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(OrgEncryptionConfig)

		HTTP(func() {
			POST("/rpc/orgEncryption.resume")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "resumeOrgEncryption")
		Meta("openapi:extension:x-speakeasy-name-override", "resume")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ResumeOrgEncryption"}`)
	})
})
