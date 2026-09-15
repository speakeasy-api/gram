package jsonwebkeysets

import (
	. "goa.design/goa/v3/dsl"
)

// JsonWebKeySet is the full detail of a JSON Web Key Set: a named collection of
// published public keys backed by one organization external key at a time.
var JsonWebKeySet = Type("JsonWebKeySet", func() {
	Description("A JSON Web Key Set — a named collection of published public keys backed by an organization external key.")

	Attribute("id", String, "The ID of the key set.", func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "The organization that owns the key set.")
	Attribute("external_key_id", String, "The external key currently backing the set: new keys are published from it. Already-published keys keep signing with the external key they were minted from.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "A human-readable name for the key set.")
	Attribute("created_at", String, func() {
		Description("When the key set was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the key set was last updated.")
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "external_key_id", "name", "created_at", "updated_at")
})

// JsonWebKey is one published key inside a set. The private half stays in the
// customer's KMS; what is published here is the public JWK document and the
// key's lifecycle state.
var JsonWebKey = Type("JsonWebKey", func() {
	Description("A published JSON Web Key inside a key set.")

	Attribute("id", String, "The ID of the published key.", func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "The organization that owns the key.")
	Attribute("json_web_key_set_id", String, "The key set the key is published in.", func() {
		Format(FormatUUID)
	})
	Attribute("external_key_id", String, "The external key the key was minted from. Signing for this kid always uses exactly this key, even after the set is re-pointed at another external key.", func() {
		Format(FormatUUID)
	})
	Attribute("kid", String, "The key ID: the RFC 7638 SHA-256 thumbprint of the public key, base64url-encoded. Carried in the JWK document and in the header of every token signed with the key.")
	// Named key_state rather than a bare "state" because Speakeasy hoists enum
	// attribute names into top-level SDK type names on a first-come-first-served
	// basis, and a generic name collides with an unrelated endpoint that happens
	// to pick the same word.
	Attribute("key_state", String, "The lifecycle state of the key. pending: published for verifier cache warm-up but not signing yet. active: the key new signatures use; at most one per set. retired: no longer signing, still published so outstanding tokens keep verifying. revoked: withdrawn entirely; tokens signed with it stop verifying.", func() {
		Enum("pending", "active", "retired", "revoked")
	})
	Attribute("public_jwk", Any, "The public JWK document (RFC 7517) as published, including kid, alg and use.")
	Attribute("activated_at", String, func() {
		Description("When the key last became the set's active signing key.")
		Format(FormatDateTime)
	})
	Attribute("retired_at", String, func() {
		Description("When the key last left active signing use.")
		Format(FormatDateTime)
	})
	Attribute("revoked_at", String, func() {
		Description("When the key was revoked.")
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() {
		Description("When the key was published.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the key was last updated.")
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "json_web_key_set_id", "external_key_id", "kid", "key_state", "public_jwk", "created_at", "updated_at")
})

// CreateJsonWebKeySetForm is the input for creating a key set. Creation mints
// and publishes the set's first key from the backing external key, so the form
// is the set's full configuration.
var CreateJsonWebKeySetForm = Type("CreateJsonWebKeySetForm", func() {
	Attribute("name", String, "A human-readable name for the key set. Names may repeat within an organization.")
	Attribute("external_key_id", String, "The external key to back the set with. Must be a live, organization-owned GCP KMS key; AWS KMS keys are not supported yet.", func() {
		Format(FormatUUID)
	})

	Required("name", "external_key_id")
})

// UpdateJsonWebKeySetForm is the input for updating a key set's configuration.
// Re-pointing external_key_id is how rotation to a new KMS key version begins:
// point the set at the new external key, then publish a key from it. Both
// fields are replaced, not patched.
var UpdateJsonWebKeySetForm = Type("UpdateJsonWebKeySetForm", func() {
	Attribute("name", String, "A human-readable name for the key set.")
	Attribute("external_key_id", String, "The external key to back the set with from now on. Must be a live, organization-owned GCP KMS key; AWS KMS keys are not supported yet. Already-published keys are unaffected: each keeps signing with the external key it was minted from.", func() {
		Format(FormatUUID)
	})

	Required("name", "external_key_id")
})

// ListJsonWebKeySetsResult wraps the organization's key sets.
var ListJsonWebKeySetsResult = Type("ListJsonWebKeySetsResult", func() {
	Attribute("sets", ArrayOf(JsonWebKeySet), "The organization's JSON Web Key Sets.")
	Required("sets")
})

// ListJsonWebKeysResult wraps one set's published keys.
var ListJsonWebKeysResult = Type("ListJsonWebKeysResult", func() {
	Attribute("keys", ArrayOf(JsonWebKey), "The set's published keys.")
	Required("keys")
})

// JsonWebKeySetDeletePreflight is the impact summary the dashboard shows before
// deleting a set. Unlike the remote_session_client delete preflight, which is
// purely informational because that delete cascades, this one predicts a real
// refusal: deleteSet returns a conflict whenever client_count is non-zero.
var JsonWebKeySetDeletePreflight = Type("JsonWebKeySetDeletePreflight", func() {
	Attribute("client_count", Int, "Number of live remote_session_clients referencing this set. Deleting the set is refused while this is non-zero.")
	// remote_session_clients has no display-name column, so the closest thing to
	// a label is the OAuth client_id the counterparty issued.
	Attribute("client_ids", ArrayOf(String), "The client_id values of the referencing remote_session_clients, oldest first. Truncated when client_count exceeds the listing cap.")
	Required("client_count", "client_ids")
})
