package remotesessionissuers

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("remoteSessionIssuers", func() {
	Description("Manage remote_session_issuer records — upstream Authorization Server identity records that Gram talks to as an OAuth client.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("fetchRemoteSessionIssuerMetadata", func() {
		Description("Hit an upstream issuer's RFC 8414 .well-known/oauth-authorization-server document and return a draft suitable for createRemoteSessionIssuer. Keyed by issuer URL; no record need exist and nothing is persisted. Use refreshMetadata to re-discover and persist against an existing issuer.")

		Payload(func() {
			Attribute("issuer", String, "Issuer URL to fetch metadata for (e.g. https://login.linear.com).")
			Required("issuer")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionIssuerDraft)

		HTTP(func() {
			POST("/rpc/remoteSessionIssuers.fetchMetadata")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "fetchRemoteSessionIssuerMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "fetchMetadata")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "FetchRemoteSessionIssuerMetadata"}`)
	})

	Method("refreshRemoteSessionIssuerMetadata", func() {
		Description("Re-fetch an existing remote_session_issuer's RFC 8414 metadata document and persist the discovered values. Keyed by issuer id. Only RFC 8414-derived columns are written — endpoints, the *_supported arrays, client_id_metadata_document_supported, and the documentation URLs. Gram behavior and display fields (oidc, passthrough, name, slug, logo, client setup documentation) are left alone. Requires project:write.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionIssuerRefresh)

		HTTP(func() {
			POST("/rpc/remoteSessionIssuers.refreshMetadata")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "refreshRemoteSessionIssuerMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "refreshMetadata")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RefreshRemoteSessionIssuerMetadata"}`)
	})

	Method("createRemoteSessionIssuer", func() {
		Description("Create a new remote_session_issuer.")

		Payload(func() {
			Extend(CreateRemoteSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/remoteSessionIssuers.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateRemoteSessionIssuer"}`)
	})

	Method("updateRemoteSessionIssuer", func() {
		Description("Update fields on an existing remote_session_issuer.")

		Payload(func() {
			Extend(UpdateRemoteSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/remoteSessionIssuers.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateRemoteSessionIssuer"}`)
	})

	Method("listRemoteSessionIssuers", func() {
		Description("List remote_session_issuers in the caller's project.")

		Payload(func() {
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListRemoteSessionIssuersResult)

		HTTP(func() {
			GET("/rpc/remoteSessionIssuers.list")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listRemoteSessionIssuers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoteSessionIssuers"}`)
	})

	Method("getRemoteSessionIssuer", func() {
		Description("Get a remote_session_issuer by id or by slug. Provide exactly one.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "The remote_session_issuer slug.")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			GET("/rpc/remoteSessionIssuers.get")
			Param("id")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoteSessionIssuer"}`)
	})

	Method("deleteRemoteSessionIssuer", func() {
		Description("Soft-delete a remote_session_issuer. Blocked if any remote_session_clients still reference it.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/remoteSessionIssuers.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteRemoteSessionIssuer"}`)
	})
})

// organizationRemoteSessionIssuers manages organization-level (cross-project)
// remote_session_issuers — rows with a NULL project_id scoped to an
// organization. These are administered by org admins and inherited by every
// project in the org. Security is organization-scoped (no ProjectSlug); RBAC
// gates writes on org:admin and reads on org:read.
var _ = Service("organizationRemoteSessionIssuers", func() {
	Description("Manage organization-level remote_session_issuer records — cross-project upstream Authorization Server identity records inherited by every project in the organization.")
	Security(security.Session)
	Security(security.ByKey, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	// --- Organization administrator surface (AIS-119) ---
	// These methods power the org-admin "Remote Identity Providers" UI. Reads
	// require org:read, writes require org:admin. Unlike the methods above they
	// span both organizational (project_id NULL) and project-specific issuers in
	// the caller's organization, scoped exclusively by organization_id.

	Method("createIssuer", func() {
		Description("Create a remote_session_issuer in the caller's organization. With no project_id the issuer is organization-level (project_id NULL, inherited by every project); with a project_id (which must belong to the organization) it is project-specific. Requires org:admin.")

		Payload(func() {
			Extend(CreateRemoteSessionIssuerForm)
			Attribute("project_id", String, "Owning project id; the project must belong to the caller's organization. Omit to create an organization-level issuer.", func() {
				Format(FormatUUID)
			})
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionIssuers.create")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createOrganizationRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateOrganizationRemoteSessionIssuer"}`)
	})

	Method("listIssuers", func() {
		Description("List all remote_session_issuers in the caller's organization — organizational (project_id NULL) and project-specific — each with its associated client count and, for project-specific issuers, the owning project name. Requires org:read.")

		Payload(func() {
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(ListOrganizationRemoteSessionIssuersResult)

		HTTP(func() {
			GET("/rpc/organizationRemoteSessionIssuers.list")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listOrganizationRemoteSessionIssuers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationRemoteSessionIssuers"}`)
	})

	Method("getIssuer", func() {
		Description("Get any remote_session_issuer (organizational or project-specific) in the caller's organization by id. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			GET("/rpc/organizationRemoteSessionIssuers.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getOrganizationRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationRemoteSessionIssuer"}`)
	})

	Method("getIssuerDeletePreflight", func() {
		Description("Authoritative impact summary for deleting a remote_session_issuer: associated client count and affected MCP server names. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(OrganizationIssuerDeletePreflight)

		HTTP(func() {
			GET("/rpc/organizationRemoteSessionIssuers.getDeletePreflight")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getOrganizationRemoteSessionIssuerDeletePreflight")
		Meta("openapi:extension:x-speakeasy-name-override", "getDeletePreflight")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationRemoteSessionIssuerDeletePreflight"}`)
	})

	Method("updateIssuer", func() {
		Description("Update any remote_session_issuer (organizational or project-specific) in the caller's organization. Requires org:admin.")

		Payload(func() {
			Extend(UpdateRemoteSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionIssuers.update")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateOrganizationRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateOrganizationRemoteSessionIssuer"}`)
	})

	Method("deleteIssuer", func() {
		Description("Soft-delete any remote_session_issuer (organizational or project-specific) in the caller's organization. Blocked when any remote_session_clients still reference it. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		HTTP(func() {
			DELETE("/rpc/organizationRemoteSessionIssuers.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteOrganizationRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteOrganizationRemoteSessionIssuer"}`)
	})

	Method("moveIssuer", func() {
		Description("Re-scope a remote_session_issuer in the caller's organization: provide a project_id (which must belong to the organization) to make it project-specific, or omit it to make it organization-level (project_id NULL, inherited by every project). Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Attribute("project_id", String, "Target owning project id; the project must belong to the caller's organization. Omit to make the issuer organization-level.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionIssuer)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionIssuers.move")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "moveOrganizationRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "move")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "MoveOrganizationRemoteSessionIssuer"}`)
	})

	Method("getIssuerMigratePreflight", func() {
		Description("Authoritative impact summary for migrating a remote_session_issuer's clients onto another issuer: the clients that would move, the affected MCP servers, and every blocker (endpoint mismatches, conflicting MCP-server bindings). Requires org:read.")

		Payload(func() {
			Attribute("source_id", String, "The remote_session_issuer to migrate away from.", func() {
				Format(FormatUUID)
			})
			Attribute("target_id", String, "The remote_session_issuer to migrate onto.", func() {
				Format(FormatUUID)
			})
			Required("source_id", "target_id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(OrganizationIssuerMigratePreflight)

		HTTP(func() {
			GET("/rpc/organizationRemoteSessionIssuers.getMigratePreflight")
			Param("source_id")
			Param("target_id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getOrganizationRemoteSessionIssuerMigratePreflight")
		Meta("openapi:extension:x-speakeasy-name-override", "getMigratePreflight")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationRemoteSessionIssuerMigratePreflight"}`)
	})

	Method("migrateIssuer", func() {
		Description("Consolidate two remote_session_issuers that point at the same upstream authorization server: re-point every client from the source issuer onto the target issuer, then soft-delete the source. Existing remote sessions are preserved, so no user re-authenticates. Both issuers must belong to the caller's organization and agree on issuer, token_endpoint, and authorization_endpoint. The target may not be narrower in scope than the source: a project-specific issuer may migrate onto an issuer in the same project or onto an organization-level issuer, and an organization-level issuer may migrate onto another organization-level issuer. Requires org:admin.")

		Payload(func() {
			Attribute("source_id", String, "The remote_session_issuer to migrate away from; soft-deleted on success.", func() {
				Format(FormatUUID)
			})
			Attribute("target_id", String, "The remote_session_issuer to migrate onto; survives and adopts the source's clients.", func() {
				Format(FormatUUID)
			})
			Required("source_id", "target_id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(MigrateOrganizationRemoteSessionIssuerResult)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionIssuers.migrate")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "migrateOrganizationRemoteSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "migrate")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "MigrateOrganizationRemoteSessionIssuer"}`)
	})

	Method("fetchIssuerMetadata", func() {
		Description("Hit an upstream issuer's RFC 8414 .well-known/oauth-authorization-server document and return a draft suitable for organizationRemoteSessionIssuers.create. Keyed by issuer URL; no record need exist and nothing is persisted. The organization-scoped counterpart of remoteSessionIssuers.fetchMetadata, so creating an organization-level issuer no longer has to borrow an unrelated project's scope. Requires org:admin.")

		Payload(func() {
			Attribute("issuer", String, "Issuer URL to fetch metadata for (e.g. https://login.linear.com).")
			Required("issuer")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionIssuerDraft)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionIssuers.fetchMetadata")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "fetchOrganizationRemoteSessionIssuerMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "fetchMetadata")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "FetchOrganizationRemoteSessionIssuerMetadata"}`)
	})

	Method("refreshIssuerMetadata", func() {
		Description("Re-fetch an existing remote_session_issuer's RFC 8414 metadata document and persist the discovered values. Keyed by issuer id; serves both organizational and project-specific issuers in the caller's organization. Only RFC 8414-derived columns are written — endpoints, the *_supported arrays, client_id_metadata_document_supported, and the documentation URLs. Gram behavior and display fields (oidc, passthrough, name, slug, logo, client setup documentation) are left alone. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionIssuerRefresh)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionIssuers.refreshMetadata")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "refreshOrganizationRemoteSessionIssuerMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "refreshMetadata")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RefreshOrganizationRemoteSessionIssuerMetadata"}`)
	})
})

var CreateRemoteSessionIssuerForm = Type("CreateRemoteSessionIssuerForm", func() {
	Description("Form for creating a remote_session_issuer.")

	Attribute("slug", String, "Project-unique slug.")
	Attribute("issuer", String, "Issuer URL; matches the iss claim.")
	Attribute("name", String, "Optional display name. Stored NULL when empty; clients fall back to the issuer URL/slug.")
	Attribute("logo_asset_id", String, "Optional logo asset id.", func() {
		Format(FormatUUID)
	})
	Attribute("client_setup_documentation_url", String, "URL of OAuth client setup documentation shown when creating clients. Manually set, not RFC 8414; rejected unless an absolute http(s) URL.")
	Attribute("authorization_endpoint", String, "Upstream authorization endpoint.")
	Attribute("token_endpoint", String, "Upstream token endpoint.")
	Attribute("registration_endpoint", String, "Upstream RFC 7591 registration endpoint; absent for issuers without DCR.")
	Attribute("jwks_uri", String, "Upstream JWKS URI.")
	Attribute("service_documentation", String, "RFC 8414 service_documentation; developer documentation for the issuer. Discovered from the issuer metadata document; rejected unless an absolute http(s) URL.")
	Attribute("op_policy_uri", String, "RFC 8414 op_policy_uri; the issuer's client data-usage policy. Discovered from the issuer metadata document; rejected unless an absolute http(s) URL.")
	Attribute("op_tos_uri", String, "RFC 8414 op_tos_uri; the issuer's terms of service. Discovered from the issuer metadata document; rejected unless an absolute http(s) URL.")
	Attribute("scopes_supported", ArrayOf(String), "Scopes advertised by the issuer.")
	Attribute("grant_types_supported", ArrayOf(String), "Grant types advertised by the issuer.")
	Attribute("response_types_supported", ArrayOf(String), "Response types advertised by the issuer.")
	Attribute("token_endpoint_auth_methods_supported", ArrayOf(String), "Token endpoint auth methods advertised by the issuer.")
	Attribute("oidc", Boolean, "When true, may unlock OIDC-aware behaviour. Default false.")
	Attribute("passthrough", Boolean, "When true, the MCP client registers and transacts directly with this issuer. Default false.")
	Attribute("client_id_metadata_document_supported", Boolean, "When true, the issuer accepts a Client ID Metadata Document URL as client_id (OAuth CIMD draft). Discovered from the issuer metadata document and used to pre-flight outbound CIMD. Default false.")

	Required("slug", "issuer")
})

var UpdateRemoteSessionIssuerForm = Type("UpdateRemoteSessionIssuerForm", func() {
	Description("Form for updating a remote_session_issuer. All non-id fields are optional patches.")

	Attribute("id", String, "The remote_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("slug", String, "Rename the slug.")
	Attribute("issuer", String, "Issuer URL; matches the iss claim.")
	Attribute("name", String, "Set or clear the display name. An empty string clears it to NULL.")
	Attribute("logo_asset_id", String, "Set the logo asset id.", func() {
		Format(FormatUUID)
	})
	Attribute("client_setup_documentation_url", String, "Set or clear the URL of OAuth client setup documentation shown when creating clients. An empty string clears it to NULL; any other value must be an absolute http(s) URL.")
	Attribute("authorization_endpoint", String, "Upstream authorization endpoint.")
	Attribute("token_endpoint", String, "Upstream token endpoint.")
	Attribute("registration_endpoint", String, "Upstream RFC 7591 registration endpoint.")
	Attribute("jwks_uri", String, "Upstream JWKS URI.")
	Attribute("service_documentation", String, "Set or clear RFC 8414 service_documentation. An empty string clears it to NULL; any other value must be an absolute http(s) URL.")
	Attribute("op_policy_uri", String, "Set or clear RFC 8414 op_policy_uri. An empty string clears it to NULL; any other value must be an absolute http(s) URL.")
	Attribute("op_tos_uri", String, "Set or clear RFC 8414 op_tos_uri. An empty string clears it to NULL; any other value must be an absolute http(s) URL.")
	Attribute("scopes_supported", ArrayOf(String))
	Attribute("grant_types_supported", ArrayOf(String))
	Attribute("response_types_supported", ArrayOf(String))
	Attribute("token_endpoint_auth_methods_supported", ArrayOf(String))
	Attribute("oidc", Boolean)
	Attribute("passthrough", Boolean)
	Attribute("client_id_metadata_document_supported", Boolean, "Whether the issuer accepts a Client ID Metadata Document URL as client_id (OAuth CIMD draft).")

	Required("id")
})

var RemoteSessionIssuer = Type("RemoteSessionIssuer", func() {
	Meta("struct:pkg:path", "types")

	Description("A remote_session_issuer record — upstream Authorization Server identity that Gram speaks OAuth to.")

	Attribute("id", String, "The remote_session_issuer id.", func() {
		Format(FormatUUID)
	})
	// No FormatUUID: organization-level issuers have no project and serialize
	// this as an empty string, which a UUID format check would reject.
	Attribute("project_id", String, "The owning project id. Empty for organization-level issuers.")
	Attribute("organization_id", String, "The owning organization id. Empty for legacy rows not yet backfilled.")
	Attribute("slug", String, "Project-unique slug.")
	Attribute("issuer", String, "Issuer URL; matches the iss claim.")
	Attribute("name", String, "Optional display name; null when unset.")
	Attribute("logo_asset_id", String, "Optional logo asset id; null when unset.", func() {
		Format(FormatUUID)
	})
	Attribute("client_setup_documentation_url", String, "URL of OAuth client setup documentation shown when creating clients. Manually set, not RFC 8414; null when unset.")
	Attribute("authorization_endpoint", String, "Upstream authorization endpoint.")
	Attribute("token_endpoint", String, "Upstream token endpoint.")
	Attribute("registration_endpoint", String, "Upstream RFC 7591 registration endpoint; null for issuers without DCR.")
	Attribute("jwks_uri", String, "Upstream JWKS URI; null when not advertised.")
	Attribute("service_documentation", String, "RFC 8414 service_documentation; developer documentation for the issuer. Null when not advertised.")
	Attribute("op_policy_uri", String, "RFC 8414 op_policy_uri; the issuer's client data-usage policy. Null when not advertised.")
	Attribute("op_tos_uri", String, "RFC 8414 op_tos_uri; the issuer's terms of service. Null when not advertised.")
	Attribute("scopes_supported", ArrayOf(String))
	Attribute("grant_types_supported", ArrayOf(String))
	Attribute("response_types_supported", ArrayOf(String))
	Attribute("token_endpoint_auth_methods_supported", ArrayOf(String))
	Attribute("oidc", Boolean, "When true, may unlock OIDC-aware behaviour.")
	Attribute("passthrough", Boolean, "When true, the MCP client registers and transacts directly with this issuer.")
	Attribute("client_id_metadata_document_supported", Boolean, "Whether the issuer accepts a Client ID Metadata Document URL as client_id (OAuth CIMD draft).")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "project_id", "organization_id", "slug", "issuer", "oidc", "passthrough", "client_id_metadata_document_supported", "created_at", "updated_at")
})

var RemoteSessionIssuerDraft = Type("RemoteSessionIssuerDraft", func() {
	Meta("struct:pkg:path", "types")

	Description("A draft remote_session_issuer returned by discover. Same shape as RemoteSessionIssuer minus id/project_id/timestamps, plus discovery_warnings describing any RFC 8414 deviations.")

	Attribute("issuer", String, "Issuer URL; matches the iss claim.")
	Attribute("authorization_endpoint", String, "Upstream authorization endpoint.")
	Attribute("token_endpoint", String, "Upstream token endpoint.")
	Attribute("registration_endpoint", String, "Upstream RFC 7591 registration endpoint; null for issuers without DCR.")
	Attribute("jwks_uri", String, "Upstream JWKS URI; null when not advertised.")
	Attribute("service_documentation", String, "RFC 8414 service_documentation; developer documentation for the issuer. Null when not advertised or when the advertised value is not an absolute http(s) URL.")
	Attribute("op_policy_uri", String, "RFC 8414 op_policy_uri; the issuer's client data-usage policy. Null when not advertised or when the advertised value is not an absolute http(s) URL.")
	Attribute("op_tos_uri", String, "RFC 8414 op_tos_uri; the issuer's terms of service. Null when not advertised or when the advertised value is not an absolute http(s) URL.")
	Attribute("scopes_supported", ArrayOf(String))
	Attribute("grant_types_supported", ArrayOf(String))
	Attribute("response_types_supported", ArrayOf(String))
	Attribute("token_endpoint_auth_methods_supported", ArrayOf(String))
	Attribute("oidc", Boolean, "When true, may unlock OIDC-aware behaviour.")
	Attribute("passthrough", Boolean, "When true, the MCP client registers and transacts directly with this issuer.")
	Attribute("client_id_metadata_document_supported", Boolean, "Whether the issuer advertises support for a Client ID Metadata Document URL as client_id (OAuth CIMD draft), parsed from the discovery document.")
	Attribute("discovery_warnings", ArrayOf(String), "Warnings describing any RFC 8414 deviations encountered during discovery.")

	Required("issuer", "oidc", "passthrough", "client_id_metadata_document_supported", "discovery_warnings")
})

var RemoteSessionIssuerRefresh = Type("RemoteSessionIssuerRefresh", func() {
	Meta("struct:pkg:path", "types")

	Description("Result of refreshMetadata: the stored remote_session_issuer as it now stands, plus any warnings raised while re-reading the upstream RFC 8414 document. Distinct from RemoteSessionIssuerDraft, which describes an issuer that may not exist yet and is never persisted.")

	Attribute("issuer", RemoteSessionIssuer, "The remote_session_issuer after the refreshed metadata was persisted.")
	Attribute("discovery_warnings", ArrayOf(String), "Warnings describing any RFC 8414 deviations encountered while re-reading the issuer's metadata document. A refresh that returns warnings still persisted its result; deviations severe enough to distrust the document abort the refresh with an error instead.")

	Required("issuer", "discovery_warnings")
})

var ListRemoteSessionIssuersResult = Type("ListRemoteSessionIssuersResult", func() {
	Description("Result type for listing remote_session_issuers.")

	Attribute("items", ArrayOf(RemoteSessionIssuer))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})
