package design

import (
	. "goa.design/goa/v3/dsl"
)

// XaaApp mirrors the dev-idp `xaa_apps` table: a client allowed to ask the
// IdP for an ID-JAG on a user's behalf.
var XaaApp = Type("XaaApp", func() {
	Attribute("id", String, "App UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("client_id", String, "Client id this app authenticates to the token endpoint with (unique).")
	Attribute("client_secret", String, "Client secret; empty registers a public client that authenticates by client_id alone.")
	Attribute("name", String, "Display name.")
	Attribute("enabled", Boolean, "Disabled apps are refused at mint time.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "client_id", "client_secret", "name", "enabled", "created_at", "updated_at")
})

// XaaResource mirrors the dev-idp `xaa_resources` table: one resource
// authorization server, mounted at /resource-as/<slug>.
var XaaResource = Type("XaaResource", func() {
	Attribute("id", String, "Resource UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("slug", String, "Path segment this resource's authorization server is served at (unique).")
	Attribute("name", String, "Display name.")
	Attribute("resource_identifier", String, "The MCP server URL this resource guards; lands in an ID-JAG's `resource` claim.")
	Attribute("issuer", String, "Issuer identifier of this resource's authorization server. Derived from the slug; the value an ID-JAG must carry in `aud`.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "slug", "name", "resource_identifier", "issuer", "created_at", "updated_at")
})

// XaaAppAssignment mirrors the dev-idp `xaa_app_assignments` table: which
// user may drive which app against which resource. The absence of a row is
// the denial.
var XaaAppAssignment = Type("XaaAppAssignment", func() {
	Attribute("id", String, "Assignment UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("app_id", String, "App UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("user_id", String, "User UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("resource_id", String, "Resource UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("granted_scopes", String, "Space-delimited scopes this assignment grants. A mint request is narrowed to these.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "app_id", "user_id", "resource_id", "granted_scopes", "created_at", "updated_at")
})

// XaaTrustRule mirrors the dev-idp `xaa_trust_rules` table: which issuer a
// resource authorization server accepts ID-JAGs from.
var XaaTrustRule = Type("XaaTrustRule", func() {
	Attribute("id", String, "Trust rule UUID.", func() {
		Format(FormatUUID)
	})
	Attribute("resource_id", String, "Resource UUID this rule applies to.", func() {
		Format(FormatUUID)
	})
	Attribute("trusted_issuer", String, "An ID-JAG `iss` value this resource accepts. Need not be this dev-idp's own issuer.")
	Attribute("allowed_client_ids", String, "JSON array of client ids to accept grants for. '[]' means any client the issuer vouched for.")
	Attribute("allowed_scopes", String, "Space-delimited scope ceiling applied on top of the ID-JAG. Empty means no ceiling.")
	Attribute("enabled", Boolean, "Disabled rules are refused at redeem time, distinctly from a missing rule.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "resource_id", "trusted_issuer", "allowed_client_ids", "allowed_scopes", "enabled", "created_at", "updated_at")
})

// XaaIssuedJag mirrors the dev-idp `xaa_issued_jags` ledger: what the IdP
// actually minted. Read-only — the dashboard shows it to explain a policy
// decision after the fact.
var XaaIssuedJag = Type("XaaIssuedJag", func() {
	Attribute("jti", String, "The grant's unique id, as it appears in the ID-JAG's `jti` claim.")
	Attribute("app_id", String, "App UUID that requested it.", func() {
		Format(FormatUUID)
	})
	Attribute("user_id", String, "User UUID it was minted for.", func() {
		Format(FormatUUID)
	})
	Attribute("resource_id", String, "Resource UUID it names.", func() {
		Format(FormatUUID)
	})
	Attribute("scope", String, "Space-delimited scopes actually granted.")
	Attribute("expires_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})

	Required("jti", "app_id", "user_id", "resource_id", "scope", "expires_at", "created_at")
})
