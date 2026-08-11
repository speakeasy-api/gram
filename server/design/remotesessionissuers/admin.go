package remotesessionissuers

import (
	. "goa.design/goa/v3/dsl"
)

// Org-admin types — the read/aggregate shapes that power the organization
// administrator UI (AIS-119) for remote_session_issuers. They wrap the shared
// RemoteSessionIssuer type with the extra counts and names the listing/detail
// pages need, without mutating that shared type or the existing
// org-level-only list semantics. The client and session org-admin types live
// in the remotesessionclients and remotesessions design packages alongside
// their own services.

// OrganizationRemoteSessionIssuer pairs an issuer with its associated client count
// and, for project-specific issuers, the owning project's name. Organizational
// issuers (project_id NULL) carry an empty project_name.
var OrganizationRemoteSessionIssuer = Type("OrganizationRemoteSessionIssuer", func() {
	Description("An organization-administrator view of a remote_session_issuer: the issuer plus its associated client count and (for project-specific issuers) the owning project's name.")

	Attribute("issuer", RemoteSessionIssuer, "The remote_session_issuer record.")
	Attribute("client_count", Int, "Number of non-deleted remote_session_clients registered with this issuer.")
	Attribute("project_name", String, "The owning project's name. Empty for organizational (project_id NULL) issuers.")

	Required("issuer", "client_count")
})

var ListOrganizationRemoteSessionIssuersResult = Type("ListOrganizationRemoteSessionIssuersResult", func() {
	Description("Result type for the organization-administrator issuer listing — organizational and project-specific issuers across the org.")

	Attribute("items", ArrayOf(OrganizationRemoteSessionIssuer))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})

// OrganizationIssuerDeletePreflight describes the impact of deleting an issuer so the
// confirmation dialog can be authoritative.
var OrganizationIssuerDeletePreflight = Type("OrganizationIssuerDeletePreflight", func() {
	Description("Authoritative impact summary for deleting a remote_session_issuer: how many clients reference it and the names of the MCP servers those clients are attached to.")

	Attribute("client_count", Int, "Number of non-deleted remote_session_clients registered with this issuer.")
	Attribute("mcp_server_names", ArrayOf(String), "Display names of MCP servers attached to this issuer's clients.")

	Required("client_count", "mcp_server_names")
})

// OrganizationIssuerMigratePreflight describes the impact of consolidating a
// source issuer onto a target issuer so the confirmation dialog can list every
// blocker before the mutation runs. can_migrate is FALSE exactly when
// endpoint_mismatches or conflicting_mcp_server_names is non-empty — the same
// two conditions the migrate mutation rejects with 409.
var OrganizationIssuerMigratePreflight = Type("OrganizationIssuerMigratePreflight", func() {
	Description("Authoritative impact summary for migrating a remote_session_issuer's clients onto another issuer: how many clients move, which MCP servers are affected, and every blocker that would make the migration fail.")

	Attribute("client_count", Int, "Number of non-deleted remote_session_clients that would be re-pointed from the source issuer to the target issuer.")
	Attribute("mcp_server_names", ArrayOf(String), "Display names of MCP servers attached to the source issuer's clients.")
	Attribute("endpoint_mismatches", ArrayOf(String), "Names of the authorization-server metadata fields (issuer, token_endpoint, authorization_endpoint) that differ between source and target. Non-empty blocks the migration.")
	Attribute("conflicting_mcp_server_names", ArrayOf(String), "Display names of MCP servers where both the source and the target issuer already have a client bound. Non-empty blocks the migration; detach one client per listed server and retry.")
	Attribute("warnings", ArrayOf(String), "Non-blocking divergences (oidc, passthrough, scopes_supported). The target issuer's values become authoritative for the migrated clients.")
	Attribute("can_migrate", Boolean, "TRUE when the migration would succeed: no endpoint mismatches and no conflicting MCP-server bindings.")

	Required("client_count", "mcp_server_names", "endpoint_mismatches", "conflicting_mcp_server_names", "warnings", "can_migrate")
})

// MigrateOrganizationRemoteSessionIssuerResult reports the outcome of a
// consolidation: the surviving target issuer plus what happened to the source.
var MigrateOrganizationRemoteSessionIssuerResult = Type("MigrateOrganizationRemoteSessionIssuerResult", func() {
	Description("Outcome of consolidating a source remote_session_issuer onto a target issuer: the surviving target issuer, how many clients were re-pointed, and whether the source was soft-deleted.")

	Attribute("issuer", RemoteSessionIssuer, "The surviving target remote_session_issuer.")
	Attribute("clients_migrated", Int, "Number of remote_session_clients re-pointed from the source issuer to the target issuer. Zero when the source had no active clients.")
	Attribute("source_deleted", Boolean, "TRUE when the source issuer was soft-deleted.")

	Required("issuer", "clients_migrated", "source_deleted")
})
