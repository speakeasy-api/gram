package orgencryption

import (
	. "goa.design/goa/v3/dsl"
)

// OrgEncryptionConfig is the organization's encryption configuration. It names
// keys by id and never carries key material.
var OrgEncryptionConfig = Type("OrgEncryptionConfig", func() {
	Description("Which key encryption key protects the organization's data, and whether it is usable.")

	Attribute("id", String, "The ID of the configuration.", func() {
		Format(FormatUUID)
	})
	Attribute("key_mode", String, "Who controls the current key.", func() {
		Enum("speakeasy_managed", "customer_managed")
	})
	Attribute("config_state", String, "active: one key protects all data. switching: data is readable under both the current and previous key. suspended: the key is unusable and every encrypt and decrypt fails.", func() {
		Enum("active", "switching", "suspended")
	})
	Attribute("key_id", String, "The ID of the current key encryption key.", func() {
		Format(FormatUUID)
	})
	Attribute("previous_key_id", String, "The ID of the key being switched away from, while a switch is in flight.", func() {
		Format(FormatUUID)
	})
	Attribute("switch_started_at", String, "When the in-flight or last switch started.", func() {
		Format(FormatDateTime)
	})
	Attribute("switch_completed_at", String, "When the last switch completed.", func() {
		Format(FormatDateTime)
	})
	Attribute("suspended_at", String, "When the configuration was suspended.", func() {
		Format(FormatDateTime)
	})
	Attribute("suspended_reason", String, "The provider error that caused the suspension.")
	Attribute("last_check_at", String, "When the hourly health probe last checked the keys.", func() {
		Format(FormatDateTime)
	})
	Attribute("last_check_error", String, "The error from the last health probe, when it failed.")

	Required("id", "key_mode", "config_state", "key_id")
})

// OrgEncryptionKey is one key encryption key the organization can use.
var OrgEncryptionKey = Type("OrgEncryptionKey", func() {
	Description("A symmetric KMS key that can protect the organization's data.")

	Attribute("id", String, "The ID of the key.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "A human-readable name for the key.")
	Attribute("provider", String, "The cloud KMS provider of the key.", func() {
		Enum("gcp_kms")
	})
	Attribute("resource_name", String, "The GCP KMS crypto key resource name (projects/.../cryptoKeys/...).")
	Attribute("managed", Boolean, "Whether Speakeasy minted and controls the key.")
	Attribute("external_credential_id", String, "The external credential Speakeasy reaches the key with. Omitted for Speakeasy-managed keys.", func() {
		Format(FormatUUID)
	})
	Attribute("customer_grant_reference", String, "The Speakeasy principal the customer granted roles/iam.serviceAccountTokenCreator on the credential's service account. Not a secret.")
	Attribute("created_at", String, "When the key was registered.", func() {
		Format(FormatDateTime)
	})

	Required("id", "name", "provider", "resource_name", "managed", "created_at")
})

// ListOrgEncryptionKeysResult wraps the organization's encryption keys.
var ListOrgEncryptionKeysResult = Type("ListOrgEncryptionKeysResult", func() {
	Attribute("keys", ArrayOf(OrgEncryptionKey), "The organization's encryption keys, newest first.")

	Required("keys")
})

// CreateOrgEncryptionGcpKeyForm registers a customer-managed GCP KMS key.
var CreateOrgEncryptionGcpKeyForm = Type("CreateOrgEncryptionGcpKeyForm", func() {
	Attribute("external_credential_id", String, "The organization's gcp_iam external credential Speakeasy reaches the key with.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "A human-readable name for the key.")
	Attribute("resource_name", String, "The GCP KMS crypto key resource name, without a version: projects/<p>/locations/<l>/keyRings/<r>/cryptoKeys/<k>. The key must have purpose ENCRYPT_DECRYPT.")
	Attribute("customer_grant_reference", String, "Optional. The Speakeasy principal, as reported by externalCredentials.getGcpSetupInfo, that the customer granted roles/iam.serviceAccountTokenCreator on the credential's service account. That service account, in the customer's project, is what holds roles/cloudkms.cryptoKeyEncrypterDecrypter on the key; Speakeasy is never granted on the key directly. Not a secret.")

	Required("external_credential_id", "name", "resource_name")
})

// VerifyOrgEncryptionKeyResult is the outcome of an encrypt-then-decrypt probe.
// The enum attribute is named key_probe_outcome to keep its hoisted SDK type
// name distinct from the signing key probe's.
var VerifyOrgEncryptionKeyResult = Type("VerifyOrgEncryptionKeyResult", func() {
	Description("Result of a live probe that Speakeasy can encrypt and decrypt with a key.")

	Attribute("verified", Boolean, "Whether the key encrypted the probe payload and decrypted it again.")
	Attribute("key_probe_outcome", String, "The machine-readable outcome of the probe.", func() {
		Enum(
			"verified",
			"credential_unusable",
			"invalid_resource_name",
			"key_not_found",
			"permission_denied",
			"key_unusable",
			"round_trip_failed",
			"unavailable",
			"unexpected",
		)
	})
	Attribute("detail", String, "Human-readable detail about the probe outcome, including the failure reason when it did not verify.")

	Required("verified", "key_probe_outcome")
})
