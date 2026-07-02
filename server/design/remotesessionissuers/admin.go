package remotesessionissuers

import (
	. "goa.design/goa/v3/dsl"

	rsclients "github.com/speakeasy-api/gram/server/design/remotesessionclients"
	rsessions "github.com/speakeasy-api/gram/server/design/remotesessions"
)

// Org-admin types — the read/aggregate shapes that power the organization
// administrator UI (AIS-119). They wrap the existing remote_session_* resource
// types with the extra counts and names the listing/detail pages need, without
// mutating those shared types or the existing org-level-only list semantics.

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

// OrganizationRemoteSessionClient pairs a client with the number of MCP servers it
// is attached to (resolved through user_session_issuers) and the number of active
// remote_sessions minted against it.
var OrganizationRemoteSessionClient = Type("OrganizationRemoteSessionClient", func() {
	Description("An organization-administrator view of a remote_session_client: the client plus the number of MCP servers it is attached to and the number of active sessions minted against it.")

	Attribute("client", rsclients.RemoteSessionClient, "The remote_session_client record.")
	Attribute("mcp_server_count", Int, "Number of non-deleted MCP servers attached to this client (via user_session_issuers).")
	Attribute("active_session_count", Int, "Number of non-deleted (active) remote_sessions minted against this client.")

	Required("client", "mcp_server_count", "active_session_count")
})

var ListOrganizationRemoteSessionClientsResult = Type("ListOrganizationRemoteSessionClientsResult", func() {
	Description("Result type for the organization-administrator client listing for a single issuer.")

	Attribute("items", ArrayOf(OrganizationRemoteSessionClient))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})

// OrganizationMcpServer is the minimal MCP server shape the org-admin client detail
// page needs: identity for linking plus name/url for display-name precedence.
var OrganizationMcpServer = Type("OrganizationMcpServer", func() {
	Description("An MCP server attached to a remote_session_client, with the fields the org-admin UI needs to display and link to it.")

	Attribute("id", String, "The mcp_server id.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The owning project id.", func() {
		Format(FormatUUID)
	})
	Attribute("project_slug", String, "The owning project's slug, for linking to the MCP server in its project.")
	Attribute("name", String, "The MCP server name; empty when unset (display falls back to the URL).")
	Attribute("slug", String, "The MCP server slug.")
	Attribute("url", String, "The remote MCP server URL; empty for non-remote (toolset-backed) servers.")

	Required("id", "project_id")
})

var ListOrganizationMcpServersResult = Type("ListOrganizationMcpServersResult", func() {
	Description("Result type for the MCP servers attached to a remote_session_client.")

	Attribute("items", ArrayOf(OrganizationMcpServer))

	Required("items")
})

var ListOrganizationRemoteSessionsResult = Type("ListOrganizationRemoteSessionsResult", func() {
	Description("Result type for the remote_sessions minted against a remote_session_client.")

	Attribute("items", ArrayOf(rsessions.RemoteSession))
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

// OrganizationClientDeletePreflight describes the impact of deleting a client.
var OrganizationClientDeletePreflight = Type("OrganizationClientDeletePreflight", func() {
	Description("Authoritative impact summary for deleting a remote_session_client: how many sessions it holds and the names of the MCP servers it is attached to.")

	Attribute("session_count", Int, "Number of non-deleted remote_sessions minted against this client.")
	Attribute("mcp_server_names", ArrayOf(String), "Display names of MCP servers this client is attached to.")

	Required("session_count", "mcp_server_names")
})

// scopePattern / audiencePattern and the scopeAttribute / audienceAttribute
// helpers mirror the project-scoped remoteSessionClients design so the org-admin
// update path enforces the same boundary validation on values that are later
// sent to upstream OAuth endpoints. Keep in sync with design/remotesessionclients.
const (
	scopePattern    = `^[!#-[\]-~]+$`
	audiencePattern = `^[!-~]+$`
)

func scopeAttribute(description string) {
	Description(description)
	Elem(func() {
		Pattern(scopePattern)
		MaxLength(128)
	})
}

func audienceAttribute() {
	Pattern(audiencePattern)
	MaxLength(512)
}

// resourceAttribute constrains RFC 8707 resource indicators: an absolute
// URI without whitespace. Full URI validation happens upstream at the AS.
func resourceAttribute() {
	Pattern(audiencePattern)
	MaxLength(512)
}

// CreateOrganizationRemoteSessionClientForm registers a standalone
// remote_session_client under an existing remote_session_issuer in the caller's
// organization, with no user_session_issuer attachments. Scope mirrors
// createIssuer's project_id: a supplied project_id scopes the client to that
// project; an omitted project_id inherits a project-specific issuer's project,
// or, under an organization-level issuer, creates an organization-level client
// (no project) that every project in the organization can attach. The caller
// supplies client_id (and optional client_secret) obtained out-of-band,
// typically via Dynamic Client Registration performed client-side. A supplied
// secret is encrypted before persisting; the plaintext is never returned.
var CreateOrganizationRemoteSessionClientForm = Type("CreateOrganizationRemoteSessionClientForm", func() {
	Description("Form for an org admin to register a standalone remote_session_client under an existing issuer, with no user_session_issuer attachments.")

	Attribute("remote_session_issuer_id", String, "The owning remote_session_issuer id; must belong to the caller's organization.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "Owning project id for the new client; the project must belong to the caller's organization. Omit to inherit a project-specific issuer's project, or to create an organization-level client (no project, attachable by every project) under an organization-level issuer.", func() {
		Format(FormatUUID)
	})
	Attribute("client_id", String, "client_id supplied by the caller, e.g. from Dynamic Client Registration.")
	Attribute("client_secret", String, "Optional client_secret supplied by the caller. Gram encrypts before persisting; the plaintext is never returned.")
	Attribute("token_endpoint_auth_method", String, "How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.", func() {
		Enum("client_secret_basic", "client_secret_post", "none")
	})
	Attribute("scope", ArrayOf(String), func() {
		scopeAttribute("Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.")
	})
	Attribute("audience", String, "Optional upstream OAuth audience to send on the authorize redirect and token exchange.", audienceAttribute)
	Attribute("resource", String, "Optional RFC 8707 resource indicator sent on the authorize redirect, token exchange, and refresh grant.", resourceAttribute)

	Required("remote_session_issuer_id", "client_id")
})

// CreateCimdOrganizationRemoteSessionClientForm registers a standalone client
// in Client ID Metadata Document (CIMD) mode. The caller supplies no
// credentials: Gram generates the client_id and hosts the metadata document.
var CreateCimdOrganizationRemoteSessionClientForm = Type("CreateCimdOrganizationRemoteSessionClientForm", func() {
	Description("Form for an org admin to register a standalone remote_session_client in Client ID Metadata Document (CIMD) mode under an existing issuer, with no user_session_issuer attachments. Gram generates the client_id and hosts the metadata document; the issuer must advertise client_id_metadata_document_supported.")

	Attribute("remote_session_issuer_id", String, "The owning remote_session_issuer id; must belong to the caller's organization and advertise client_id_metadata_document_supported.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "Owning project id for the new client; the project must belong to the caller's organization. Omit to inherit a project-specific issuer's project, or to create an organization-level client (no project, attachable by every project) under an organization-level issuer.", func() {
		Format(FormatUUID)
	})
	Attribute("scope", ArrayOf(String), func() {
		scopeAttribute("Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.")
	})
	Attribute("audience", String, "Optional upstream OAuth audience to send on the authorize redirect and token exchange.", audienceAttribute)
	Attribute("resource", String, "Optional RFC 8707 resource indicator sent on the authorize redirect, token exchange, and refresh grant.", resourceAttribute)

	Required("remote_session_issuer_id")
})

// UpdateOrganizationRemoteSessionClientForm patches the fields of a
// remote_session_client from the org-admin Settings tab, including client
// secret rotation. A supplied secret is re-encrypted with the project-scoped
// key before persisting; the plaintext is never returned.
var UpdateOrganizationRemoteSessionClientForm = Type("UpdateOrganizationRemoteSessionClientForm", func() {
	Description("Form for an org admin to update a remote_session_client's non-secret fields.")

	Attribute("id", String, "The remote_session_client id.", func() {
		Format(FormatUUID)
	})
	Attribute("client_secret", String, "Rotate the client secret. Gram re-encrypts before persisting; the plaintext is never returned.")
	Attribute("token_endpoint_auth_method", String, "Change how the client authenticates at the issuer's token endpoint.", func() {
		Enum("client_secret_basic", "client_secret_post", "none")
	})
	Attribute("scope", ArrayOf(String), func() {
		scopeAttribute("Replace the explicit upstream OAuth scopes for this client.")
	})
	Attribute("audience", String, "Replace the upstream OAuth audience sent for this client.", audienceAttribute)
	Attribute("resource", String, "Replace the RFC 8707 resource indicator sent for this client.", resourceAttribute)

	Required("id")
})

// RevokeAllRemoteSessionsResult reports how many sessions a revoke-all cleared.
var RevokeAllRemoteSessionsResult = Type("RevokeAllRemoteSessionsResult", func() {
	Description("Result type for revoking all of a client's remote_sessions.")

	Attribute("revoked_count", Int, "Number of remote_sessions revoked.")

	Required("revoked_count")
})
