package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminUpstreamOAuthMigrationOrganization = Type("AdminUpstreamOAuthMigrationOrganization", func() {
	Description("An organization the upstream OAuth migration applies to: it has a live MCP server signed in through a user session issuer.")
	Required("id", "name", "slug", "servers")

	Attribute("id", String, "The organization ID.")
	Attribute("name", String, "The organization name.")
	Attribute("slug", String, "The organization slug.")
	Attribute("servers", Int64, "Live MCP servers in the organization signed in through a user session issuer.")
})

var AdminUpstreamOAuthMigrationOrganizations = Type("AdminUpstreamOAuthMigrationOrganizations", func() {
	Required("organizations")

	Attribute("organizations", ArrayOf(AdminUpstreamOAuthMigrationOrganization), "Every organization the migration applies to, by name.")
})

var AdminUpstreamOAuthMigrationServer = Type("AdminUpstreamOAuthMigrationServer", func() {
	Description("An MCP server whose upstream OAuth client the existing configuration does not prove. An administrator must associate its client before the organization is migrated.")
	Required("mcp_server_id", "project_id")

	Attribute("mcp_server_id", String, "The MCP server ID.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "The server's project ID.", func() { Format(FormatUUID) })
})

var AdminUpstreamOAuthMigrationStatus = Type("AdminUpstreamOAuthMigrationStatus", func() {
	Description("Where one organization stands in the upstream OAuth migration, read from a dry run of both backfills. The organization is migrated when nothing is left to write and no server needs configuring. Credentials that must reconnect do not block it: they are asked to sign in again once serving reads their choices.")
	Required("organization_id", "migrated", "servers", "pending_associations", "unresolved_servers", "credentials", "pending_selections", "reconnect_credentials")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("migrated", Boolean, "Whether the organization has nothing left to write and no server needing configuration.")
	Attribute("servers", Int64, "MCP servers on a user session issuer that the association backfill decided.")
	Attribute("pending_associations", Int64, "Server to client associations the configuration proves that are not yet recorded.")
	Attribute("unresolved_servers", ArrayOf(AdminUpstreamOAuthMigrationServer), "Servers whose upstream OAuth client must be associated by an administrator.")
	Attribute("credentials", Int64, "Live user session credentials scanned.")
	Attribute("pending_selections", Int64, "Credential choices the configuration decides that are not yet recorded.")
	Attribute("reconnect_credentials", Int64, "Credentials with at least one server they will be asked to reconnect to, because no single client is decided for them.")
})

var AdminUpstreamOAuthMigrationRun = Type("AdminUpstreamOAuthMigrationRun", func() {
	Description("What one migration run wrote, and the organization's status after it.")
	Required("associations_written", "associations_skipped", "selections_written", "status")

	Attribute("associations_written", Int64, "Server to client associations recorded.")
	Attribute("associations_skipped", Int64, "Associations not recorded because their configuration changed during the run. Run again to pick them up.")
	Attribute("selections_written", Int64, "Credential choices recorded.")
	Attribute("status", AdminUpstreamOAuthMigrationStatus, "The organization's status after the run.")
})

func upstreamOAuthMigrationMethods() {
	Method("listUpstreamOAuthMigrationOrganizations", func() {
		Description("Lists every organization the upstream OAuth migration applies to, so staff can check and migrate them one by one.")

		Payload(func() {
			security.AdminAuthPayload()
		})

		Result(AdminUpstreamOAuthMigrationOrganizations)

		HTTP(func() {
			GET("/admin/upstreamOAuthMigration.organizations")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminListUpstreamOAuthMigrationOrganizations")
		Meta("openapi:extension:x-speakeasy-name-override", "listUpstreamOAuthMigrationOrganizations")
	})

	Method("getUpstreamOAuthMigrationStatus", func() {
		Description("Reports where one organization stands in the upstream OAuth migration from a dry run that writes nothing.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "The organization ID.")
		})

		Result(AdminUpstreamOAuthMigrationStatus)

		HTTP(func() {
			GET("/admin/upstreamOAuthMigration.status")
			Param("organization_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetUpstreamOAuthMigrationStatus")
		Meta("openapi:extension:x-speakeasy-name-override", "getUpstreamOAuthMigrationStatus")
	})

	Method("runUpstreamOAuthMigration", func() {
		Description("Migrates one organization's upstream OAuth configuration: records the server to client associations its configuration proves, then each live credential's client choice. Idempotent, so it is safe to run again; a server the configuration does not prove is reported and never guessed.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "The organization ID.")

			// Goa's OpenAPI emitter deduplicates request bodies by shape, so
			// without an explicit typename this body takes another method's
			// name in the generated SDK.
			Meta("openapi:typename", "RunUpstreamOAuthMigrationRequestBody")
		})

		Result(AdminUpstreamOAuthMigrationRun)

		HTTP(func() {
			POST("/admin/upstreamOAuthMigration.run")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminRunUpstreamOAuthMigration")
		Meta("openapi:extension:x-speakeasy-name-override", "runUpstreamOAuthMigration")
	})
}
