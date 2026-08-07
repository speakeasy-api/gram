// Package remotesessions declares the adminRemoteSessions Goa service: the
// platform-admin (Speakeasy-only) surface for curating "global"
// remote_session_issuer / remote_session_client records (project_id IS NULL AND
// organization_id IS NULL) shared across every organization. Implemented on the
// existing *remotesessions.Service; reuses the existing form/result types.
package remotesessions

import (
	. "goa.design/goa/v3/dsl"

	rsclients "github.com/speakeasy-api/gram/server/design/remotesessionclients"
	rsissuers "github.com/speakeasy-api/gram/server/design/remotesessionissuers"
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("adminRemoteSessions", func() {
	Description("Platform-admin management of global remote_session_issuer / remote_session_client records — shared across every organization (project_id NULL, organization_id NULL). Speakeasy-staff only; every method requires the platform-admin flag.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	// --- Global issuers ---

	Method("createGlobalIssuer", func() {
		Description("Create a global remote_session_issuer (project_id NULL, organization_id NULL). Requires platform admin.")

		Payload(func() {
			Extend(rsissuers.CreateRemoteSessionIssuerForm)
			security.SessionPayload()
		})

		Result(rsissuers.RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/adminRemoteSessions.createGlobalIssuer")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "createGlobalIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateGlobalRemoteSessionIssuer"}`)
	})

	Method("listGlobalIssuers", func() {
		Description("List global remote_session_issuers. Requires platform admin.")

		Payload(func() {
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
		})

		Result(ListGlobalRemoteSessionIssuersResult)

		HTTP(func() {
			GET("/rpc/adminRemoteSessions.listGlobalIssuers")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listGlobalRemoteSessionIssuers")
		Meta("openapi:extension:x-speakeasy-name-override", "listGlobalIssuers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GlobalRemoteSessionIssuers"}`)
	})

	Method("getGlobalIssuer", func() {
		Description("Get a global remote_session_issuer by id. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(GlobalRemoteSessionIssuer)

		HTTP(func() {
			GET("/rpc/adminRemoteSessions.getGlobalIssuer")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "getGlobalIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GlobalRemoteSessionIssuer"}`)
	})

	Method("updateGlobalIssuer", func() {
		Description("Update a global remote_session_issuer. Requires platform admin.")

		Payload(func() {
			Extend(rsissuers.UpdateRemoteSessionIssuerForm)
			security.SessionPayload()
		})

		Result(rsissuers.RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/adminRemoteSessions.updateGlobalIssuer")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "updateGlobalIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateGlobalRemoteSessionIssuer"}`)
	})

	Method("deleteGlobalIssuer", func() {
		Description("Soft-delete a global remote_session_issuer. Blocked when any global remote_session_clients still reference it. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/adminRemoteSessions.deleteGlobalIssuer")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteGlobalIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteGlobalRemoteSessionIssuer"}`)
	})

	Method("fetchGlobalIssuerMetadata", func() {
		Description("Hit an upstream issuer's RFC 8414 .well-known/oauth-authorization-server document and return a draft suitable for createGlobalIssuer. Keyed by issuer URL; no record need exist and nothing is persisted. Requires platform admin.")

		Payload(func() {
			Attribute("issuer", String, "Issuer URL to fetch metadata for (e.g. https://login.linear.com).")
			Required("issuer")
			security.SessionPayload()
		})

		Result(rsissuers.RemoteSessionIssuerDraft)

		HTTP(func() {
			POST("/rpc/adminRemoteSessions.fetchGlobalIssuerMetadata")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "fetchGlobalRemoteSessionIssuerMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "fetchGlobalIssuerMetadata")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "FetchGlobalRemoteSessionIssuerMetadata"}`)
	})

	Method("refreshGlobalIssuerMetadata", func() {
		Description("Re-fetch an existing global remote_session_issuer's RFC 8414 metadata document and persist the discovered values. Keyed by issuer id. Only RFC 8414-derived columns are written — endpoints, the *_supported arrays, client_id_metadata_document_supported, and the documentation URLs. Gram behavior and display fields (oidc, passthrough, name, slug, logo, client setup documentation) are left alone. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(rsissuers.RemoteSessionIssuerRefresh)

		HTTP(func() {
			POST("/rpc/adminRemoteSessions.refreshGlobalIssuerMetadata")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "refreshGlobalRemoteSessionIssuerMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "refreshGlobalIssuerMetadata")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RefreshGlobalRemoteSessionIssuerMetadata"}`)
	})

	// --- Global clients ---

	Method("createGlobalClient", func() {
		Description("Register a global remote_session_client under an existing global remote_session_issuer. Caller supplies client_id and optional client_secret obtained out-of-band from the upstream issuer. Requires platform admin.")

		Payload(func() {
			Extend(CreateGlobalRemoteSessionClientForm)
			security.SessionPayload()
		})

		Result(rsclients.RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/adminRemoteSessions.createGlobalClient")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createGlobalRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "createGlobalClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateGlobalRemoteSessionClient"}`)
	})

	Method("listGlobalClients", func() {
		Description("List the global remote_session_clients registered with a global remote_session_issuer. Requires platform admin.")

		Payload(func() {
			Attribute("remote_session_issuer_id", String, "The global remote_session_issuer id to list clients for.", func() {
				Format(FormatUUID)
			})
			Required("remote_session_issuer_id")
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
		})

		Result(rsclients.ListRemoteSessionClientsResult)

		HTTP(func() {
			GET("/rpc/adminRemoteSessions.listGlobalClients")
			Param("remote_session_issuer_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listGlobalRemoteSessionClients")
		Meta("openapi:extension:x-speakeasy-name-override", "listGlobalClients")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GlobalRemoteSessionClients"}`)
	})

	Method("getGlobalClient", func() {
		Description("Get a global remote_session_client by id. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		Result(rsclients.RemoteSessionClient)

		HTTP(func() {
			GET("/rpc/adminRemoteSessions.getGlobalClient")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getGlobalRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "getGlobalClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GlobalRemoteSessionClient"}`)
	})

	Method("updateGlobalClient", func() {
		Description("Rotate the client_secret or change non-issuer settings on a global remote_session_client. Requires platform admin.")

		Payload(func() {
			Extend(rsclients.UpdateRemoteSessionClientForm)
			security.SessionPayload()
		})

		Result(rsclients.RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/adminRemoteSessions.updateGlobalClient")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateGlobalRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "updateGlobalClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateGlobalRemoteSessionClient"}`)
	})

	Method("deleteGlobalClient", func() {
		Description("Soft-delete a global remote_session_client. Cascades to the remote_sessions minted against it. Requires platform admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/adminRemoteSessions.deleteGlobalClient")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteGlobalRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteGlobalClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteGlobalRemoteSessionClient"}`)
	})

	// --- Tenant issuer convergence ---

	Method("listGlobalIssuerConvergenceCandidates", func() {
		Description("List the organization- and project-level remote_session_issuers that describe the same upstream authorization server as a given global issuer, and so could be consolidated onto it. Matching is by canonical issuer URL, collapsing trailing-slash and default-port spellings. Each candidate carries its owning organization, the number of clients that would move, and the metadata differences that would block or accompany the migration. Requires platform admin.")

		Payload(func() {
			Attribute("target_id", String, "The global remote_session_issuer that candidates would be consolidated onto.", func() {
				Format(FormatUUID)
			})
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			Required("target_id")
			security.SessionPayload()
		})

		Result(ListIssuerConvergenceCandidatesResult)

		HTTP(func() {
			GET("/rpc/adminRemoteSessions.listGlobalIssuerConvergenceCandidates")
			Param("target_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listGlobalRemoteSessionIssuerConvergenceCandidates")
		Meta("openapi:extension:x-speakeasy-name-override", "listGlobalIssuerConvergenceCandidates")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GlobalRemoteSessionIssuerConvergenceCandidates"}`)
	})

	Method("getGlobalIssuerMigratePreflight", func() {
		Description("Authoritative impact summary for consolidating a tenant remote_session_issuer onto a global one: the clients that would move, the affected MCP servers, and every blocker (endpoint mismatches, conflicting MCP-server bindings). Also reports how many tenant-owned clients the target already carries, since those permanently block deleting it. Requires platform admin.")

		Payload(func() {
			Attribute("source_id", String, "The organization- or project-level remote_session_issuer to migrate away from.", func() {
				Format(FormatUUID)
			})
			Attribute("target_id", String, "The global remote_session_issuer to migrate onto.", func() {
				Format(FormatUUID)
			})
			Required("source_id", "target_id")
			security.SessionPayload()
		})

		Result(IssuerMigratePreflight)

		HTTP(func() {
			GET("/rpc/adminRemoteSessions.getGlobalIssuerMigratePreflight")
			Param("source_id")
			Param("target_id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getGlobalRemoteSessionIssuerMigratePreflight")
		Meta("openapi:extension:x-speakeasy-name-override", "getGlobalIssuerMigratePreflight")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GlobalRemoteSessionIssuerMigratePreflight"}`)
	})

	Method("migrateToGlobalIssuer", func() {
		Description("Consolidate an organization- or project-level remote_session_issuer onto a global one: re-point every client from the source issuer onto the target, then soft-delete the source. Existing remote sessions are preserved, so no user re-authenticates. The source may belong to any organization; the target must be a global issuer. Both must agree on issuer (compared canonically), token_endpoint, and authorization_endpoint. One source per call. Requires platform admin.")

		Payload(func() {
			Attribute("source_id", String, "The organization- or project-level remote_session_issuer to migrate away from; soft-deleted on success.", func() {
				Format(FormatUUID)
			})
			Attribute("target_id", String, "The global remote_session_issuer to migrate onto; survives and adopts the source's clients.", func() {
				Format(FormatUUID)
			})
			Required("source_id", "target_id")
			security.SessionPayload()

			// This payload is shape-identical to organizationRemoteSessionIssuers'
			// migrateIssuer, and Goa's OpenAPI emitter deduplicates request bodies by
			// shape rather than by name: without an explicit typename the two collapse
			// into one schema and this method's generated SDK type takes the other
			// service's name.
			Meta("openapi:typename", "MigrateRemoteSessionIssuerRequestBody")
		})

		Result(MigrateRemoteSessionIssuerResult)

		HTTP(func() {
			POST("/rpc/adminRemoteSessions.migrateToGlobalIssuer")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "migrateToGlobalRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "migrateToGlobalIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "MigrateToGlobalRemoteSessionIssuer"}`)
	})
})

// IssuerConvergenceCandidate is one organization's issuer that names the
// same upstream as a global issuer, as offered to a platform admin considering
// consolidation.
//
// It carries the two blocker sets that are pure functions of the two issuer
// records — endpoint_mismatches and warnings — so the listing itself explains
// why a near-miss candidate cannot be migrated. It deliberately does NOT carry
// conflicting MCP-server bindings or an overall can_migrate verdict: detecting
// those costs two more queries per candidate, both scaling with the target's
// population across every tenant, and the page would get slower the more widely
// adopted the platform issuer is. getIssuerMigratePreflight answers that
// for a single pair.
var IssuerConvergenceCandidate = Type("IssuerConvergenceCandidate", func() {
	Description("An organization- or project-level remote_session_issuer that names the same upstream authorization server as a global issuer, and so could be consolidated onto it.")

	Attribute("issuer", rsissuers.RemoteSessionIssuer, "The candidate tenant remote_session_issuer.")
	Attribute("organization_id", String, "The organization that owns the candidate. Empty for a legacy project-scoped issuer written before this column existed.")
	Attribute("organization_name", String, "Display name of the owning organization. Empty when the organization has no synced metadata.")
	Attribute("client_count", Int, "Number of non-deleted remote_session_clients that would move onto the target issuer.")
	Attribute("endpoint_mismatches", ArrayOf(String), "Names of the authorization-server metadata fields (issuer, token_endpoint, authorization_endpoint) that differ from the target. Non-empty blocks the migration.")
	Attribute("warnings", ArrayOf(String), "Non-blocking divergences (oidc, passthrough, scopes_supported). The target issuer's values become authoritative for the migrated clients.")

	Required("issuer", "organization_id", "organization_name", "client_count", "endpoint_mismatches", "warnings")
})

var ListIssuerConvergenceCandidatesResult = Type("ListIssuerConvergenceCandidatesResult", func() {
	Description("Result type for the platform-administrator convergence-candidate listing.")

	Attribute("items", ArrayOf(IssuerConvergenceCandidate))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})

// IssuerMigratePreflight describes the impact of consolidating one
// organization's issuer onto a global issuer. can_migrate is FALSE exactly when
// endpoint_mismatches or conflicting_mcp_server_names is non-empty — the same
// two conditions the migrate mutation rejects with 409.
var IssuerMigratePreflight = Type("IssuerMigratePreflight", func() {
	Description("Authoritative impact summary for consolidating a tenant remote_session_issuer onto a global one: how many clients move, which MCP servers are affected, every blocker that would make the migration fail, and how many tenant-owned clients the target already carries.")

	Attribute("client_count", Int, "Number of non-deleted remote_session_clients that would be re-pointed from the source issuer to the target issuer.")
	Attribute("mcp_server_names", ArrayOf(String), "Display names of MCP servers attached to the source issuer's clients.")
	Attribute("endpoint_mismatches", ArrayOf(String), "Names of the authorization-server metadata fields (issuer, token_endpoint, authorization_endpoint) that differ between source and target. Non-empty blocks the migration.")
	Attribute("conflicting_mcp_server_names", ArrayOf(String), "Display names of MCP servers where both the source and the target issuer already have a client bound. Non-empty blocks the migration; detach one client per listed server and retry.")
	Attribute("warnings", ArrayOf(String), "Non-blocking divergences (oidc, passthrough, scopes_supported). The target issuer's values become authoritative for the migrated clients.")
	Attribute("can_migrate", Boolean, "TRUE when the migration would succeed: no endpoint mismatches and no conflicting MCP-server bindings.")
	Attribute("target_tenant_client_count", Int, "Number of tenant-owned remote_session_clients already registered with the target issuer, BEFORE this migration. Any non-zero value blocks deleting the target issuer, and only the owning organizations can clear it, so a successful migration is effectively one-way.")

	Required("client_count", "mcp_server_names", "endpoint_mismatches", "conflicting_mcp_server_names", "warnings", "can_migrate", "target_tenant_client_count")
})

// MigrateRemoteSessionIssuerResult reports the outcome of consolidating
// one organization's issuer onto the platform catalog.
var MigrateRemoteSessionIssuerResult = Type("MigrateRemoteSessionIssuerResult", func() {
	Description("Outcome of consolidating a tenant remote_session_issuer onto a global issuer: the surviving global issuer, how many clients were re-pointed, and whether the source was soft-deleted.")

	Attribute("issuer", rsissuers.RemoteSessionIssuer, "The surviving target global remote_session_issuer.")
	Attribute("clients_migrated", Int, "Number of remote_session_clients re-pointed from the source issuer to the target issuer. Zero when the source had no active clients.")
	Attribute("source_deleted", Boolean, "TRUE when the source issuer was soft-deleted.")

	Required("issuer", "clients_migrated", "source_deleted")
})

// GlobalRemoteSessionIssuer is the platform-admin view of a global
// remote_session_issuer: the record plus the two client counts that decide
// whether it can be deleted. They are reported separately because only one of
// them is actionable by the platform admin — global clients they can delete
// here, tenant-owned clients they can neither see nor remove.
var GlobalRemoteSessionIssuer = Type("GlobalRemoteSessionIssuer", func() {
	Description("A platform-administrator view of a global remote_session_issuer: the issuer plus its global and tenant-owned client counts.")

	Attribute("issuer", rsissuers.RemoteSessionIssuer, "The remote_session_issuer record.")
	Attribute("global_client_count", Int, "Number of non-deleted global remote_session_clients (project_id NULL, organization_id NULL) registered with this issuer. These block a delete and the platform admin can remove them here.")
	Attribute("tenant_client_count", Int, "Number of non-deleted remote_session_clients owned by an organization or project that are registered with this issuer. These block a delete but only their owning organization can remove them.")

	Required("issuer", "global_client_count", "tenant_client_count")
})

var ListGlobalRemoteSessionIssuersResult = Type("ListGlobalRemoteSessionIssuersResult", func() {
	Description("Result type for the platform-administrator global issuer listing.")

	Attribute("items", ArrayOf(GlobalRemoteSessionIssuer))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})

// CreateGlobalRemoteSessionClientForm is the global-client create form: like
// CreateRemoteSessionClientForm but without user_session_issuer attachments.
var CreateGlobalRemoteSessionClientForm = Type("CreateGlobalRemoteSessionClientForm", func() {
	Description("Form for creating a global remote_session_client. Caller supplies client_id (and optional client_secret) obtained out-of-band from the upstream issuer.")

	Attribute("remote_session_issuer_id", String, "The owning global remote_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("client_id", String, "client_id supplied by the caller.")
	Attribute("client_secret", String, "client_secret supplied by the caller. Gram encrypts before persisting.")
	Attribute("token_endpoint_auth_method", String, "How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.", func() {
		Enum("client_secret_basic", "client_secret_post", "none")
	})
	Attribute("scope", ArrayOf(String), func() {
		rsclients.ScopeAttribute("Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.")
	})
	Attribute("audience", String, "Optional upstream OAuth audience to send on the authorize redirect and token exchange.", rsclients.AudienceAttribute)

	Required("remote_session_issuer_id", "client_id")
})
