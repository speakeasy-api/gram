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
