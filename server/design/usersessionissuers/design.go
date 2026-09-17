package usersessionissuers

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	cimddesign "github.com/speakeasy-api/gram/server/design/usersessionissuerscimdclients"
)

var _ = Service("userSessionIssuers", func() {
	Description("Manage user_session_issuer records — Gram-side authorization-server configuration that issues user sessions for an MCP server.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createUserSessionIssuer", func() {
		Description("Create a new user_session_issuer.")

		Payload(func() {
			Extend(CreateUserSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UserSessionIssuer)

		HTTP(func() {
			POST("/rpc/userSessionIssuers.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateUserSessionIssuer"}`)
	})

	Method("updateUserSessionIssuer", func() {
		Description("Update fields on an existing user_session_issuer.")

		Payload(func() {
			Extend(UpdateUserSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UserSessionIssuer)

		HTTP(func() {
			POST("/rpc/userSessionIssuers.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateUserSessionIssuer"}`)
	})

	Method("listUserSessionIssuers", func() {
		Description("List user_session_issuers in the caller's project.")

		Payload(func() {
			Attribute("cursor", String, "Pagination cursor: id of the last item from the previous page.", func() {
				Format(FormatUUID)
			})
			Attribute("limit", Int, "Page size (default 50, max 100).")
			Attribute("mcp_resource_id", String, "Optional MCP server or toolset resource ID used to authorize callers with resource-scoped mcp:write access. The resource must belong to the selected project.", func() {
				Format(FormatUUID)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListUserSessionIssuersResult)

		HTTP(func() {
			GET("/rpc/userSessionIssuers.list")
			Param("cursor")
			Param("limit")
			Param("mcp_resource_id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listUserSessionIssuers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionIssuers"}`)
	})

	Method("getUserSessionIssuer", func() {
		Description("Get a user_session_issuer by id or by slug. Provide exactly one.")

		Payload(func() {
			Attribute("id", String, "The user_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "The user_session_issuer slug.")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UserSessionIssuer)

		HTTP(func() {
			GET("/rpc/userSessionIssuers.get")
			Param("id")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionIssuer"}`)
	})

	Method("deleteUserSessionIssuer", func() {
		Description("Soft-delete a user_session_issuer. Cascades to dependent user_sessions, user_session_consents, and remote_session_clients.")

		Payload(func() {
			Attribute("id", String, "The user_session_issuer id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/userSessionIssuers.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteUserSessionIssuer"}`)
	})
})

var _ = Service("organizationUserSessionIssuers", func() {
	Description("Manage organization-owned user_session_issuer records inherited by every project in the caller's organization.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("createIssuer", func() {
		Description("Create an organization-owned user_session_issuer. Requires org:admin.")
		Payload(func() {
			Extend(CreateOrganizationUserSessionIssuerForm)
			security.SessionPayload()
		})
		Result(UserSessionIssuer)
		HTTP(func() {
			POST("/rpc/organizationUserSessionIssuers.create")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createOrganizationUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateOrganizationUserSessionIssuer"}`)
	})

	Method("listIssuers", func() {
		Description("List organization-owned user_session_issuers. Requires org:read.")
		Payload(func() {
			Attribute("cursor", String, "Pagination cursor: id of the last item from the previous page.", func() { Format(FormatUUID) })
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
		})
		Result(ListOrganizationUserSessionIssuersResult)
		HTTP(func() {
			GET("/rpc/organizationUserSessionIssuers.list")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			Response(StatusOK)
		})
		shared.CursorPagination()
		Meta("openapi:operationId", "listOrganizationUserSessionIssuers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationUserSessionIssuers"}`)
	})

	Method("getIssuer", func() {
		Description("Get an organization-owned user_session_issuer by id. Requires org:read.")
		Payload(func() {
			Attribute("id", String, "The user_session_issuer id.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
		})
		Result(UserSessionIssuer)
		HTTP(func() {
			GET("/rpc/organizationUserSessionIssuers.get")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getOrganizationUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationUserSessionIssuer"}`)
	})

	Method("updateIssuer", func() {
		Description("Update an organization-owned user_session_issuer. Requires org:admin.")
		Payload(func() {
			Extend(UpdateOrganizationUserSessionIssuerForm)
			security.SessionPayload()
		})
		Result(UserSessionIssuer)
		HTTP(func() {
			POST("/rpc/organizationUserSessionIssuers.update")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "updateOrganizationUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateOrganizationUserSessionIssuer"}`)
	})

	Method("getIssuerDeletePreflight", func() {
		Description("Report the clients, live sessions, MCP servers, and toolsets affected by deleting an organization-owned user_session_issuer. Requires org:read.")
		Payload(func() {
			Attribute("id", String, "The user_session_issuer id.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
		})
		Result(OrganizationUserSessionIssuerDeletePreflight)
		HTTP(func() {
			GET("/rpc/organizationUserSessionIssuers.getDeletePreflight")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getOrganizationUserSessionIssuerDeletePreflight")
		Meta("openapi:extension:x-speakeasy-name-override", "getDeletePreflight")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationUserSessionIssuerDeletePreflight"}`)
	})

	Method("deleteIssuer", func() {
		Description("Soft-delete an organization-owned user_session_issuer. Refuses while a live MCP server or toolset references it. Requires org:admin.")
		Payload(func() {
			Attribute("id", String, "The user_session_issuer id.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
		})
		HTTP(func() {
			DELETE("/rpc/organizationUserSessionIssuers.delete")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "deleteOrganizationUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteOrganizationUserSessionIssuer"}`)
	})

	Method("moveIssuer", func() {
		Description("Re-scope a user_session_issuer in the caller's organization. Provide a project_id to make it project-specific, or omit it to make it organization-owned. Existing clients and sessions move with the issuer. Requires org:admin.")
		Payload(func() {
			Attribute("id", String, "The user_session_issuer id.", func() { Format(FormatUUID) })
			Attribute("project_id", String, "Target owning project id. Omit to make the issuer organization-owned.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
		})
		Result(UserSessionIssuer)
		HTTP(func() {
			POST("/rpc/organizationUserSessionIssuers.move")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "moveOrganizationUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "move")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "MoveOrganizationUserSessionIssuer"}`)
	})

	Method("getIssuerMigratePreflight", func() {
		Description("Report the impact, blockers, and configuration warnings for consolidating one user_session_issuer onto another. Requires org:read.")
		Payload(func() {
			Attribute("source_id", String, "The user_session_issuer to migrate away from.", func() { Format(FormatUUID) })
			Attribute("target_id", String, "The user_session_issuer to migrate onto.", func() { Format(FormatUUID) })
			Required("source_id", "target_id")
			security.SessionPayload()
		})
		Result(OrganizationUserSessionIssuerMigratePreflight)
		HTTP(func() {
			GET("/rpc/organizationUserSessionIssuers.getMigratePreflight")
			Param("source_id")
			Param("target_id")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getOrganizationUserSessionIssuerMigratePreflight")
		Meta("openapi:extension:x-speakeasy-name-override", "getMigratePreflight")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationUserSessionIssuerMigratePreflight"}`)
	})

	Method("migrateIssuer", func() {
		Description("Consolidate a source user_session_issuer onto a target issuer, preserving clients, sessions, attachments, and remote-session credentials before soft-deleting the source. The target must be in the same project or a broader organization scope. Requires org:admin.")
		Payload(func() {
			Attribute("source_id", String, "The user_session_issuer to migrate away from; soft-deleted on success.", func() { Format(FormatUUID) })
			Attribute("target_id", String, "The surviving user_session_issuer.", func() { Format(FormatUUID) })
			Required("source_id", "target_id")
			security.SessionPayload()
		})
		Result(MigrateOrganizationUserSessionIssuerResult)
		HTTP(func() {
			POST("/rpc/organizationUserSessionIssuers.migrate")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "migrateOrganizationUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "migrate")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "MigrateOrganizationUserSessionIssuer"}`)
	})

	Method("createCimdClient", func() {
		Description("Allow an additional CIMD document URL on an organization-owned user_session_issuer. Requires org:admin.")
		Payload(func() {
			Extend(cimddesign.CreateUserSessionIssuerCimdClientForm)
			security.SessionPayload()
		})
		Result(cimddesign.CreateUserSessionIssuerCimdClientResult)
		HTTP(func() {
			POST("/rpc/organizationUserSessionIssuers.createCimdClient")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createOrganizationUserSessionIssuerCimdClient")
		Meta("openapi:extension:x-speakeasy-name-override", "createCimdClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateOrganizationUserSessionIssuerCimdClient"}`)
	})

	Method("listCimdClients", func() {
		Description("List custom CIMD document URLs on an organization-owned user_session_issuer. Requires org:read.")
		Payload(func() {
			Attribute("user_session_issuer_id", String, "The organization-owned user_session_issuer whose custom CIMD clients are listed.", func() { Format(FormatUUID) })
			Attribute("cursor", String, "Pagination cursor: id of the last item from the previous page.", func() { Format(FormatUUID) })
			Attribute("limit", Int, "Page size (default 50, max 100).")
			Required("user_session_issuer_id")
			security.SessionPayload()
		})
		Result(cimddesign.ListUserSessionIssuerCimdClientsResult)
		HTTP(func() {
			GET("/rpc/organizationUserSessionIssuers.listCimdClients")
			Param("user_session_issuer_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			Response(StatusOK)
		})
		shared.CursorPagination()
		Meta("openapi:operationId", "listOrganizationUserSessionIssuerCimdClients")
		Meta("openapi:extension:x-speakeasy-name-override", "listCimdClients")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationUserSessionIssuerCimdClients"}`)
	})

	Method("getCimdClient", func() {
		Description("Get a custom CIMD document URL on an organization-owned user_session_issuer. Requires org:read.")
		Payload(func() {
			Attribute("id", String, "The user_session_issuer_cimd_client id.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
		})
		Result(cimddesign.UserSessionIssuerCimdClient)
		HTTP(func() {
			GET("/rpc/organizationUserSessionIssuers.getCimdClient")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getOrganizationUserSessionIssuerCimdClient")
		Meta("openapi:extension:x-speakeasy-name-override", "getCimdClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationUserSessionIssuerCimdClient"}`)
	})

	Method("deleteCimdClient", func() {
		Description("Remove a custom CIMD document URL from an organization-owned user_session_issuer. Requires org:admin.")
		Payload(func() {
			Attribute("id", String, "The user_session_issuer_cimd_client id.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
		})
		HTTP(func() {
			DELETE("/rpc/organizationUserSessionIssuers.deleteCimdClient")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "deleteOrganizationUserSessionIssuerCimdClient")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteCimdClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteOrganizationUserSessionIssuerCimdClient"}`)
	})
})

var CreateUserSessionIssuerForm = Type("CreateUserSessionIssuerForm", func() {
	Description("Form for creating a user_session_issuer.")

	Attribute("slug", String, "Issuer slug. Unique for project-owned issuers; organization-owned issuer slugs may repeat.")
	Attribute("authn_challenge_mode", String, "How multi-remote authn challenges are presented: chain | interactive.", func() {
		Enum("chain", "interactive")
	})
	Attribute("session_duration_hours", Int, "Maximum issued user session lifetime, in hours.", func() {
		Minimum(1)
		Maximum(2562047)
	})

	Required("slug", "authn_challenge_mode", "session_duration_hours")
})

var UpdateUserSessionIssuerForm = Type("UpdateUserSessionIssuerForm", func() {
	Description("Form for updating a user_session_issuer. All non-id fields are optional patches.")

	Attribute("id", String, "The user_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("slug", String, "Rename the slug.")
	Attribute("authn_challenge_mode", String, "chain | interactive.", func() {
		Enum("chain", "interactive")
	})
	Attribute("session_duration_hours", Int, "Maximum issued user session lifetime, in hours.", func() {
		Minimum(1)
		Maximum(2562047)
	})
	Attribute("client_id_metadata_admission_mode", String, "Which CIMD (OAuth Client ID Metadata Document) clients this issuer admits. 'presets' admits Gram's curated catalog plus this issuer's custom URLs; 'open' admits any spec-valid document; 'disabled' admits none and stops advertising CIMD support. Omit to leave unchanged.", func() {
		Enum("disabled", "presets", "open")
	})

	Required("id")
})

var CreateOrganizationUserSessionIssuerForm = Type("CreateOrganizationUserSessionIssuerForm", func() {
	Description("Form for creating an organization-owned user_session_issuer.")
	Extend(CreateUserSessionIssuerForm)
	Attribute("trusted_remote_session_issuer_id", String, "Organization-level or global remote_session_issuer whose assertions this issuer trusts. Omit to leave enterprise-managed authorization disabled.", func() {
		Format(FormatUUID)
	})
	Attribute("trusted_remote_session_client_id", String, "Organization-level remote_session_client Gram uses with the trusted issuer. Must be supplied together with trusted_remote_session_issuer_id.", func() {
		Format(FormatUUID)
	})
})

var UpdateOrganizationUserSessionIssuerForm = Type("UpdateOrganizationUserSessionIssuerForm", func() {
	Description("Form for updating an organization-owned user_session_issuer. All non-id fields are optional patches.")
	Extend(UpdateUserSessionIssuerForm)
	Attribute("trusted_remote_session_issuer_id", String, "Organization-level or global remote_session_issuer whose assertions this issuer trusts. Omit to leave unchanged; pass an empty string to clear the link.")
	Attribute("trusted_remote_session_client_id", String, "Organization-level remote_session_client Gram uses with the trusted issuer. Omit to leave unchanged; pass an empty string to clear the link. The resulting issuer and client must either both be configured or both be absent.")
})

var UserSessionIssuer = Type("UserSessionIssuer", func() {
	Meta("struct:pkg:path", "types")

	Description("A user_session_issuer record.")

	Attribute("id", String, "The user_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The owning project id; empty for organization-owned issuers.")
	Attribute("organization_id", String, "The owning organization id.")
	Attribute("slug", String, "Issuer slug. Unique for project-owned issuers; organization-owned issuer slugs may repeat.")
	Attribute("authn_challenge_mode", String, "chain | interactive.")
	Attribute("session_duration_hours", Int, "Maximum issued user session lifetime, in hours.")
	Attribute("client_id_metadata_admission_mode", String, "The EFFECTIVE CIMD admission policy in force for this issuer: disabled | presets | reporting | open. Always populated, so clients never have to reason about an unset state. 'open' is the resting policy an issuer carries unless an operator chooses otherwise: it admits any spec-valid document, and 'presets' enforcement is opt-in because a denial under it is unrecoverable for the end user. Note 'reporting' can be READ but not written: it is a legacy value that admits exactly what 'open' admits, and no issuer is created with it.", func() {
		Enum("disabled", "presets", "reporting", "open")
	})
	Attribute("trusted_remote_session_issuer_id", String, "The organization-level or global remote_session_issuer whose assertions this issuer trusts. Absent when enterprise-managed authorization is disabled.", func() {
		Format(FormatUUID)
	})
	Attribute("trusted_remote_session_client_id", String, "The organization-level remote_session_client Gram uses with the trusted issuer. Absent when enterprise-managed authorization is disabled.", func() {
		Format(FormatUUID)
	})
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "project_id", "organization_id", "slug", "authn_challenge_mode", "session_duration_hours", "client_id_metadata_admission_mode", "created_at", "updated_at")
})

var ListUserSessionIssuersResult = Type("ListUserSessionIssuersResult", func() {
	Description("Result type for listing user_session_issuers.")

	Attribute("items", ArrayOf(UserSessionIssuer))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})

var ListOrganizationUserSessionIssuersResult = Type("ListOrganizationUserSessionIssuersResult", func() {
	Description("Result type for listing organization-owned user_session_issuers.")
	Attribute("items", ArrayOf(UserSessionIssuer))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")
	Required("items")
})

var OrganizationUserSessionIssuerReference = Type("OrganizationUserSessionIssuerReference", func() {
	Description("A live project resource that references an organization-owned user_session_issuer.")
	Attribute("id", String, "The referencing resource id.", func() { Format(FormatUUID) })
	Attribute("name", String, "The referencing resource display name.")
	Attribute("project_id", String, "The owning project id.", func() { Format(FormatUUID) })
	Attribute("project_name", String, "The owning project name.")
	Required("id", "name", "project_id", "project_name")
})

var OrganizationUserSessionIssuerDeletePreflight = Type("OrganizationUserSessionIssuerDeletePreflight", func() {
	Description("Authoritative impact summary for deleting an organization-owned user_session_issuer.")
	Attribute("client_count", Int, "Number of non-deleted user_session_clients registered with the issuer.")
	Attribute("live_session_count", Int, "Number of non-deleted, unexpired user_sessions issued by the issuer.")
	Attribute("mcp_servers", ArrayOf(OrganizationUserSessionIssuerReference), "Live MCP servers that block deletion.")
	Attribute("toolsets", ArrayOf(OrganizationUserSessionIssuerReference), "Live toolsets that block deletion.")
	Attribute("can_delete", Boolean, "True when no live MCP server or toolset references the issuer.")
	Required("client_count", "live_session_count", "mcp_servers", "toolsets", "can_delete")
})

var UserSessionIssuerFieldMismatch = Type("UserSessionIssuerFieldMismatch", func() {
	Description("One user_session_issuer setting whose source and target values differ.")
	Attribute("field", String, "The differing field name.")
	Attribute("source_value", String, "The source issuer's rendered value. Empty when unset.")
	Attribute("target_value", String, "The target issuer's rendered value. Empty when unset.")
	Required("field", "source_value", "target_value")
})

var OrganizationUserSessionIssuerMigratePreflight = Type("OrganizationUserSessionIssuerMigratePreflight", func() {
	Description("Authoritative impact summary for consolidating one user_session_issuer onto another.")
	Attribute("client_count", Int, "Clients that would move.")
	Attribute("session_count", Int, "User sessions that would move.")
	Attribute("consent_count", Int, "User-session consents that would move.")
	Attribute("cimd_client_count", Int, "Custom CIMD allowlist entries that would move or merge.")
	Attribute("remote_session_count", Int, "Remote-session credentials whose issuer provenance would move.")
	Attribute("conflicting_client_ids", ArrayOf(String), "Active OAuth client ids already present on both issuers. Non-empty blocks migration.")
	Attribute("principal_binding_conflict_count", Int, "Active principal bindings that would violate target uniqueness. Non-zero blocks migration.")
	Attribute("ema_binding_conflict_count", Int, "Enterprise-managed authorization bindings already present on the target. Non-zero blocks migration.")
	Attribute("platform_owned", Boolean, "Whether a Platform MCP catalog registration owns either issuer. True blocks migration.")
	Attribute("warnings", ArrayOf(UserSessionIssuerFieldMismatch), "Configuration differences that do not invalidate existing sessions but change future authorization behavior.")
	Attribute("can_migrate", Boolean, "True when no hard blocker is present.")
	Required("client_count", "session_count", "consent_count", "cimd_client_count", "remote_session_count", "conflicting_client_ids", "principal_binding_conflict_count", "ema_binding_conflict_count", "platform_owned", "warnings", "can_migrate")
})

var MigrateOrganizationUserSessionIssuerResult = Type("MigrateOrganizationUserSessionIssuerResult", func() {
	Description("Outcome of consolidating a source user_session_issuer onto a target issuer.")
	Attribute("issuer", UserSessionIssuer, "The surviving target issuer.")
	Attribute("clients_migrated", Int, "Clients re-pointed to the target.")
	Attribute("sessions_migrated", Int, "User sessions re-pointed to the target.")
	Attribute("consents_migrated", Int, "Consents normalized to the target scope.")
	Attribute("cimd_clients_migrated", Int, "CIMD allowlist entries moved or merged.")
	Attribute("remote_sessions_migrated", Int, "Remote-session credentials whose provenance moved.")
	Attribute("source_deleted", Boolean, "True when the source issuer was soft-deleted.")
	Required("issuer", "clients_migrated", "sessions_migrated", "consents_migrated", "cimd_clients_migrated", "remote_sessions_migrated", "source_deleted")
})
