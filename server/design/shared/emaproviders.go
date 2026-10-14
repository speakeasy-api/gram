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
	Attribute("unavailable_reason", String, "Why the provider cannot serve now; omitted when it can. Unavailable providers are skipped.", func() {
		Enum("deleted", "disabled", "not_trusted_by_endpoint", "signing_unavailable")
	})
	Required("kind")
})

// EMAProviderOrder is a vendor's EMA provider order, saved or default.
var EMAProviderOrder = Type("EMAProviderOrder", func() {
	Meta("struct:pkg:path", "types")
	Description("The EMA providers identity chaining tries, in order, for one vendor authorization server; each failure falls through to the next.")
	Attribute("vendor_issuer", String, "The vendor authorization server issuer, canonicalized.")
	Attribute("is_default", Boolean, "No order is saved, so every available provider applies: external first, then Speakeasy.")
	Attribute("providers", ArrayOf(EMAProvider), "Providers in the order they are tried.")
	Required("vendor_issuer", "is_default", "providers")
})
