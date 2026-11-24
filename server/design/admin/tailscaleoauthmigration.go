package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminTailscaleOAuthMigrationOrganization = Type("AdminTailscaleOAuthMigrationOrganization", func() {
	Description("Where one organization stands in moving its saved Tailscale OAuth clients from the platform key to its own organization key (the tailscale-oauth-clients use case).")
	Required("organization_id", "name", "slug", "state", "generation", "waiting", "unreadable", "platform_copies")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("name", String, "The organization name.")
	Attribute("slug", String, "The organization slug.")
	Attribute("state", String, "The transition state.", func() {
		Enum("platform_key", "dual_write", "org_kms")
	})
	Attribute("generation", Int64, "The transition generation; 0 before the organization is enrolled.")
	Attribute("state_changed_at", String, "When the current state was entered. Absent before the organization is enrolled.", func() { Format(FormatDateTime) })
	Attribute("waiting", Int64, "Saved credentials with only a platform-key copy, which the backfill still has to seal with the organization key.")
	Attribute("unreadable", Int64, "Saved credentials whose platform-key copy does not decrypt. They do not block finalize; clearing the platform-key copies destroys them.")
	Attribute("platform_copies", Int64, "Saved credentials that still hold a platform-key copy.")
	Attribute("last_backfill_started_at", String, "When the last backfill started.", func() { Format(FormatDateTime) })
	Attribute("last_backfill_completed_at", String, "When the last backfill ended.", func() { Format(FormatDateTime) })
	Attribute("last_backfill_error", String, "Why the last backfill stopped early. Never carries values or key material.")
	Attribute("last_verified_at", String, "When the last clean verification pass started.", func() { Format(FormatDateTime) })
	Attribute("platform_copies_cleared_at", String, "When the platform-key copies were destroyed after finalize.", func() { Format(FormatDateTime) })
	Attribute("verification_failures", Int64, "Only returned by the backfill step: credentials whose two copies did not match, or whose organization-key copy did not open, in the verification that step just ran. Above zero means no clean verification was recorded, so finalize stays blocked.")
})

var AdminTailscaleOAuthMigrationOrganizations = Type("AdminTailscaleOAuthMigrationOrganizations", func() {
	Required("organizations", "minimum_dual_write_seconds")

	Attribute("organizations", ArrayOf(AdminTailscaleOAuthMigrationOrganization), "Every organization with saved Tailscale OAuth credentials, by name.")
	Attribute("minimum_dual_write_seconds", Int64, "How long an organization must stay in dual_write before it can be finalized.")
})

// MCP parity: not exposed through Staff Admin MCP. This is a temporary,
// one-off migration of a handful of organizations, run by a person from the
// admin dashboard and removed once every organization has moved.
func tailscaleOAuthMigrationMethods() {
	Method("listTailscaleOAuthMigration", func() {
		Description("Lists every organization with saved Tailscale OAuth credentials and where it stands in moving them to its organization key.")

		Payload(func() {
			security.AdminAuthPayload()
		})

		Result(AdminTailscaleOAuthMigrationOrganizations)
		declareUnavailable()

		HTTP(func() {
			GET("/admin/tailscaleOAuthMigration.organizations")
			Response(StatusOK)
			declareUnavailableResponse()
		})

		Meta("openapi:operationId", "adminListTailscaleOAuthMigration")
		Meta("openapi:extension:x-speakeasy-name-override", "listTailscaleOAuthMigration")
	})

	Method("runTailscaleOAuthMigrationStep", func() {
		Description("Runs one step of moving an organization's saved Tailscale OAuth credentials to its organization key. enroll provisions the organization key and starts dual_write; backfill seals every waiting credential with the organization key and verifies both copies; finalize moves to org_kms once the backfill and verification are clean and the minimum time in dual_write has passed; clear_platform_copies then destroys the platform-key copies; rollback returns a dual_write organization to platform_key. Each step is safe to run again.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id", "step")

			Attribute("organization_id", String, "The organization ID.")
			Attribute("step", String, "The step to run.", func() {
				Enum("enroll", "backfill", "finalize", "clear_platform_copies", "rollback")
			})

			// Goa's OpenAPI emitter deduplicates request bodies by shape, so
			// without an explicit typename this body takes another method's
			// name in the generated SDK.
			Meta("openapi:typename", "RunTailscaleOAuthMigrationStepRequestBody")
		})

		Result(AdminTailscaleOAuthMigrationOrganization)
		declareUnavailable()

		HTTP(func() {
			POST("/admin/tailscaleOAuthMigration.run")
			Response(StatusOK)
			declareUnavailableResponse()
		})

		Meta("openapi:operationId", "adminRunTailscaleOAuthMigrationStep")
		Meta("openapi:extension:x-speakeasy-name-override", "runTailscaleOAuthMigrationStep")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminRunTailscaleOAuthMigrationStep"}`)
	})
}
