package remotesessionissuers

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/shared"
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
	Description("Authoritative impact summary for deleting a remote_session_issuer: its client, trusted user-session-issuer, and active identity-chaining binding references.")

	Attribute("client_count", Int, "Number of non-deleted remote_session_clients registered with this issuer.")
	Attribute("mcp_server_names", ArrayOf(String), "Display names of MCP servers attached to this issuer's clients.")
	Attribute("trusted_user_session_issuers", ArrayOf(TrustedUserSessionIssuerReference), "Organization-owned user_session_issuers that trust this issuer and block deletion.")

	Required("client_count", "mcp_server_names", "trusted_user_session_issuers")
	Attribute("ema_binding_count", Int64, "Active identity-chaining bindings that must be explicitly unlinked before deletion.")
	Required("ema_binding_count")
})

var TrustedUserSessionIssuerReference = Type("TrustedUserSessionIssuerReference", func() {
	Description("An organization-owned user_session_issuer that uses a remote_session_issuer as its trust anchor.")
	Attribute("id", String, "The user_session_issuer id.", func() { Format(FormatUUID) })
	Attribute("slug", String, "The user_session_issuer slug.")
	Required("id", "slug")
})

// IssuerFieldMismatch is one issuer field whose value differs between the
// source and target of a consolidation, carrying both sides' values so the
// confirmation dialog can show what an administrator is accepting rather than
// only which field disagrees.
//
// Scalar and list values are separate attribute pairs rather than one array
// pair. A scalar's absence is load-bearing: a target that declares no token
// endpoint blocks the migration, and has to stay distinguishable from one that
// declares an empty string, which a single array pair could not express.
var IssuerFieldMismatch = Type("IssuerFieldMismatch", func() {
	Meta("struct:pkg:path", "types")

	Description("One remote_session_issuer field whose value differs between the source and target of a consolidation, with both sides' values.")

	Attribute("field", String, "The differing field's name: issuer, token_endpoint, authorization_endpoint, oidc, passthrough, or scopes_supported.")
	Attribute("source_value", String, "The source issuer's value for a scalar field, rendered as a string. Null when the source leaves the field unset, and null for a list-valued field.")
	Attribute("target_value", String, "The target issuer's value for a scalar field, rendered as a string. Null when the target leaves the field unset, and null for a list-valued field.")
	Attribute("source_values", ArrayOf(String), "The source issuer's entries for a list-valued field. Absent for a scalar field, and absent when the source's list is empty.")
	Attribute("target_values", ArrayOf(String), "The target issuer's entries for a list-valued field. Absent for a scalar field, and absent when the target's list is empty.")

	Required("field")
})

// OrganizationIssuerMigratePreflight describes the impact of consolidating a
// source issuer onto a target issuer so the confirmation dialog can list every
// blocker before the mutation runs. can_migrate is FALSE when endpoint
// metadata differs, an MCP-server binding conflicts, an EMA binding is active,
// or a user-session issuer still trusts the source — the same conditions the mutation rejects with 409.
var OrganizationIssuerMigratePreflight = Type("OrganizationIssuerMigratePreflight", func() {
	Description("Authoritative impact summary for migrating a remote_session_issuer's clients onto another issuer: how many clients move, which MCP servers are affected, and every blocker that would make the migration fail.")

	Attribute("client_count", Int, "Number of non-deleted remote_session_clients that would be re-pointed from the source issuer to the target issuer.")
	Attribute("mcp_server_names", ArrayOf(String), "Display names of MCP servers attached to the source issuer's clients.")
	Attribute("endpoint_mismatches", ArrayOf(IssuerFieldMismatch), "The authorization-server metadata fields (issuer, token_endpoint, authorization_endpoint) that differ between source and target, with both sides' values. Non-empty blocks the migration.")
	Attribute("conflicting_mcp_server_names", ArrayOf(String), "Display names of MCP servers where both the source and the target issuer already have a client bound. Non-empty blocks the migration; detach one client per listed server and retry.")
	Attribute("warnings", ArrayOf(IssuerFieldMismatch), "Non-blocking divergences (oidc, passthrough, scopes_supported), with both sides' values. The target issuer's values become authoritative for the migrated clients.")
	Attribute("trusted_user_session_issuers", ArrayOf(TrustedUserSessionIssuerReference), "User-session issuers that trust the source and block migration until explicitly unlinked or re-linked.")
	Attribute("ema_binding_count", Int64, "Number of active identity-chaining bindings on the source. Non-zero blocks migration; explicitly unlink these bindings before migration, then prepare new bindings for the target.")
	Attribute("can_migrate", Boolean, "TRUE when the migration would succeed: no active identity-chaining bindings, endpoint mismatches, conflicting MCP-server bindings, or user-session issuers that trust the source.")

	Required("ema_binding_count", "client_count", "mcp_server_names", "endpoint_mismatches", "conflicting_mcp_server_names", "warnings", "trusted_user_session_issuers", "can_migrate")
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

// EMAService is one service identity chaining reaches in the organization.
var EMAService = Type("EMAService", func() {
	Description("One service identity chaining reaches in the organization: a vendor authorization server, the identity providers that can vouch for people to it with whether the vendor has accepted each, and the MCP servers that chain through it.")

	Attribute("vendor_issuer", String, "The vendor authorization server issuer, canonicalized.")
	Attribute("name", String, "The vendor's display name, from an issuer record that names it. Empty when none has one.")
	Attribute("remote_session_issuer_id", String, "An issuer record the organization can name for this vendor, organization-level first, to pass to setEMAProviders. Omitted only when no issuer record names the vendor any more.", func() {
		Format(FormatUUID)
	})
	Attribute("ema_providers", shared.EMAProviderOrder, "The organization-wide provider order. Each provider's last_accepted_at is the newest acceptance across every binding in the organization.")
	Attribute("server_count", Int, "Distinct live MCP servers whose identity chaining binding for this vendor has been turned on, in any state after setup, including servers the caller cannot read.")
	Attribute("ready_server_count", Int, "Of those, how many have a ready binding, including servers the caller cannot read.")
	Attribute("servers", ArrayOf(EMAServiceServer), "The counted servers the caller can read (mcp:read), ordered by project and name.")
	Attribute("hidden_server_count", Int, "Counted servers the caller cannot read; they are not named.")

	Required("vendor_issuer", "name", "ema_providers", "server_count", "ready_server_count", "servers", "hidden_server_count")
})

// EMAServiceServer is one MCP server that reaches a service through identity chaining.
var EMAServiceServer = Type("EMAServiceServer", func() {
	Description("One live MCP server whose identity chaining binding for the service has been turned on.")

	Attribute("id", String, "The MCP server id.", func() { Format(FormatUUID) })
	Attribute("name", String, "The MCP server name. Empty when it has none.")
	Attribute("slug", String, "The MCP server slug. Empty when it has none.")
	Attribute("project_id", String, "The owning project.", func() { Format(FormatUUID) })
	Attribute("project_slug", String, "The owning project's slug.")
	Attribute("project_name", String, "The owning project's name.")
	Attribute("status", String, "working: on, and the vendor accepted an assertion for this server from a provider that can vouch now. not_proven: on, with no such acceptance yet. needs_attention: on but blocked, or no provider can vouch for it right now.", func() {
		Enum("working", "not_proven", "needs_attention")
	})
	Attribute("last_accepted_at", String, "When the vendor last accepted an assertion for this server from a provider that can vouch now. Omitted when never.", func() {
		Format(FormatDateTime)
	})
	Attribute("sign_in_issuer_id", String, "The remote session issuer the server's sign-in trusts, such as an Okta authorization server: only that issuer can vouch for people on the server. Omitted when the server uses Speakeasy's own sign-in, where only Speakeasy can vouch.", func() {
		Format(FormatUUID)
	})
	Attribute("sign_in_issuer_name", String, "The display name of sign_in_issuer_id. Omitted with it, or when the issuer has no name.")

	Required("id", "name", "slug", "project_id", "project_slug", "project_name", "status")
})

var ListEMAServicesResult = Type("ListEMAServicesResult", func() {
	Description("Every service identity chaining reaches in the organization.")

	Attribute("items", ArrayOf(EMAService))

	Required("items")
})
