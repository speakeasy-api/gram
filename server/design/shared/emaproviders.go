package shared

import (
	. "goa.design/goa/v3/dsl"
)

var emaProviderKind = func() {
	Enum("external", "speakeasy")
}

// EMAProviderInput names the EMA provider an administrator selects for a vendor.
var EMAProviderInput = Type("EMAProviderInput", func() {
	Meta("struct:pkg:path", "types")
	Description("The EMA provider selected to issue ID-JAGs for a vendor.")
	Attribute("kind", String, "external is an organization-level remote session issuer that exchanges the user's identity for an ID-JAG, such as an Okta authorization server; speakeasy is the organization's internal session issuer.", emaProviderKind)
	Attribute("remote_session_issuer_id", String, "Required for external, omitted for speakeasy: an organization-level issuer that an organization user session issuer trusts.", func() {
		Format(FormatUUID)
	})
	Required("kind")
})

// EMAProvider is the EMA provider serving a vendor.
var EMAProvider = Type("EMAProvider", func() {
	Meta("struct:pkg:path", "types")
	Description("The EMA provider serving a vendor.")
	Attribute("kind", String, "external or speakeasy.", emaProviderKind)
	Attribute("remote_session_issuer_id", String, "The external provider's remote session issuer. Omitted for speakeasy, and for the endpoint's trusted issuer in a default selection with none.", func() {
		Format(FormatUUID)
	})
	Attribute("internal_session_issuer_id", String, "The organization's internal session issuer, for speakeasy.", func() {
		Format(FormatUUID)
	})
	Attribute("unavailable_reason", String, "Why the provider cannot serve now; omitted when it can. An unavailable selection never falls back to another provider. readiness_unknown means the Speakeasy issuer's state could not be read just now; retry the read.", func() {
		Enum("deleted", "disabled", "not_trusted_by_endpoint", "signing_unavailable", "not_rolled_out", "verified_domain_required", "setup_required", "keyless", "signing_paused", "sign_in_enforcement_required", "readiness_unknown")
	})
	Required("kind")
})

// EMAProviderSelection is a vendor's EMA provider, selected or default.
var EMAProviderSelection = Type("EMAProviderSelection", func() {
	Meta("struct:pkg:path", "types")
	Description("The EMA provider for one vendor authorization server. There is no automatic fallback to another provider.")
	Attribute("vendor_issuer", String, "The vendor authorization server issuer, canonicalized.")
	Attribute("is_default", Boolean, "No selection is saved, so the endpoint uses its configured external issuer.")
	Attribute("provider", EMAProvider, "The explicitly selected provider, or the external default.")
	Required("vendor_issuer", "is_default", "provider")
})
