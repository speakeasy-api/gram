package shared

import (
	. "goa.design/goa/v3/dsl"
)

var emaProviderKind = func() {
	Enum("external", "speakeasy")
}

// EMAProviderInput names one provider a vendor accepts.
var EMAProviderInput = Type("EMAProviderInput", func() {
	Meta("struct:pkg:path", "types")
	Description("One EMA provider allowed to issue ID-JAGs for a vendor.")
	Attribute("kind", String, "external is an organization-level remote session issuer that exchanges the user's identity for an ID-JAG, such as an Okta authorization server; speakeasy is the organization's internal session issuer.", emaProviderKind)
	Attribute("remote_session_issuer_id", String, "Required for external, omitted for speakeasy: an organization-level issuer that an organization user session issuer trusts.", func() {
		Format(FormatUUID)
	})
	Required("kind")
})

// EMAProvider is one provider a vendor accepts.
var EMAProvider = Type("EMAProvider", func() {
	Meta("struct:pkg:path", "types")
	Description("One EMA provider a vendor accepts.")
	Attribute("kind", String, "external or speakeasy.", emaProviderKind)
	Attribute("remote_session_issuer_id", String, "The external provider's remote session issuer. Omitted for speakeasy, and in the organization-wide default order, where an external entry stands for each endpoint's own trusted issuer.", func() {
		Format(FormatUUID)
	})
	Attribute("internal_session_issuer_id", String, "The organization's internal session issuer, for speakeasy.", func() {
		Format(FormatUUID)
	})
	Attribute("unavailable_reason", String, "Why the provider cannot serve now; omitted when it can. Identity chaining skips it and tries the next provider. not_trusted_by_endpoint means the endpoint's sign-in doesn't go through this provider, so it can't vouch for people there: an external issuer other than the sign-in's trusted issuer, or Speakeasy on a sign-in federated through an external issuer. readiness_unknown means the Speakeasy issuer's state could not be read just now; retry the read.", func() {
		Enum("deleted", "disabled", "not_trusted_by_endpoint", "signing_unavailable", "not_rolled_out", "verified_domain_required", "setup_required", "keyless", "signing_paused", "sign_in_enforcement_required", "readiness_unknown")
	})
	Attribute("last_accepted_at", String, "When the vendor last accepted an assertion from this provider anywhere in the organization, so the vendor already trusts it, from cached credentials that still hold an access token. Attributed to the exact issuer that vouched; the organization-wide default's external entry takes the newest acceptance of any external issuer. Set on every response that returns an order; omitted when the vendor has never accepted it, when acceptance could not be read, or on readEMA when the caller lacks org:read.", func() {
		Format(FormatDateTime)
	})
	Required("kind")
})

// EMAProviderOrder is the EMA providers a vendor accepts, saved or default.
var EMAProviderOrder = Type("EMAProviderOrder", func() {
	Meta("struct:pkg:path", "types")
	Description("The EMA providers one vendor authorization server accepts, saved or default. They are stored as an ordered list that identity chaining tries in turn, but each endpoint can use only the provider its sign-in goes through, so the list acts as the set of accepted providers.")
	Attribute("vendor_issuer", String, "The vendor authorization server issuer, canonicalized.")
	Attribute("is_default", Boolean, "Nothing is saved, so every provider is accepted. An endpoint whose sign-in trusts an external issuer uses that issuer alone; one that trusts none uses the organization's Speakeasy issuer when it has one.")
	Attribute("providers", ArrayOf(EMAProvider), "The saved providers in their stored order, or the derived default described by is_default.")
	Required("vendor_issuer", "is_default", "providers")
})
