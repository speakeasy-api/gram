package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminMarketplaceTokenBackfill = Type("AdminMarketplaceTokenBackfill", func() {
	Description("The plugin marketplace token backfill: gives each GitHub connection its project's organization, and moves each plaintext marketplace token to its hash and a sealed copy. Counts never name organizations.")
	Required("attributed", "moved", "skipped", "needs_organization_key", "unattributed", "legacy", "hashed")

	Attribute("attributed", Int64, "Connections this run gave an organization. Zero in a preview.")
	Attribute("moved", Int64, "Plaintext tokens this run moved. Zero in a preview.")
	Attribute("skipped", Int64, "Plaintext tokens that changed between the read and the write, such as when another run moved them first. Zero in a preview.")
	Attribute("needs_organization_key", Int64, "Plaintext tokens left in place because their organization's transition state needs organization encryption this process does not have. Zero in a preview.")
	Attribute("unattributed", Int64, "Connections without an organization. After an apply, these are connections whose project's organization has no metadata row.")
	Attribute("legacy", Int64, "Connections that still hold a plaintext token.")
	Attribute("hashed", Int64, "Connections whose token is stored as a hash and sealed copies.")
})

var AdminRunnerKeyOrganizationBackfill = Type("AdminRunnerKeyOrganizationBackfill", func() {
	Description("The function runner key organization backfill: sets functions_access.organization_id from each runner key's project. Counts include soft-deleted runner keys and never name organizations.")
	Required("filled", "pending", "missing_organization")

	Attribute("filled", Int64, "Runner keys this run gave an organization. Zero in a preview.")
	Attribute("pending", Int64, "Runner keys without an organization whose project's organization exists. An apply fills these.")
	Attribute("missing_organization", Int64, "Runner keys without an organization whose project is gone or whose project's organization has no metadata row. They stay without one.")
})

var AdminSecretBackfills = Type("AdminSecretBackfills", func() {
	Description("What each secret backfill would do if applied now.")
	Required("marketplace_tokens", "runner_key_organizations")

	Attribute("marketplace_tokens", AdminMarketplaceTokenBackfill, "A preview of the marketplace token backfill.")
	Attribute("runner_key_organizations", AdminRunnerKeyOrganizationBackfill, "A preview of the runner key organization backfill.")
	Attribute("platform_key_fingerprint", String, "The fingerprint of the platform key this admin process seals marketplace tokens with. It must match the fingerprint every server and worker logs at startup as \"encryption key loaded\". Absent when this process has no platform key, so the marketplace token backfill cannot be applied.")
})

// MCP parity: not exposed through Staff Admin MCP. These are temporary,
// fleet-wide backfills run once per environment by a person from the admin
// dashboard, and the page and endpoints are removed once both are done. Admin
// MCP writes need one exact target, which a fleet-wide backfill does not have.
func secretBackfillMethods() {
	Method("getSecretBackfills", func() {
		Description("Previews the temporary secret backfills: what applying each would change now. Writes nothing.")

		Payload(func() {
			security.AdminAuthPayload()
		})

		Result(AdminSecretBackfills)

		HTTP(func() {
			GET("/admin/secretBackfills.get")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetSecretBackfills")
		Meta("openapi:extension:x-speakeasy-name-override", "getSecretBackfills")
	})

	Method("runMarketplaceTokenBackfill", func() {
		Description("Applies the marketplace token backfill: gives each GitHub connection its project's organization, then moves each plaintext marketplace token to its hash and the copies its organization's transition state writes, sealed with this process's platform key. Install URLs do not change. Safe to run again; an interrupted run continues where it stopped. Refused while another run of it is in progress, and when this process's platform key fingerprint is not the one the operator checked.")

		Payload(func() {
			security.AdminAuthPayload()
			Attribute("platform_key_fingerprint", String, "The platform key fingerprint the operator checked against the servers' \"encryption key loaded\" log. The run is refused when this process's platform key has another fingerprint, such as after a restart with a different key.", func() {
				MinLength(1)
			})
			Required("platform_key_fingerprint")
		})

		Result(AdminMarketplaceTokenBackfill)
		declareUnavailable()

		HTTP(func() {
			POST("/admin/secretBackfills.runMarketplaceTokens")
			Response(StatusOK)
			declareUnavailableResponse()
		})

		Meta("openapi:operationId", "adminRunMarketplaceTokenBackfill")
		Meta("openapi:extension:x-speakeasy-name-override", "runMarketplaceTokenBackfill")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminRunMarketplaceTokenBackfill"}`)
	})

	Method("runRunnerKeyOrganizationBackfill", func() {
		Description("Applies the runner key organization backfill: sets functions_access.organization_id from each runner key's project, in batches that each commit on their own. Runner keys and deployed function runners do not change. Safe to run again; an interrupted run continues where it stopped. Refused while another run of it is in progress.")

		Payload(func() {
			security.AdminAuthPayload()
		})

		Result(AdminRunnerKeyOrganizationBackfill)

		HTTP(func() {
			POST("/admin/secretBackfills.runRunnerKeyOrganizations")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminRunRunnerKeyOrganizationBackfill")
		Meta("openapi:extension:x-speakeasy-name-override", "runRunnerKeyOrganizationBackfill")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminRunRunnerKeyOrganizationBackfill"}`)
	})
}
