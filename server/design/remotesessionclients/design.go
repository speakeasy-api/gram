package remotesessionclients

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

func tokenEndpointAuthMethodEnum() {
	Enum("client_secret_basic", "client_secret_post", "none")
}

// scopePattern matches RFC 6749 §3.3 scope-token: printable ASCII
// excluding space, double-quote, and backslash.
const scopePattern = `^[!#-[\]-~]+$`

// audiencePattern matches non-empty printable ASCII with no whitespace.
const audiencePattern = `^[!-~]+$`

// ScopeAttribute applies the scope-token element validation. Exported for the
// platformadmin/remotesessions design, which enforces the same boundary rules
// on the global-client forms.
func ScopeAttribute(description string) {
	Description(description)
	Elem(func() {
		Pattern(scopePattern)
		MaxLength(128)
	})
}

// AudienceAttribute applies the audience validation. Exported for the
// platformadmin/remotesessions design.
func AudienceAttribute() {
	Pattern(audiencePattern)
	MaxLength(512)
}

var _ = Service("remoteSessionClients", func() {
	Description("Manage remote_session_client records — credentials Gram uses when acting as an OAuth client of a remote_session_issuer. client_secret_encrypted is never returned.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createRemoteSessionClient", func() {
		Description("Register a remote_session_client by supplying a client_id and optional client_secret obtained out-of-band from the upstream issuer.")

		Payload(func() {
			Extend(CreateRemoteSessionClientForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/remoteSessionClients.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateRemoteSessionClient"}`)
	})

	Method("createCimd", func() {
		Description("Register a remote_session_client in Client ID Metadata Document (CIMD) mode. Gram generates the client_id (the URL of a hosted client metadata document) and serves the document publicly; the client carries no secret and authenticates with token_endpoint_auth_method=none. The owning issuer must advertise client_id_metadata_document_supported.")

		Payload(func() {
			Extend(CreateCimdForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/remoteSessionClients.createCimd")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createCimdRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "createCimd")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateCimdRemoteSessionClient"}`)
	})

	Method("cloneClientFromOAuthProxyProvider", func() {
		Description("Platform-admin-only. Clone the client_id / client_secret from an existing oauth_proxy_provider into a new remote_session_client paired with the supplied issuers. The upstream secret stays server-side: it is read from the proxy provider's stored secrets, re-encrypted, and persisted on the remote_session_client row without ever crossing the wire.")

		Payload(func() {
			Extend(CloneClientFromOAuthProxyProviderForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/remoteSessionClients.cloneClientFromOAuthProxyProvider")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "cloneClientFromOAuthProxyProvider")
		Meta("openapi:extension:x-speakeasy-name-override", "cloneClientFromOAuthProxyProvider")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CloneClientFromOAuthProxyProvider"}`)
	})

	Method("updateRemoteSessionClient", func() {
		Description("Rotate the client_secret or change the non-issuer settings on an existing remote_session_client. Issuer attachments are managed via attachUserSessionIssuer / detachUserSessionIssuer.")

		Payload(func() {
			Extend(UpdateRemoteSessionClientForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/remoteSessionClients.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateRemoteSessionClient"}`)
	})

	Method("attachUserSessionIssuer", func() {
		Description("Attach a user_session_issuer to a remote_session_client by recording the binding in the join table. Rejected when another client is already bound to the same user_session_issuer for this client's remote_session_issuer.")

		Payload(func() {
			Extend(AttachUserSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/remoteSessionClients.attachUserSessionIssuer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "attachUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "attachUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AttachUserSessionIssuer"}`)
	})

	Method("detachUserSessionIssuer", func() {
		Description("Detach a user_session_issuer from a remote_session_client by removing the binding from the join table. A no-op when the binding does not exist.")

		Payload(func() {
			Extend(DetachUserSessionIssuerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/remoteSessionClients.detachUserSessionIssuer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "detachUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-name-override", "detachUserSessionIssuer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DetachUserSessionIssuer"}`)
	})

	Method("listRemoteSessionClients", func() {
		Description("List remote_session_clients in the caller's project.")

		Payload(func() {
			Attribute("remote_session_issuer_id", String, "Filter to clients registered with this issuer.", func() {
				Format(FormatUUID)
			})
			Attribute("user_session_issuer_id", String, "Filter to clients paired with this user_session_issuer.", func() {
				Format(FormatUUID)
			})
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListRemoteSessionClientsResult)

		HTTP(func() {
			GET("/rpc/remoteSessionClients.list")
			Param("remote_session_issuer_id")
			Param("user_session_issuer_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listRemoteSessionClients")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoteSessionClients"}`)
	})

	Method("getRemoteSessionClient", func() {
		Description("Get a remote_session_client by id.")

		Payload(func() {
			Attribute("id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteSessionClient)

		HTTP(func() {
			GET("/rpc/remoteSessionClients.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoteSessionClient"}`)
	})

	Method("deleteRemoteSessionClient", func() {
		Description("Soft-delete a remote_session_client. Cascades to remote_sessions rows pointing at this client; affected principals are forced to re-authenticate.")

		Payload(func() {
			Attribute("id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/remoteSessionClients.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteRemoteSessionClient"}`)
	})
})

// organizationRemoteSessionClients manages remote_session_clients from the
// organization-administrator surface (AIS-119). Unlike the project-scoped
// remoteSessionClients service above, these methods span both organizational
// (project_id NULL) and project-specific clients in the caller's organization,
// scoped exclusively by organization_id. Security is organization-scoped (no
// ProjectSlug); RBAC gates writes on org:admin and reads on org:read.
var _ = Service("organizationRemoteSessionClients", func() {
	Description("Manage remote_session_client records from the organization-administrator surface — clients across every project in the caller's organization. client_secret_encrypted is never returned.")
	Security(security.Session)
	Security(security.ByKey, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("listClients", func() {
		Description("List the remote_session_clients registered with a given issuer in the caller's organization, each with its MCP server attachment count. Requires org:read.")

		Payload(func() {
			Attribute("issuer_id", String, "The remote_session_issuer id to list clients for.", func() {
				Format(FormatUUID)
			})
			Required("issuer_id")
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(ListOrganizationRemoteSessionClientsResult)

		HTTP(func() {
			GET("/rpc/organizationRemoteSessionClients.list")
			Param("issuer_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listOrganizationRemoteSessionClients")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationRemoteSessionClients"}`)
	})

	Method("getClient", func() {
		Description("Get a remote_session_client in the caller's organization by id. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionClient)

		HTTP(func() {
			GET("/rpc/organizationRemoteSessionClients.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getOrganizationRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationRemoteSessionClient"}`)
	})

	Method("getClientDeletePreflight", func() {
		Description("Authoritative impact summary for deleting a remote_session_client: associated session count and affected MCP server names. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(OrganizationClientDeletePreflight)

		HTTP(func() {
			GET("/rpc/organizationRemoteSessionClients.getDeletePreflight")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getOrganizationRemoteSessionClientDeletePreflight")
		Meta("openapi:extension:x-speakeasy-name-override", "getDeletePreflight")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationRemoteSessionClientDeletePreflight"}`)
	})

	Method("listClientMcpServers", func() {
		Description("List the MCP servers a remote_session_client is attached to (resolved through user_session_issuers) in the caller's organization. Requires org:read.")

		Payload(func() {
			Attribute("client_id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("client_id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(ListOrganizationMcpServersResult)

		HTTP(func() {
			GET("/rpc/organizationRemoteSessionClients.listMcpServers")
			Param("client_id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listOrganizationRemoteSessionClientMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "listMcpServers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationRemoteSessionClientMcpServers"}`)
	})

	Method("createClient", func() {
		Description("Register a standalone remote_session_client under an existing remote_session_issuer in the caller's organization, with no user_session_issuer attachments. The client is project-scoped: it inherits a project-specific issuer's project, or the caller names a project (which must belong to the organization) when the issuer is organization-level. Requires org:admin.")

		Payload(func() {
			Extend(CreateOrganizationRemoteSessionClientForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionClients.create")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createOrganizationRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateOrganizationRemoteSessionClient"}`)
	})

	Method("createCimdClient", func() {
		Description("Register a standalone remote_session_client in Client ID Metadata Document (CIMD) mode under an existing remote_session_issuer in the caller's organization, with no user_session_issuer attachments. Gram generates the client_id and hosts the metadata document; the issuer must advertise client_id_metadata_document_supported. The client is project-scoped: it inherits a project-specific issuer's project, or the caller names a project (which must belong to the organization) when the issuer is organization-level. Requires org:admin.")

		Payload(func() {
			Extend(CreateCimdOrganizationRemoteSessionClientForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionClients.createCimd")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createCimdOrganizationRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "createCimd")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateCimdOrganizationRemoteSessionClient"}`)
	})

	Method("updateClient", func() {
		Description("Update a remote_session_client's non-secret fields in the caller's organization. Requires org:admin.")

		// Reuses the project-scoped UpdateRemoteSessionClientForm: the org-admin
		// update takes the same id + non-secret fields, and the two forms are
		// structurally identical (Speakeasy collapses them to one SDK component),
		// so a separate org form would only fork the generated request-body name.
		Payload(func() {
			Extend(UpdateRemoteSessionClientForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSessionClient)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionClients.update")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateOrganizationRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateOrganizationRemoteSessionClient"}`)
	})

	Method("deleteClient", func() {
		Description("Soft-delete a remote_session_client in the caller's organization. Cascades to the remote_sessions minted against it. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		HTTP(func() {
			DELETE("/rpc/organizationRemoteSessionClients.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteOrganizationRemoteSessionClient")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteOrganizationRemoteSessionClient"}`)
	})

	Method("removeClientFromMcpServer", func() {
		Description("Detach a remote_session_client from an MCP server (clears the MCP server's user_session_issuer link) in the caller's organization. Requires org:admin.")

		Payload(func() {
			Attribute("client_id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Attribute("mcp_server_id", String, "The mcp_server id to detach from.", func() {
				Format(FormatUUID)
			})
			Required("client_id", "mcp_server_id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		HTTP(func() {
			POST("/rpc/organizationRemoteSessionClients.removeFromMcpServer")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "removeOrganizationRemoteSessionClientFromMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "removeFromMcpServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoveOrganizationRemoteSessionClientFromMcpServer"}`)
	})
})

var CreateRemoteSessionClientForm = Type("CreateRemoteSessionClientForm", func() {
	Description("Form for creating a remote_session_client. Caller supplies client_id (and optional client_secret) obtained out-of-band from the upstream issuer.")

	Attribute("remote_session_issuer_id", String, "The owning remote_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_ids", ArrayOf(String), "The user_session_issuers to attach this client to via the join table. Omit or pass an empty array to create a standalone client with no attachments.", func() {
		Elem(func() {
			Format(FormatUUID)
		})
	})
	Attribute("client_id", String, "client_id supplied by the caller.")
	Attribute("client_secret", String, "client_secret supplied by the caller. Gram encrypts before persisting.")
	Attribute("token_endpoint_auth_method", String, "How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.", tokenEndpointAuthMethodEnum)
	Attribute("scope", ArrayOf(String), func() {
		ScopeAttribute("Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.")
	})
	Attribute("audience", String, "Optional upstream OAuth audience to send on the authorize redirect and token exchange.", AudienceAttribute)

	Required("remote_session_issuer_id", "client_id")
})

var CreateCimdForm = Type("CreateCimdForm", func() {
	Description("Form for creating a remote_session_client in Client ID Metadata Document (CIMD) mode. Gram generates the client_id (the URL of a hosted client metadata document) and serves the document publicly; the row carries no secret and authenticates with token_endpoint_auth_method=none. The caller supplies no client_id or credentials.")

	Attribute("remote_session_issuer_id", String, "The owning remote_session_issuer id. Must advertise client_id_metadata_document_supported.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_ids", ArrayOf(String), "The user_session_issuers to attach this client to via the join table. Omit or pass an empty array to create a standalone client with no attachments.", func() {
		Elem(func() {
			Format(FormatUUID)
		})
	})
	Attribute("scope", ArrayOf(String), func() {
		ScopeAttribute("Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.")
	})
	Attribute("audience", String, "Optional upstream OAuth audience to send on the authorize redirect and token exchange.", AudienceAttribute)

	Required("remote_session_issuer_id")
})

var CloneClientFromOAuthProxyProviderForm = Type("CloneClientFromOAuthProxyProviderForm", func() {
	Description("Form for cloning an oauth_proxy_provider's client credentials into a new remote_session_client. The caller supplies the existing oauth_proxy_provider and the remote_session_issuer to register the new client with, plus zero or more user_session_issuers to attach it to.")

	Attribute("oauth_proxy_provider_id", String, "The oauth_proxy_provider to read client_id / client_secret from. Must live in the caller's project.", func() {
		Format(FormatUUID)
	})
	Attribute("remote_session_issuer_id", String, "The remote_session_issuer the new client is registered with.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_ids", ArrayOf(String), "The user_session_issuers to attach the new client to via the join table. Omit or pass an empty array to clone a standalone client with no attachments.", func() {
		Elem(func() {
			Format(FormatUUID)
		})
	})
	Attribute("token_endpoint_auth_method", String, "How the cloned client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.", tokenEndpointAuthMethodEnum)
	Attribute("scope", ArrayOf(String), func() {
		ScopeAttribute("Explicit upstream OAuth scopes the dance should request for the cloned client. Omit to fall back to the issuer's scopes_supported.")
	})
	Attribute("audience", String, "Optional upstream OAuth audience to send on the authorize redirect and token exchange for the cloned client.", AudienceAttribute)

	Required("oauth_proxy_provider_id", "remote_session_issuer_id")
})

var UpdateRemoteSessionClientForm = Type("UpdateRemoteSessionClientForm", func() {
	Description("Form for updating a remote_session_client. All non-id fields are optional patches.")

	Attribute("id", String, "The remote_session_client id.", func() {
		Format(FormatUUID)
	})
	Attribute("client_secret", String, "Rotate the client secret. Gram re-encrypts before persisting.")
	Attribute("token_endpoint_auth_method", String, "Change how the client authenticates at the issuer's token endpoint.", tokenEndpointAuthMethodEnum)
	Attribute("scope", ArrayOf(String), func() {
		ScopeAttribute("Replace the explicit upstream OAuth scopes for this client. Omit to leave unchanged.")
	})
	Attribute("audience", String, "Replace the upstream OAuth audience sent for this client. Omit to leave unchanged.", AudienceAttribute)

	Required("id")
})

var AttachUserSessionIssuerForm = Type("AttachUserSessionIssuerForm", func() {
	Description("Form for attaching a user_session_issuer to a remote_session_client via the join table.")

	Attribute("id", String, "The remote_session_client id.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_id", String, "The user_session_issuer to attach.", func() {
		Format(FormatUUID)
	})

	Required("id", "user_session_issuer_id")
})

var DetachUserSessionIssuerForm = Type("DetachUserSessionIssuerForm", func() {
	Description("Form for detaching a user_session_issuer from a remote_session_client via the join table.")

	Attribute("id", String, "The remote_session_client id.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_id", String, "The user_session_issuer to detach.", func() {
		Format(FormatUUID)
	})

	Required("id", "user_session_issuer_id")
})

var RemoteSessionClient = Type("RemoteSessionClient", func() {
	Meta("struct:pkg:path", "types")

	Description("A remote_session_client record. client_secret_encrypted is never returned.")

	Attribute("id", String, "The remote_session_client id.", func() {
		Format(FormatUUID)
	})
	// No FormatUUID: organization-level clients have no project, and global
	// clients (project_id NULL, organization_id NULL) have neither, so both
	// serialize this as an empty string, which a UUID format check would reject.
	Attribute("project_id", String, "The owning project id. Empty for organization-level and global clients.")
	Attribute("organization_id", String, "The owning organization id. Empty for legacy rows not yet backfilled and global clients.")
	Attribute("remote_session_issuer_id", String, "The owning remote_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_ids", ArrayOf(String), "The user_session_issuers this client is attached to via the join table. Empty for a standalone client with no attachments.", func() {
		Elem(func() {
			Format(FormatUUID)
		})
	})
	Attribute("client_id", String, "The client_id used to identify this client at the issuer's token and authorization endpoints.")
	Attribute("client_id_metadata_uri", String, "When set, the client is in Client ID Metadata Document (CIMD) mode: Gram hosts its OAuth client metadata document at this URL and uses it as the client_id. Null for non-CIMD clients.")
	Attribute("client_id_issued_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("client_secret_expires_at", String, "Null when the secret does not expire.", func() {
		Format(FormatDateTime)
	})
	Attribute("token_endpoint_auth_method", String, "How the client authenticates at the issuer's token endpoint. Null resolves to client_secret_basic at runtime.", tokenEndpointAuthMethodEnum)
	Attribute("scope", ArrayOf(String), "Explicit upstream OAuth scopes the dance requests for this client. Null falls back to the issuer's scopes_supported.")
	Attribute("audience", String, "Upstream OAuth audience sent on the authorize redirect and token exchange. Null omits the audience parameter.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "project_id", "organization_id", "remote_session_issuer_id", "user_session_issuer_ids", "client_id", "client_id_issued_at", "created_at", "updated_at")
})

var ListRemoteSessionClientsResult = Type("ListRemoteSessionClientsResult", func() {
	Description("Result type for listing remote_session_clients.")

	Attribute("items", ArrayOf(RemoteSessionClient))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})
