package usersessionissuers

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/design/usersessionclients"
	cimddesign "github.com/speakeasy-api/gram/server/design/usersessionissuerscimdclients"
)

var _ = Service("userSessionIssuers", func() {
	Description("Manage user_session_issuer records — Speakeasy-side authorization-server configuration that issues user sessions for an MCP server.")
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

	Method("getAttachImpact", func() {
		Description("Preview what attaching a user_session_issuer to an existing MCP server, toolset, or meta MCP server changes for its clients, before calling the matching update. Name exactly one of mcp_server_id, toolset_id, or meta_mcp_server_id; omit user_session_issuer_id to preview detaching the current issuer. Clients and sessions belong to an issuer, so any change of issuer requires the server's clients to authorize again. Advisory only: the update does not require it. Requires the mcp:write access the matching update requires.")

		Payload(func() {
			Attribute("mcp_server_id", String, "The MCP server the issuer would be attached to.", func() {
				Format(FormatUUID)
			})
			Attribute("toolset_id", String, "The toolset the issuer would be attached to.", func() {
				Format(FormatUUID)
			})
			Attribute("meta_mcp_server_id", String, "The meta MCP server the issuer would be attached to.", func() {
				Format(FormatUUID)
			})
			Attribute("user_session_issuer_id", String, "The issuer to attach. Omit to preview detaching the current issuer.", func() {
				Format(FormatUUID)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(UserSessionIssuerAttachImpact)

		HTTP(func() {
			GET("/rpc/userSessionIssuers.getAttachImpact")
			Param("mcp_server_id")
			Param("toolset_id")
			Param("meta_mcp_server_id")
			Param("user_session_issuer_id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getUserSessionIssuerAttachImpact")
		Meta("openapi:extension:x-speakeasy-name-override", "getAttachImpact")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UserSessionIssuerAttachImpact"}`)
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
		Description("List the user_session_issuers in the caller's organization: organization-owned and project-specific, each project-specific issuer with its owning project name. Requires org:read.")
		Payload(func() {
			Attribute("cursor", String, "Pagination cursor: id of the last item from the previous page.", func() { Format(FormatUUID) })
			Attribute("limit", Int, "Page size (default 50, max 100).")
			Attribute("tier", String, "Only issuers from one tier: organization-owned (organization) or project-specific (project). Omitted lists both.", func() {
				Enum("organization", "project")
			})
			security.SessionPayload()
		})
		Result(ListOrganizationUserSessionIssuersResult)
		HTTP(func() {
			GET("/rpc/organizationUserSessionIssuers.list")
			Param("cursor")
			Param("limit")
			Param("tier")
			security.SessionHeader()
			Response(StatusOK)
		})
		shared.CursorPagination()
		Meta("openapi:operationId", "listOrganizationUserSessionIssuers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationUserSessionIssuers"}`)
	})

	Method("getIssuer", func() {
		Description("Get any user_session_issuer in the caller's organization, organization-owned or project-specific, by id. Requires org:read.")
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

	Method("listMcpServers", func() {
		Description("List the live MCP servers, toolsets, and meta MCP servers attached to a user_session_issuer in the caller's organization, organization-owned or project-specific. A toolset served through an MCP server is listed once, as that server. Requires org:read.")
		Payload(func() {
			Attribute("user_session_issuer_id", String, "The user_session_issuer whose attached MCP servers are listed.", func() { Format(FormatUUID) })
			Attribute("cursor", String, "Pagination cursor: id of the last item from the previous page.", func() { Format(FormatUUID) })
			Attribute("limit", Int, "Page size (default 50, max 100).")
			Required("user_session_issuer_id")
			security.SessionPayload()
		})
		Result(ListUserSessionIssuerMcpServersResult)
		HTTP(func() {
			GET("/rpc/organizationUserSessionIssuers.listMcpServers")
			Param("user_session_issuer_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			Response(StatusOK)
		})
		shared.CursorPagination()
		Meta("openapi:operationId", "listOrganizationUserSessionIssuerMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "listMcpServers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationUserSessionIssuerMcpServers"}`)
	})

	Method("getAuthenticationHostImpact", func() {
		Description("Preview moving a user_session_issuer in the caller's organization onto or off the deployment's authentication host: where clients sign in before and after, and how many MCP servers that affects. Requires org:read.")
		Payload(func() {
			Attribute("id", String, "The user_session_issuer id.", func() { Format(FormatUUID) })
			Attribute("use_authentication_host", Boolean, "True to preview moving onto the authentication host, false to preview moving off it.")
			Required("id", "use_authentication_host")
			security.SessionPayload()
		})
		Result(UserSessionIssuerAuthenticationHostImpact)
		HTTP(func() {
			GET("/rpc/organizationUserSessionIssuers.getAuthenticationHostImpact")
			Param("id")
			Param("use_authentication_host")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getOrganizationUserSessionIssuerAuthenticationHostImpact")
		Meta("openapi:extension:x-speakeasy-name-override", "getAuthenticationHostImpact")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationUserSessionIssuerAuthenticationHostImpact"}`)
	})

	Method("setAuthenticationHost", func() {
		Description("Move a user_session_issuer in the caller's organization onto or off the deployment's authentication host. A shared authorization server with a pinned issuer URL is re-pinned to the matching host, so its issuer URL and token endpoint change; other issuers announce their authorization servers on the new host. Clients and anything configured with the previous URLs must be updated. Refused when the deployment has no authentication host, and for issuers managed by Speakeasy. Requires org:admin.")
		Payload(func() {
			Attribute("id", String, "The user_session_issuer id.", func() { Format(FormatUUID) })
			Attribute("use_authentication_host", Boolean, "True to move onto the authentication host, false to move off it.")
			Required("id", "use_authentication_host")
			security.SessionPayload()
		})
		Result(UserSessionIssuer)
		HTTP(func() {
			POST("/rpc/organizationUserSessionIssuers.setAuthenticationHost")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "setOrganizationUserSessionIssuerAuthenticationHost")
		Meta("openapi:extension:x-speakeasy-name-override", "setAuthenticationHost")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetOrganizationUserSessionIssuerAuthenticationHost"}`)
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
		Description("Consolidate a source user_session_issuer onto a target issuer, preserving clients, sessions, attachments, and remote-session credentials before soft-deleting the source. The target must be in the same project or a broader organization scope. Access tokens issued through the authorization_code and refresh_token grants are audience-bound to the issuer, so once a repointed MCP server verifies bearers against the target, outstanding access tokens minted under the source are rejected until the client refreshes. Migrated sessions and refresh tokens keep working, so the refresh succeeds without re-authorization. Tokens from the jwt-bearer (ID-JAG) grant are bound to the resource URL and are unaffected. Requires org:admin.")
		Payload(func() {
			Attribute("source_id", String, "The user_session_issuer to migrate away from; soft-deleted on success.", func() { Format(FormatUUID) })
			Attribute("target_id", String, "The surviving user_session_issuer.", func() { Format(FormatUUID) })
			Attribute("confirmed_warnings_fingerprint", String, "The exact warnings_fingerprint returned by the latest preflight. Required when that preflight reports warnings.")
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

	Method("createJwtBearerClient", func() {
		Description("Register an MCP client on an organization-owned user_session_issuer for enterprise-managed authorization: the client redeems identity assertion grants (ID-JAG) from the organization's identity provider with the jwt-bearer grant. The client has no redirect URIs and cannot use the authorization code flow. For client_secret_basic and client_secret_post, a client secret is generated and returned once, in this response; Speakeasy stores only its hash. Registering a client does not make the exchange work by itself: the issuer must also trust the identity provider that issues the assertions. Requires org:admin.")
		Payload(func() {
			Extend(CreateOrganizationUserSessionIssuerJwtBearerClientForm)
			security.SessionPayload()
		})
		Result(CreateOrganizationUserSessionIssuerJwtBearerClientResult)
		HTTP(func() {
			POST("/rpc/organizationUserSessionIssuers.createJwtBearerClient")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "createOrganizationUserSessionIssuerJwtBearerClient")
		Meta("openapi:extension:x-speakeasy-name-override", "createJwtBearerClient")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateOrganizationUserSessionIssuerJwtBearerClient"}`)
	})
})

var CreateOrganizationUserSessionIssuerJwtBearerClientForm = Type("CreateOrganizationUserSessionIssuerJwtBearerClientForm", func() {
	Description("Form for registering a jwt-bearer (ID-JAG) client on an organization-owned user_session_issuer.")
	Attribute("user_session_issuer_id", String, "The organization-owned user_session_issuer the client is registered on.", func() { Format(FormatUUID) })
	Attribute("client_name", String, "Display name for the client, such as the MCP client application it configures.", func() {
		MinLength(1)
		MaxLength(255)
	})
	Attribute("token_endpoint_auth_method", String, "How the client authenticates at the token endpoint. 'none' registers a public client with no secret. 'client_secret_basic' and 'client_secret_post' register a confidential client and generate its secret.", func() {
		Enum("none", "client_secret_basic", "client_secret_post")
	})
	Required("user_session_issuer_id", "client_name", "token_endpoint_auth_method")
})

var CreateOrganizationUserSessionIssuerJwtBearerClientResult = Type("CreateOrganizationUserSessionIssuerJwtBearerClientResult", func() {
	Description("A newly registered jwt-bearer client and the issuer it belongs to.")
	Attribute("client", usersessionclients.UserSessionClient, "The registered client. client.client_id is the value the identity provider must put in the ID-JAG client_id claim and the MCP client must present at the token endpoint.")
	Attribute("client_secret", String, "The client secret. Present only for client_secret_basic and client_secret_post, and only in this response: it cannot be read again.")
	Attribute("issuer", UserSessionIssuer, "The issuer the client is registered on. For a shared issuer, issuer_url is the ID-JAG audience and token_endpoint is where the client redeems it; otherwise each MCP server announces its own authorization server.")
	Attribute("trusted_identity_provider_configured", Boolean, "Whether the issuer trusts an identity provider's assertions. While false, the token endpoint rejects every ID-JAG this client presents.")
	Required("client", "issuer", "trusted_identity_provider_configured")
})

var CreateUserSessionIssuerForm = Type("CreateUserSessionIssuerForm", func() {
	Attribute("authorization_server_mode", String, "Authorization-server topology. Shared mode supports standalone workload token exchange without an MCP server.", func() {
		Enum("endpoint", "shared")
		Default("endpoint")
	})
	Description("Form for creating a user_session_issuer.")

	userSessionIssuerCreationAttributes()
})

// userSessionIssuerCreationAttributes excludes project-only authorization-server options.
func userSessionIssuerCreationAttributes() {
	Attribute("slug", String, "Issuer slug. Unique for project-owned issuers; organization-owned issuer slugs may repeat.")
	Attribute("authn_challenge_mode", String, "How multi-remote authn challenges are presented: chain | interactive.", func() {
		Enum("chain", "interactive")
	})
	Attribute("session_duration_hours", Int, "Maximum issued user session lifetime, in hours.", func() {
		Minimum(1)
		Maximum(2562047)
	})

	Required("slug", "authn_challenge_mode", "session_duration_hours")
}

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
	Attribute("client_id_metadata_admission_mode", String, "Which CIMD (OAuth Client ID Metadata Document) clients this issuer admits. 'presets' admits Speakeasy's curated catalog plus this issuer's custom URLs; 'open' admits any spec-valid document; 'disabled' admits none and stops advertising CIMD support. Omit to leave unchanged.", func() {
		Enum("disabled", "presets", "open")
	})

	Required("id")
})

var CreateOrganizationUserSessionIssuerForm = Type("CreateOrganizationUserSessionIssuerForm", func() {
	Description("Form for creating an organization-owned user_session_issuer.")
	userSessionIssuerCreationAttributes()
	Attribute("trusted_remote_session_issuer_id", String, "Organization-level or global remote_session_issuer whose assertions this issuer trusts. Omit to leave enterprise-managed authorization disabled.", func() {
		Format(FormatUUID)
	})
	Attribute("trusted_remote_session_client_id", String, "Organization-level remote_session_client Speakeasy uses with the trusted issuer. Must be supplied together with trusted_remote_session_issuer_id.", func() {
		Format(FormatUUID)
	})
})

var UpdateOrganizationUserSessionIssuerForm = Type("UpdateOrganizationUserSessionIssuerForm", func() {
	Description("Form for updating an organization-owned user_session_issuer. All non-id fields are optional patches.")
	Extend(UpdateUserSessionIssuerForm)
	Attribute("trusted_remote_session_issuer_id", String, "Organization-level or global remote_session_issuer whose assertions this issuer trusts. Omit to leave unchanged; pass an empty string to clear the link.")
	Attribute("trusted_remote_session_client_id", String, "Organization-level remote_session_client Speakeasy uses with the trusted issuer. Omit to leave unchanged; pass an empty string to clear the link. The resulting issuer and client must either both be configured or both be absent.")
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
	Attribute("trusted_remote_session_client_id", String, "The organization-level remote_session_client Speakeasy uses with the trusted issuer. Absent when enterprise-managed authorization is disabled.", func() {
		Format(FormatUUID)
	})
	Attribute("authorization_server_mode", String, "Which OAuth authorization server the issuer's MCP servers point clients to. 'endpoint' gives every MCP server its own authorization server, rooted at the server's URL. 'shared' serves one authorization server for the issuer, used by all of its MCP servers; issuer_url and token_endpoint locate it. A shared issuer whose authorization server this deployment does not serve falls back to per-endpoint authorization servers, and authorization_server_unavailable_reason says why.", func() {
		Enum("endpoint", "shared")
	})
	Attribute("use_authentication_host", Boolean, "Whether the issuer opts in to announcing its authorization servers on the deployment's authentication host rather than the MCP host. This is the stored setting: on a deployment without an authentication host it has no effect, and issuer_url shows the host actually used.")
	Attribute("issuer_url_pinned", Boolean, "Whether the shared authorization server's issuer URL was fixed when the issuer was set up, so it stays the same if the deployment's hosts change. An unpinned shared issuer derives its issuer URL from the deployment's current hosts, so a host change also changes the values configured on other platforms. Always false outside shared mode.")
	Attribute("issuer_url", String, "The RFC 8414 issuer identifier of the issuer's shared authorization server. Present only for a shared issuer whose authorization server this deployment serves.")
	Attribute("token_endpoint", String, "The token endpoint of the issuer's shared authorization server. Present exactly when issuer_url is.")
	Attribute("authorization_server_unavailable_reason", String, "Why a shared issuer's authorization server is not served, so its MCP servers fall back to per-endpoint authorization servers: the pinned issuer URL is not this issuer's shared authorization server (pinned_issuer_url_invalid), or it is on a host this deployment does not serve (host_not_served). Absent otherwise.", func() {
		Enum("pinned_issuer_url_invalid", "host_not_served")
	})
	Attribute("project_name", String, "The owning project's name. Present on organization-level reads of project-specific issuers.")
	Attribute("authentication_host_available", Boolean, "Whether this deployment has an authentication host the issuer can be moved onto. Present on responses, not audit snapshots.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "project_id", "organization_id", "slug", "authn_challenge_mode", "session_duration_hours", "client_id_metadata_admission_mode", "authorization_server_mode", "use_authentication_host", "issuer_url_pinned", "created_at", "updated_at")
})

var UserSessionIssuerAuthenticationHostImpact = Type("UserSessionIssuerAuthenticationHostImpact", func() {
	Description("What moving a user_session_issuer onto or off the authentication host changes for its clients.")
	Attribute("issuer", UserSessionIssuer, "The issuer as it is now.")
	Attribute("authorization_server_before", String, "Where clients sign in now: the shared authorization server's issuer URL, or the origin that announces each MCP server's own authorization server. Empty for a shared issuer this deployment does not serve.")
	Attribute("authorization_server_after", String, "Where clients sign in after the move, in the same terms.")
	Attribute("changes", Boolean, "Whether the move changes where clients sign in. False when the issuer is already where it would move.")
	Attribute("mcp_server_count", Int, "Live MCP servers, toolsets, and meta MCP servers attached to the issuer. Those on a custom domain keep announcing their own authorization servers there.")
	Required("issuer", "authorization_server_before", "authorization_server_after", "changes", "mcp_server_count")
})

var UserSessionIssuerAttachImpact = Type("UserSessionIssuerAttachImpact", func() {
	Description("What attaching a user_session_issuer to an existing MCP server changes for its clients.")
	Attribute("current_issuer", UserSessionIssuer, "The issuer the server uses now. Absent when it has none.")
	Attribute("candidate_issuer", UserSessionIssuer, "The issuer that would be attached. Absent when previewing a detach.")
	Attribute("reauthorization_required", Boolean, "True when the server has an issuer and the change replaces or removes it. Its clients' registrations and sessions belong to the current issuer, so every client must register and authorize again.")
	Attribute("authorization_server_changes", Boolean, "True when the authorization server the server advertises to clients changes: its own per-server authorization server, a shared authorization server's issuer URL, or none. Anything configured with the current authorization server, such as an identity provider or a federated platform, must be updated.")
	Required("reauthorization_required", "authorization_server_changes")
})

var ListUserSessionIssuersResult = Type("ListUserSessionIssuersResult", func() {
	Description("Result type for listing user_session_issuers.")

	Attribute("items", ArrayOf(UserSessionIssuer))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})

var ListOrganizationUserSessionIssuersResult = Type("ListOrganizationUserSessionIssuersResult", func() {
	Description("Result type for listing the user_session_issuers in an organization.")
	Attribute("items", ArrayOf(UserSessionIssuer))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")
	Required("items")
})

var UserSessionIssuerMcpServer = Type("UserSessionIssuerMcpServer", func() {
	Description("A live MCP server, toolset, or meta MCP server attached to a user_session_issuer.")
	Attribute("kind", String, "Which kind of MCP server this is.", func() {
		Enum("mcp_server", "toolset", "meta_mcp_server")
	})
	Attribute("id", String, "The MCP server, toolset, or meta MCP server id.", func() { Format(FormatUUID) })
	Attribute("name", String, "The display name.")
	Attribute("slug", String, "The MCP server or toolset slug. Absent for meta MCP servers and MCP servers without one.")
	Attribute("project_id", String, "The owning project id.", func() { Format(FormatUUID) })
	Attribute("project_name", String, "The owning project name.")
	Required("kind", "id", "name", "project_id", "project_name")
})

var ListUserSessionIssuerMcpServersResult = Type("ListUserSessionIssuerMcpServersResult", func() {
	Description("Result type for listing the MCP servers attached to a user_session_issuer.")
	Attribute("items", ArrayOf(UserSessionIssuerMcpServer))
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
	Attribute("can_delete", Boolean, "True when no live MCP server, toolset, or active identity-chaining binding references the issuer.")
	Required("client_count", "live_session_count", "mcp_servers", "toolsets", "can_delete")
	Attribute("ema_binding_count", Int64, "Active identity-chaining bindings that must be explicitly unlinked before deletion.")
	Required("ema_binding_count")
	Attribute("withdraws_organization_gateway", Boolean, "True when this is the organization's only organization-level issuer, so deleting it stops serving the organization gateway its members sign in to. Does not block deletion.")
	Required("withdraws_organization_gateway")
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
	Attribute("warnings", ArrayOf(UserSessionIssuerFieldMismatch), "Configuration differences that do not block the migration. The target's values become authoritative for future authorization; existing sessions and refresh tokens are unaffected. Besides the issuer settings, this reports authorization_server_mode, use_authentication_host, and issuer_url (the shared authorization server's issuer identifier, empty when not served) whenever they differ: repointed MCP servers then direct clients to a different authorization server, so anything configured with the source's values, such as an identity provider or a federated platform, must be updated.")
	Attribute("warnings_fingerprint", String, "Stable fingerprint of the current warnings. Empty when there are no warnings; otherwise pass this exact value to migrateIssuer to confirm them.")
	Attribute("can_migrate", Boolean, "True when no hard blocker is present.")
	Required("client_count", "session_count", "consent_count", "cimd_client_count", "remote_session_count", "conflicting_client_ids", "principal_binding_conflict_count", "ema_binding_conflict_count", "platform_owned", "warnings", "warnings_fingerprint", "can_migrate")
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
