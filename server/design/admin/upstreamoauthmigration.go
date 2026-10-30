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
	Attribute("execution_profiles", AdminUpstreamOAuthMigrationExecutionProfiles, "Where the organization stands in recording the auth client choices its assistants' turns use. Separate from migrated. Absent when its check failed.")
	Attribute("enforcement", AdminUpstreamAuthEnforcement, "Whether upstream auth enforcement is on for the organization, and whether it can be turned on.")
})

var AdminUpstreamAuthEnforcement = Type("AdminUpstreamAuthEnforcement", func() {
	Description("One organization's upstream auth enforcement.")
	Required("enabled", "ready", "blockers")

	Attribute("enabled", Boolean, "Whether enforcement is on.")
	Attribute("ready", Boolean, "Whether the coverage enforcement needs is in place, so it can be turned on.")
	Attribute("blockers", ArrayOf(String), "What keeps enforcement from being turned on, empty when ready.")
})

var AdminUpstreamOAuthMigrationExecutionProfileGap = Type("AdminUpstreamOAuthMigrationExecutionProfileGap", func() {
	Description("MCP servers execution-selection profiles hold no choice for, for one execution context and reason.")
	Required("execution_context", "reason", "servers")

	Attribute("execution_context", String, "user_backed for turns run for a user the event identified, userless for turns run as the trigger's workload.")
	Attribute("reason", String, "Why there is no choice: selection_required (several clients could serve it), personal_requires_user (only a personal client serves it, and the turn has no user), no_client (no live, enabled auth client carries its upstream OAuth client) or setup_incomplete (no auth client was ever linked to it, live or detached, but it has upstream credentials that no auth client records yet).")
	Attribute("servers", Int64, "Profile and server pairs with no choice for this reason.")
})

var AdminUpstreamOAuthMigrationExecutionProfiles = Type("AdminUpstreamOAuthMigrationExecutionProfiles", func() {
	Description("Where one organization stands in recording, for each assistant, trigger and execution context, the versioned auth client choices its turns use.")
	Required("complete", "targets", "pending_versions", "gaps")

	Attribute("complete", Boolean, "Whether every profile is recorded and holds a choice for every server its assistant reaches.")
	Attribute("targets", Int64, "Assistant and trigger pairs, each with a profile per execution context.")
	Attribute("pending_versions", Int64, "Profile versions not yet recorded.")
	Attribute("gaps", ArrayOf(AdminUpstreamOAuthMigrationExecutionProfileGap), "Servers the profiles hold no choice for, by execution context and reason. They are never guessed: configure the server's auth clients.")
})

var AdminUpstreamOAuthMigrationUnresolvedAssociation = Type("AdminUpstreamOAuthMigrationUnresolvedAssociation", func() {
	Description("Configuration the typed auth client backfill cannot record without a guess, and why. ID fields that do not apply to its kind are the nil UUID.")
	Required("kind", "reason", "subject_id", "association_id", "mcp_server_id", "project_id", "remote_session_client_id", "binding_ids", "source_ids", "server_ids")

	Attribute("kind", String, "What was decided: oauth, header or ema.")
	Attribute("reason", String, "Why it cannot be recorded: unknown_owner, type_mismatch, project_mismatch, multiple_authorization_headers, duplicate_header_names, denied_source, mixed_credential_headers, oauth_client_shared_across_sources, ema_client_shared_across_sources, conflicting_scopes, resource_variants, resource_not_matched, issuer_not_allowed or registration_out_of_reach; for a warning, server_not_matched.")
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

var AdminUpstreamOAuthMigrationSelectionClass = Type("AdminUpstreamOAuthMigrationSelectionClass", func() {
	Description("How the live credential choices of one credential class map onto typed auth clients.")
	Required("credential_class", "selections", "mapped", "pending", "no_auth", "ambiguous", "no_client", "stale_no_auth", "unusable")

	Attribute("credential_class", String, "The class: interactive, id_jag, dashboard_mint, workload or agent.")
	Attribute("selections", Int64, "Live credential choices of this class.")
	Attribute("mapped", Int64, "Choices that already name a usable auth client.")
	Attribute("pending", Int64, "Choices with exactly one auth client to name, not yet named.")
	Attribute("no_auth", Int64, "Explicit no-auth choices on servers with no auth client.")
	Attribute("ambiguous", Int64, "Choices several auth clients could serve. They are never guessed: configure the server or ask the credential to reconnect.")
	Attribute("no_client", Int64, "Choices whose upstream OAuth client has no live, enabled auth client linked to the server.")
	Attribute("stale_no_auth", Int64, "No-auth choices, and choices naming the server's headers, on servers that now offer an OAuth-backed client; the credential reconnects.")
	Attribute("unusable", Int64, "Choices naming an auth client that is no longer usable for the server; the credential reconnects.")
})

var AdminUpstreamOAuthMigrationSelectionGap = Type("AdminUpstreamOAuthMigrationSelectionGap", func() {
	Description("One live credential choice left without a usable typed auth client.")
	Required("selection_id", "credential_class", "credential_id", "mcp_server_id", "project_id", "outcome")

	Attribute("selection_id", String, "The choice.", func() { Format(FormatUUID) })
	Attribute("credential_class", String, "The class of the credential the choice belongs to.")
	Attribute("credential_id", String, "The credential.", func() { Format(FormatUUID) })
	Attribute("mcp_server_id", String, "The server the choice is for.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "The server's project.", func() { Format(FormatUUID) })
	Attribute("outcome", String, "Why the choice has no usable auth client: ambiguous, no_client, stale_no_auth, unusable or skipped_race.")
})

var AdminUpstreamOAuthMigrationAuthClients = Type("AdminUpstreamOAuthMigrationAuthClients", func() {
	Description("Where one organization stands in recording a typed auth client for every upstream identity its servers are configured with (upstream OAuth clients, source credential headers and identity chaining upstreams), and in retiring backfilled records the configuration no longer yields. Nothing reads these records yet.")
	Required("complete", "in_progress", "associations", "pending_clients", "pending_links", "pending_headers", "pending_retirements", "pending_marked_servers", "unresolved", "unresolved_associations", "warnings", "kinds", "failed_phases")
	// The selection fields are optional: replicas older than them omit them
	// during a rolling deploy, and a client must still read the rest.

	Attribute("complete", Boolean, "Whether every configured identity has its auth client, server links and header copies recorded, no backfilled record is left to retire, and no phase failed.")
	Attribute("in_progress", Boolean, "Whether a run is recording typed auth clients for the organization right now. The other counts are then empty; check again when it finishes.")
	Attribute("pending_marked_servers", Int64, "Deprecated: always 0. Whether a server needs upstream authentication follows from the auth clients it offers, so no server is marked.")
	Attribute("associations", Int64, "Configured identities and server links decided.")
	Attribute("pending_clients", Int64, "Auth clients not yet recorded.")
	Attribute("pending_links", Int64, "Server to auth client links not yet recorded.")
	Attribute("pending_headers", Int64, "Header copies and EMA scope sets not yet up to date.")
	Attribute("pending_retirements", Int64, "Backfilled auth clients and server links the configuration no longer yields, not yet retired.")
	Attribute("unresolved", Int64, "Configured identities that cannot be recorded without a guess and need attention.")
	Attribute("unresolved_associations", ArrayOf(AdminUpstreamOAuthMigrationUnresolvedAssociation), "The configuration counted in unresolved.")
	Attribute("warnings", ArrayOf(AdminUpstreamOAuthMigrationUnresolvedAssociation), "Configuration worth a look that needs no action and does not hold back complete, such as a server at an identity chaining upstream's origin that only live discovery could confirm.")
	Attribute("kinds", ArrayOf(AdminUpstreamOAuthMigrationAuthClientKind), "The same counts for each kind of configured identity.")
	Attribute("failed_phases", ArrayOf(String), "Phases whose check failed: oauth, headers, ema, ema_warnings, reconcile, mark, selections or grants. The counts cover the other phases.")
	Attribute("selections_complete", Boolean, "Whether every live credential choice names a usable auth client or is an explicit no-auth choice, and the selections check did not fail.")
	Attribute("pending_selection_mappings", Int64, "Live credential choices with exactly one auth client to name, not yet named.")
	Attribute("pending_grants", Int64, "Default auth permission grants (mcp:auth) the organization's roles, users, agents and API keys are missing, which a run adds. Enforcement needs them, so existing callers keep their access.")
	Attribute("attachments_waiting", Int64, "Agent attachments whose grants a run does not write while the organization's upstream selector writes are not allowed. While any wait, complete is false.")
	Attribute("selection_classes", ArrayOf(AdminUpstreamOAuthMigrationSelectionClass), "Credential choice coverage for each credential class.")
	Attribute("selection_gaps", ArrayOf(AdminUpstreamOAuthMigrationSelectionGap), "The first live credential choices left without a usable auth client.")
	Attribute("selection_gap_count", Int64, "Every live credential choice left without a usable auth client, including those beyond selection_gaps.")
})

var AdminUpstreamOAuthMigrationRun = Type("AdminUpstreamOAuthMigrationRun", func() {
	Description("What one migration run wrote, and the organization's status after it.")
	Required("associations_written", "associations_skipped", "selections_written", "status")

	Attribute("associations_written", Int64, "Server to client associations recorded.")
	Attribute("associations_skipped", Int64, "Associations not recorded because their configuration changed during the run. Run again to pick them up.")
	Attribute("selections_written", Int64, "Credential choices recorded.")
	Attribute("auth_clients_written", Int64, "Typed auth clients recorded.")
	Attribute("auth_client_links_written", Int64, "Server to auth client links recorded.")
	Attribute("servers_marked_requiring_upstream_auth", Int64, "Deprecated: always omitted. Whether a server needs upstream authentication follows from the auth clients it offers, so no server is marked.")
	Attribute("selections_mapped", Int64, "Credential choices this run named a typed auth client on.")
	Attribute("execution_profile_versions_written", Int64, "Execution-selection profile versions this run recorded.")
	Attribute("auth_permission_grants_written", Int64, "Default auth permissions this run granted across the organization: the personal default and shared client grants for every recorded client, and API key policies once every client is recorded. The auth-permission-defaults backfill grants the rest.")
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

	Method("enableUpstreamAuthEnforcement", func() {
		Description("Enables upstream auth enforcement for one organization: its direct proxied servers and assistant turns are served by the typed auth client resolver, and using the chosen auth client requires mcp:auth. Refused unless the organization is migrated, its auth clients are recorded, its default auth permissions are granted, every credential choice that can name an auth client does, and its assistant profiles are recorded. Choices left ambiguous and profile gaps are not blocking: those credentials are asked to reconnect and those turns are refused with the reason. Shadow parity and the cross-organization denial notice are confirmed by the caller, since they are read from telemetry and communication outside the database. Enabled enforcement does not depend on the auth-client-resolver flags.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id", "shadow_parity_confirmed", "cross_organization_denial_communicated")

			Attribute("organization_id", String, "The organization ID.")
			Attribute("shadow_parity_confirmed", Boolean, "The caller checked the organization's mandatory authorization shadow (mcp.authz.mandatory_shadow) and found no unexplained new denial.")
			Attribute("cross_organization_denial_communicated", Boolean, "The organization was told that valid credentials from another organization are refused on its public servers once enforcement is on, instead of being served anonymously.")

			Meta("openapi:typename", "EnableUpstreamAuthEnforcementRequestBody")
		})

		Result(AdminUpstreamOAuthMigrationStatus)

		HTTP(func() {
			POST("/admin/upstreamOAuthMigration.enableEnforcement")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminEnableUpstreamAuthEnforcement")
		Meta("openapi:extension:x-speakeasy-name-override", "enableUpstreamAuthEnforcement")
	})

	Method("disableUpstreamAuthEnforcement", func() {
		Description("Disables upstream auth enforcement for one organization. This is its rollback: requests return to the upstream client choice within a minute. Safe to call when enforcement is already off.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "The organization ID.")

			Meta("openapi:typename", "DisableUpstreamAuthEnforcementRequestBody")
		})

		Result(AdminUpstreamOAuthMigrationStatus)

		HTTP(func() {
			POST("/admin/upstreamOAuthMigration.disableEnforcement")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminDisableUpstreamAuthEnforcement")
		Meta("openapi:extension:x-speakeasy-name-override", "disableUpstreamAuthEnforcement")
	})
}
