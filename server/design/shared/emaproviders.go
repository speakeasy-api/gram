package shared

import (
	. "goa.design/goa/v3/dsl"
)

var emaProviderKind = func() {
	Enum("external", "speakeasy")
}

// EMAProviderInput names one provider in a vendor's EMA provider order.
var EMAProviderInput = Type("EMAProviderInput", func() {
	Meta("struct:pkg:path", "types")
	Description("One EMA provider allowed to issue ID-JAGs for a vendor.")
	Attribute("kind", String, "external is an organization-level remote session issuer that exchanges the user's identity for an ID-JAG, such as an Okta authorization server; speakeasy is the organization's internal session issuer.", emaProviderKind)
	Attribute("remote_session_issuer_id", String, "Required for external, omitted for speakeasy: an organization-level issuer that an organization user session issuer trusts.", func() {
		Format(FormatUUID)
	})
	Required("kind")
})

// EMAProvider is one provider in a vendor's EMA provider order.
var EMAProvider = Type("EMAProvider", func() {
	Meta("struct:pkg:path", "types")
	Description("One EMA provider in a vendor's order.")
	Attribute("kind", String, "external or speakeasy.", emaProviderKind)
	Attribute("remote_session_issuer_id", String, "The external provider's remote session issuer. Omitted for speakeasy, and for the endpoint's trusted issuer in a default order with none.", func() {
		Format(FormatUUID)
	})
	Attribute("internal_session_issuer_id", String, "The organization's internal session issuer, for speakeasy.", func() {
		Format(FormatUUID)
	})
	Attribute("unavailable_reason", String, "Why the provider cannot serve now; omitted when it can. Identity chaining skips it and tries the next provider. not_trusted_by_endpoint means the endpoint's authorization server does not accept this issuer. readiness_unknown means the Speakeasy issuer's state could not be read just now; retry the read.", func() {
		Enum("deleted", "disabled", "not_trusted_by_endpoint", "signing_unavailable", "not_rolled_out", "verified_domain_required", "setup_required", "keyless", "signing_paused", "sign_in_enforcement_required", "readiness_unknown")
	})
	Attribute("last_accepted_at", String, "When the vendor last accepted an assertion from this provider for this binding, from the newest live cached credential. Omitted when none has been accepted.", func() {
		Format(FormatDateTime)
	})
	Required("kind")
})

// EMAProviderOrder is a vendor's EMA provider order, saved or default.
var EMAProviderOrder = Type("EMAProviderOrder", func() {
	Meta("struct:pkg:path", "types")
	Description("The EMA providers for one vendor authorization server, in the order identity chaining tries them. Each provider that can serve is tried in turn, with short bounded retries for transient failures, until one succeeds.")
	Attribute("vendor_issuer", String, "The vendor authorization server issuer, canonicalized.")
	Attribute("is_default", Boolean, "No order is saved, so the endpoint uses its configured external issuer.")
	Attribute("providers", ArrayOf(EMAProvider), "The saved providers in order, or the external default.")
	Required("vendor_issuer", "is_default", "providers")
})
