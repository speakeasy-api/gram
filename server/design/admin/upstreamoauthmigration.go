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
	Description("Where one organization stands in the upstream OAuth migration, read from a dry run of its backfills. The organization is migrated when nothing is left to write and no server needs configuring. Credentials that must reconnect do not block it: they are asked to sign in again once serving reads their choices. Typed auth clients are reported separately in auth_clients and never affect migrated.")
	Required("organization_id", "migrated", "servers", "pending_associations", "unresolved_servers", "credentials", "pending_selections", "reconnect_credentials")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("migrated", Boolean, "Whether the organization has nothing left to write and no server needing configuration.")
	Attribute("servers", Int64, "MCP servers on a user session issuer that the association backfill decided.")
	Attribute("pending_associations", Int64, "Server to client associations the configuration proves that are not yet recorded.")
	Attribute("unresolved_servers", ArrayOf(AdminUpstreamOAuthMigrationServer), "Servers whose upstream OAuth client must be associated by an administrator.")
	Attribute("credentials", Int64, "Live user session credentials scanned.")
	Attribute("pending_selections", Int64, "Credential choices the configuration decides that are not yet recorded.")
	Attribute("reconnect_credentials", Int64, "Credentials with at least one server they will be asked to reconnect to, because no single client is decided for them.")
	Attribute("auth_clients", AdminUpstreamOAuthMigrationAuthClients, "Where the organization stands in recording typed auth clients. Separate from migrated: it never gates the upstream OAuth cutover. Absent when its check failed.")
})

var AdminUpstreamOAuthMigrationUnresolvedAssociation = Type("AdminUpstreamOAuthMigrationUnresolvedAssociation", func() {
	Description("A server to upstream OAuth client association the typed auth client backfill cannot decide: the client's owner is unknown, or the auth client already recorded for it has another type or belongs to another project.")
	Required("association_id", "mcp_server_id", "project_id", "remote_session_client_id")

	Attribute("association_id", String, "The server to upstream OAuth client association ID.", func() { Format(FormatUUID) })
	Attribute("mcp_server_id", String, "The MCP server ID.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "The server's project ID.", func() { Format(FormatUUID) })
	Attribute("remote_session_client_id", String, "The upstream OAuth client ID.", func() { Format(FormatUUID) })
	Attribute("auth_client_id", String, "The auth client already recorded for the client, if any.", func() { Format(FormatUUID) })
})

var AdminUpstreamOAuthMigrationAuthClients = Type("AdminUpstreamOAuthMigrationAuthClients", func() {
	Description("Where one organization stands in recording a typed auth client for every upstream OAuth client its servers are associated with, and in retiring backfilled records whose association or client is gone. Nothing reads these records yet.")
	Required("complete", "associations", "pending_clients", "pending_links", "pending_retirements", "pending_marked_servers", "unresolved", "unresolved_associations")

	Attribute("complete", Boolean, "Whether every association has its auth client and server link recorded, no backfilled record is left to retire, and no server offering one is left undecided.")
	Attribute("associations", Int64, "Live server to upstream OAuth client associations decided.")
	Attribute("pending_clients", Int64, "Auth clients not yet recorded.")
	Attribute("pending_links", Int64, "Server to auth client links not yet recorded.")
	Attribute("pending_retirements", Int64, "Backfilled auth clients and server links whose association or upstream client is gone, not yet retired.")
	Attribute("pending_marked_servers", Int64, "Servers offering a backfilled auth client whose need for upstream authentication is still undecided, not yet marked as needing it.")
	Attribute("unresolved", Int64, "Associations whose client cannot be typed and need attention.")
	Attribute("unresolved_associations", ArrayOf(AdminUpstreamOAuthMigrationUnresolvedAssociation), "The associations counted in unresolved.")
})

var AdminUpstreamOAuthMigrationRun = Type("AdminUpstreamOAuthMigrationRun", func() {
	Description("What one migration run wrote, and the organization's status after it.")
	Required("associations_written", "associations_skipped", "selections_written", "status")

	Attribute("associations_written", Int64, "Server to client associations recorded.")
	Attribute("associations_skipped", Int64, "Associations not recorded because their configuration changed during the run. Run again to pick them up.")
	Attribute("selections_written", Int64, "Credential choices recorded.")
	Attribute("auth_clients_written", Int64, "Typed auth clients recorded.")
	Attribute("auth_client_links_written", Int64, "Server to auth client links recorded.")
	Attribute("servers_marked_requiring_upstream_auth", Int64, "Servers marked as needing upstream authentication because they offer a backfilled auth client.")
	Attribute("auth_clients_error", String, "Why recording typed auth clients failed, when it did. The upstream OAuth migration itself is unaffected.")
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
