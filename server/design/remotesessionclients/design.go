package remotesessionclients

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

func tokenEndpointAuthMethodEnum() {
	Enum("client_secret_basic", "client_secret_post")
}

// scopePattern matches RFC 6749 §3.3 scope-token: printable ASCII
// excluding space, double-quote, and backslash.
const scopePattern = `^[!#-[\]-~]+$`

// audiencePattern matches non-empty printable ASCII with no whitespace.
const audiencePattern = `^[!-~]+$`

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
		Description("Rotate the client_secret or change the user_session_issuer_id linkage on an existing remote_session_client.")

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

var CreateRemoteSessionClientForm = Type("CreateRemoteSessionClientForm", func() {
	Description("Form for creating a remote_session_client. Caller supplies client_id (and optional client_secret) obtained out-of-band from the upstream issuer.")

	Attribute("remote_session_issuer_id", String, "The owning remote_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_id", String, "The user_session_issuer this client is paired with.", func() {
		Format(FormatUUID)
	})
	Attribute("client_id", String, "client_id supplied by the caller.")
	Attribute("client_secret", String, "client_secret supplied by the caller. Gram encrypts before persisting.")
	Attribute("token_endpoint_auth_method", String, "How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.", tokenEndpointAuthMethodEnum)
	Attribute("scope", ArrayOf(String), func() {
		scopeAttribute("Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.")
	})
	Attribute("audience", String, "Optional upstream OAuth audience to send on the authorize redirect and token exchange.", audienceAttribute)

	Required("remote_session_issuer_id", "user_session_issuer_id", "client_id")
})

var CloneClientFromOAuthProxyProviderForm = Type("CloneClientFromOAuthProxyProviderForm", func() {
	Description("Form for cloning an oauth_proxy_provider's client credentials into a new remote_session_client. The caller supplies the existing oauth_proxy_provider, plus the remote_session_issuer and user_session_issuer to pair the new client with.")

	Attribute("oauth_proxy_provider_id", String, "The oauth_proxy_provider to read client_id / client_secret from. Must live in the caller's project.", func() {
		Format(FormatUUID)
	})
	Attribute("remote_session_issuer_id", String, "The remote_session_issuer the new client is registered with.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_id", String, "The user_session_issuer the new client is paired with.", func() {
		Format(FormatUUID)
	})
	Attribute("token_endpoint_auth_method", String, "How the cloned client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.", tokenEndpointAuthMethodEnum)
	Attribute("scope", ArrayOf(String), func() {
		scopeAttribute("Explicit upstream OAuth scopes the dance should request for the cloned client. Omit to fall back to the issuer's scopes_supported.")
	})
	Attribute("audience", String, "Optional upstream OAuth audience to send on the authorize redirect and token exchange for the cloned client.", audienceAttribute)

	Required("oauth_proxy_provider_id", "remote_session_issuer_id", "user_session_issuer_id")
})

var UpdateRemoteSessionClientForm = Type("UpdateRemoteSessionClientForm", func() {
	Description("Form for updating a remote_session_client. All non-id fields are optional patches.")

	Attribute("id", String, "The remote_session_client id.", func() {
		Format(FormatUUID)
	})
	Attribute("client_secret", String, "Rotate the client secret. Gram re-encrypts before persisting.")
	Attribute("user_session_issuer_id", String, "Re-pair with a different user_session_issuer.", func() {
		Format(FormatUUID)
	})
	Attribute("token_endpoint_auth_method", String, "Change how the client authenticates at the issuer's token endpoint.", tokenEndpointAuthMethodEnum)
	Attribute("scope", ArrayOf(String), func() {
		scopeAttribute("Replace the explicit upstream OAuth scopes for this client. Omit to leave unchanged.")
	})
	Attribute("audience", String, "Replace the upstream OAuth audience sent for this client. Omit to leave unchanged.", audienceAttribute)

	Required("id")
})

var RemoteSessionClient = Type("RemoteSessionClient", func() {
	Meta("struct:pkg:path", "types")

	Description("A remote_session_client record. client_secret_encrypted is never returned.")

	Attribute("id", String, "The remote_session_client id.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The owning project id.", func() {
		Format(FormatUUID)
	})
	Attribute("remote_session_issuer_id", String, "The owning remote_session_issuer id.", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_id", String, "The user_session_issuer this client is paired with.", func() {
		Format(FormatUUID)
	})
	Attribute("client_id", String, "The client_id used to identify this client at the issuer's token and authorization endpoints.")
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

	Required("id", "project_id", "remote_session_issuer_id", "user_session_issuer_id", "client_id", "client_id_issued_at", "created_at", "updated_at")
})

var ListRemoteSessionClientsResult = Type("ListRemoteSessionClientsResult", func() {
	Description("Result type for listing remote_session_clients.")

	Attribute("items", ArrayOf(RemoteSessionClient))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})
