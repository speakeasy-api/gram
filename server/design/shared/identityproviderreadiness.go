package shared

import . "goa.design/goa/v3/dsl"

var IdentityProviderReadiness = Type("IdentityProviderReadiness", func() {
	Meta("struct:pkg:path", "types")

	Attribute("provider", String, "Identity provider supported by the guided setup.", func() { Enum("okta") })
	Attribute("eligible", Boolean, "Whether guided setup is available.")
	Attribute("checks", ArrayOf(IdentityProviderReadinessCheck), "Independent readiness checks in evaluation order.")
	Attribute("checked_at", String, func() { Format(FormatDateTime) })
	Required("provider", "eligible", "checks", "checked_at")
})

var IdentityProviderReadinessCheck = Type("IdentityProviderReadinessCheck", func() {
	Meta("struct:pkg:path", "types")

	Attribute("key", String, "Stable readiness check key.", func() {
		Enum(
			"workos_organization_linked",
			"workos_domain_verified",
			"directory_handoff_stored",
			"workos_directory_created",
			"connections_api_available",
			"sso_feature_enabled",
			"scim_feature_enabled",
		)
	})
	Attribute("ok", Boolean, "Whether the readiness check passed.")
	Attribute("detail", String, "Customer-safe explanation of the result.")
	Attribute("remedy", String, "Staff action needed to make the check pass.")
	Attribute("owner", String, "Team responsible for the check.", func() { Enum("platform_admin", "customer", "speakeasy") })
	Attribute("checked_at", String, func() { Format(FormatDateTime) })
	Required("key", "ok", "detail", "remedy", "owner", "checked_at")
})
