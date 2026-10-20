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
	Description("Configuration the typed auth client backfill cannot record without a guess, and why. ID fields that do not apply to its kind are the nil UUID.")
	Required("kind", "reason", "subject_id", "association_id", "mcp_server_id", "project_id", "remote_session_client_id", "binding_ids", "source_ids", "server_ids")

	Attribute("kind", String, "What was decided: oauth, header or ema.")
	Attribute("reason", String, "Why it cannot be recorded: unknown_owner, type_mismatch, project_mismatch, multiple_authorization_headers, duplicate_header_names, denied_source, mixed_credential_headers, oauth_client_shared_across_sources, conflicting_scopes, resource_variants, resource_not_matched, issuer_not_allowed or registration_out_of_reach; for a warning, server_not_matched.")
	Attribute("subject_id", String, "The association, source, upstream OAuth client or first binding decided.", func() { Format(FormatUUID) })
	Attribute("association_id", String, "The server to upstream OAuth client association ID, for an oauth association.", func() { Format(FormatUUID) })
	Attribute("mcp_server_id", String, "The MCP server ID, when one server is concerned.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "The project the configuration belongs to.", func() { Format(FormatUUID) })
	Attribute("remote_session_client_id", String, "The upstream OAuth client ID, for oauth and ema.", func() { Format(FormatUUID) })
	Attribute("binding_ids", ArrayOf(String, func() { Format(FormatUUID) }), "Every identity chaining binding decided together, for ema.")
	Attribute("source_ids", ArrayOf(String, func() { Format(FormatUUID) }), "The sources concerned, for an upstream OAuth client shared across sources.")
	Attribute("server_ids", ArrayOf(String, func() { Format(FormatUUID) }), "The servers concerned, for an upstream OAuth client shared across sources.")
	Attribute("auth_client_id", String, "The auth client already recorded, if any.", func() { Format(FormatUUID) })
})

var AdminUpstreamOAuthMigrationAuthClientKind = Type("AdminUpstreamOAuthMigrationAuthClientKind", func() {
	Description("Where one kind of configured upstream identity stands.")
	Required("kind", "decided", "pending_clients", "pending_links", "pending_headers", "pending_retirements", "unresolved")

	Attribute("kind", String, "The kind: oauth, header or ema.")
	Attribute("decided", Int64, "Configuration and server links decided.")
	Attribute("pending_clients", Int64, "Auth clients not yet recorded.")
	Attribute("pending_links", Int64, "Server to auth client links not yet recorded.")
	Attribute("pending_headers", Int64, "Header copies and EMA scope sets not yet up to date.")
	Attribute("pending_retirements", Int64, "Backfilled auth clients and server links the configuration no longer yields, not yet retired.")
	Attribute("unresolved", Int64, "Configuration that cannot be recorded without a guess.")
})

var AdminUpstreamOAuthMigrationAuthClients = Type("AdminUpstreamOAuthMigrationAuthClients", func() {
	Description("Where one organization stands in recording a typed auth client for every upstream identity its servers are configured with (upstream OAuth clients, source credential headers and identity chaining upstreams), and in retiring backfilled records the configuration no longer yields. Nothing reads these records yet.")
	Required("complete", "in_progress", "associations", "pending_clients", "pending_links", "pending_headers", "pending_retirements", "pending_marked_servers", "unresolved", "unresolved_associations", "warnings", "kinds", "failed_phases")

	Attribute("complete", Boolean, "Whether every configured identity has its auth client, server links and header copies recorded, no backfilled record is left to retire, no server offering one is left undecided, and no phase failed.")
	Attribute("in_progress", Boolean, "Whether a run is recording typed auth clients for the organization right now. The other counts are then empty; check again when it finishes.")
	Attribute("pending_marked_servers", Int64, "Servers offering a backfilled auth client whose need for upstream authentication is still undecided, not yet marked as needing it.")
	Attribute("associations", Int64, "Configured identities and server links decided.")
	Attribute("pending_clients", Int64, "Auth clients not yet recorded.")
	Attribute("pending_links", Int64, "Server to auth client links not yet recorded.")
	Attribute("pending_headers", Int64, "Header copies and EMA scope sets not yet up to date.")
	Attribute("pending_retirements", Int64, "Backfilled auth clients and server links the configuration no longer yields, not yet retired.")
	Attribute("unresolved", Int64, "Configured identities that cannot be recorded without a guess and need attention.")
	Attribute("unresolved_associations", ArrayOf(AdminUpstreamOAuthMigrationUnresolvedAssociation), "The configuration counted in unresolved.")
	Attribute("warnings", ArrayOf(AdminUpstreamOAuthMigrationUnresolvedAssociation), "Configuration worth a look that needs no action and does not hold back complete, such as a server at an identity chaining upstream's origin that only live discovery could confirm.")
	Attribute("kinds", ArrayOf(AdminUpstreamOAuthMigrationAuthClientKind), "The same counts for each kind of configured identity.")
	Attribute("failed_phases", ArrayOf(String), "Phases whose check failed: oauth, headers, ema, reconcile or mark. The counts cover the other phases.")
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
