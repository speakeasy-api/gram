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

// CreateExternalKeyFields holds the provider-independent inputs shared by every
// create/update form: the backing credential, algorithm, name, and the optional
// customer grant reference.
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

// CreateAwsKmsKeyForm is the input for creating (or replacing) an AWS KMS key.
var CreateAwsKmsKeyForm = Type("CreateAwsKmsKeyForm", func() {
	Extend(CreateExternalKeyFields)

	Attribute("key_arn", String, "The ARN of the AWS KMS key.")

	Required("key_arn")
})

// CreateGcpKmsKeyForm is the input for creating (or replacing) a GCP KMS key.
var CreateGcpKmsKeyForm = Type("CreateGcpKmsKeyForm", func() {
	Extend(CreateExternalKeyFields)

	Attribute("resource_name", String, "The resource name of the GCP KMS key (projects/.../cryptoKeyVersions/...).")

	Required("resource_name")
})

// ListExternalKeysResult wraps the generic, supertype-only list items.
var ListExternalKeysResult = Type("ListExternalKeysResult", func() {
	Attribute("keys", ArrayOf(ExternalKeySummary), "The organization's external keys.")
	Required("keys")
})
