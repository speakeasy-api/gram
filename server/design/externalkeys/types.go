package externalkeys

import (
	. "goa.design/goa/v3/dsl"
)

// ExternalKeySummary holds the common, provider-independent fields that live on
// the external_keys supertype. It is the item shape returned by the generic list
// endpoint and is embedded into each provider result type.
var ExternalKeySummary = Type("ExternalKeySummary", func() {
	Description("Provider-independent summary of an external key.")

	Attribute("id", String, "The ID of the external key.", func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "The organization that owns the key.")
	Attribute("external_credential_id", String, "The external credential Gram uses to authenticate to the key.", func() {
		Format(FormatUUID)
	})
	Attribute("provider", String, "The cloud KMS provider of the key.", func() {
		Enum("aws_kms", "gcp_kms")
	})
	Attribute("algorithm", String, "The signing algorithm of the key.", func() {
		Enum("RS256", "ES256")
	})
	Attribute("name", String, "A human-readable name for the key.")
	Attribute("customer_grant_reference", String, "The Gram identity (GCP service-account email or AWS principal ARN) the customer granted on the key for the key-policy / IAM-grant model. Not a secret.")
	Attribute("created_at", String, func() {
		Description("When the key was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the key was last updated.")
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "external_credential_id", "provider", "algorithm", "name", "created_at", "updated_at")
})

// AwsKmsKey is the full detail of an AWS KMS external key.
var AwsKmsKey = Type("AwsKmsKey", func() {
	Description("An AWS KMS external key.")

	Extend(ExternalKeySummary)

	Attribute("key_arn", String, "The ARN of the AWS KMS key.")

	Required("key_arn")
})

// GcpKmsKey is the full detail of a GCP KMS external key.
var GcpKmsKey = Type("GcpKmsKey", func() {
	Description("A GCP KMS external key.")

	Extend(ExternalKeySummary)

	Attribute("resource_name", String, "The resource name of the GCP KMS key.")

	Required("resource_name")
})

// CreateExternalKeyFields holds the provider-independent inputs shared by the
// create forms: the backing credential, algorithm, name, and the optional
// customer grant reference. The update forms deliberately do not extend this —
// see UpdateAwsKmsKeyForm.
var CreateExternalKeyFields = Type("CreateExternalKeyFields", func() {
	Attribute("external_credential_id", String, "The external credential Gram uses to authenticate to the key. Must belong to the same organization and matching cloud family (an aws_kms key requires an aws_iam credential; a gcp_kms key requires a gcp_iam credential).", func() {
		Format(FormatUUID)
	})
	Attribute("algorithm", String, "The signing algorithm of the key.", func() {
		Enum("RS256", "ES256")
	})
	Attribute("name", String, "A human-readable name for the key.")
	Attribute("customer_grant_reference", String, "Optional. The Gram identity (GCP service-account email or AWS principal ARN) the customer granted on the key for the key-policy / IAM-grant model. Not a secret.")

	Required("external_credential_id", "algorithm", "name")
})

// CreateAwsKmsKeyForm is the input for creating an AWS KMS key.
var CreateAwsKmsKeyForm = Type("CreateAwsKmsKeyForm", func() {
	Extend(CreateExternalKeyFields)

	Attribute("key_arn", String, "The ARN of the AWS KMS key.")

	Required("key_arn")
})

// CreateGcpKmsKeyForm is the input for creating a GCP KMS key.
var CreateGcpKmsKeyForm = Type("CreateGcpKmsKeyForm", func() {
	Extend(CreateExternalKeyFields)

	Attribute("resource_name", String, "The resource name of the GCP KMS key (projects/.../cryptoKeyVersions/...).")

	Required("resource_name")
})

// UpdateAwsKmsKeyForm is the input for updating an AWS KMS key's mutable
// configuration. It is deliberately a strict subset of CreateAwsKmsKeyForm
// rather than an extension of it: a key's provider identity (key_arn) and its
// algorithm are set at creation and never change, because an external_keys row
// must identify exactly one signable key permanently. A published JWK pins its
// kid to the row it was minted from, so re-pointing the row would silently make
// every already-published kid sign with the wrong key. Changing what a key is
// means delete and recreate, which mints a new id.
//
// external_credential_id stays editable on purpose: repairing which credential
// reaches a key does not change the key material being signed with. Note the
// guarantee is narrower than it looks — validateBackingCredential only checks
// the cloud family (aws_kms needs aws_iam, gcp_kms needs gcp_iam), so a
// credential for a different AWS account or GCP project still passes.
//
// Within that subset the update replaces rather than patches: the optional
// customer_grant_reference is cleared when omitted. That is what makes clearing
// it possible at all, since an absent field and an explicit null are
// indistinguishable once decoded.
//
// The AWS and GCP update forms spell their attributes out separately instead of
// extending a shared parent, which is deliberate and worth not "cleaning up".
// A shared parent forces one set of descriptions on both providers, and Extend
// does not let a child override an inherited attribute — the parent's definition
// wins. Writing them out is what allows the provider-specific wording, which
// documents each provider better than one sentence covering both.
//
// Separately, the two forms are structurally identical, and Goa's OpenAPI
// emitter deduplicates request bodies by shape, so the update methods carry an
// explicit `openapi:typename` to keep their generated schemas (and so their SDK
// types) distinct. Descriptions alone do not break that tie.
var UpdateAwsKmsKeyForm = Type("UpdateAwsKmsKeyForm", func() {
	Attribute("external_credential_id", String, "The external credential Gram uses to authenticate to the key. Must be an aws_iam credential belonging to the same organization.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "A human-readable name for the key.")
	Attribute("customer_grant_reference", String, "Optional. The AWS principal ARN the customer granted on the key in its key policy. Not a secret.")

	Required("external_credential_id", "name")
})

// UpdateGcpKmsKeyForm is the input for updating a GCP KMS key's mutable
// configuration. See UpdateAwsKmsKeyForm for why the two forms do not share a
// parent type.
var UpdateGcpKmsKeyForm = Type("UpdateGcpKmsKeyForm", func() {
	Attribute("external_credential_id", String, "The external credential Gram uses to authenticate to the key. Must be a gcp_iam credential belonging to the same organization.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "A human-readable name for the key.")
	Attribute("customer_grant_reference", String, "Optional. The Gram service-account email the customer granted on the key in an IAM binding. Not a secret.")

	Required("external_credential_id", "name")
})

// VerifyKmsKeyResult is the outcome of a live probe that Gram can reach an
// external key and use it to sign. It is ephemeral and never persisted.
//
// A probe that reaches the provider and is refused is a reportable outcome
// rather than a request error, because almost every negative result here names
// something the key's owner can fix. probe_outcome is what callers branch on;
// detail explains the result to a human and may carry provider error text.
//
// The field is named probe_outcome rather than a bare "outcome", "reason", or
// "status" because Speakeasy hoists enum attribute names into top-level SDK type
// names on a first-come-first-served basis, so a generic one collides with an
// unrelated endpoint that happens to pick the same word.
var VerifyKmsKeyResult = Type("VerifyKmsKeyResult", func() {
	Description("Result of a live probe that Gram can reach an external key and use it to sign.")

	Attribute("verified", Boolean, "Whether the key produced a signature that validated against its own public half.")
	Attribute("probe_outcome", String, "The machine-readable outcome of the probe.", func() {
		Enum(
			"verified",
			"credential_deleted",
			"credential_unusable",
			"invalid_resource_name",
			"key_not_found",
			"permission_denied",
			"key_unusable",
			"unsupported_algorithm",
			"algorithm_mismatch",
			"signature_invalid",
			"unavailable",
			"unexpected",
		)
	})
	Attribute("detail", String, "Human-readable detail about the probe outcome, including the failure reason when it did not verify.")

	Required("verified", "probe_outcome")
})

// ListExternalKeysResult wraps the generic, supertype-only list items.
var ListExternalKeysResult = Type("ListExternalKeysResult", func() {
	Attribute("keys", ArrayOf(ExternalKeySummary), "The organization's external keys.")
	Required("keys")
})
