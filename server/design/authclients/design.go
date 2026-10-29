package authclients

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

func clientTypeEnum() {
	Enum("remote_session", "service_account", "static_credential", "pass_through", "ema")
}

func upstreamAuthStatusEnum() {
	Enum("configured", "setup_incomplete", "no_auth_required", "undecided")
}

var AuthClientSource = Type("AuthClientSource", String, func() {
	Meta("struct:pkg:path", "types")
	Enum("backfill", "admin")
	Meta("openapi:extension:x-speakeasy-name-override", "AuthClientSource")
})

var AuthClientHeader = Type("AuthClientHeader", func() {
	Meta("struct:pkg:path", "types")
	Description("A header an auth client sends upstream. Stored values are never returned.")

	Attribute("name", String, "The header name.")
	Attribute("description", String, "What the header is for.")
	Attribute("is_required", Boolean, "Whether a caller must supply the header when it is copied from the request.")
	Attribute("is_secret", Boolean, "Whether the stored value is secret.")
	Attribute("value_from_request_header", String, "The request header the value is copied from, for pass-through headers.")
	Attribute("has_stored_value", Boolean, "Whether the client stores a value for this header.")

	Required("name", "is_required", "is_secret", "has_stored_value")
})

var AuthClientServer = Type("AuthClientServer", func() {
	Meta("struct:pkg:path", "types")
	Description("An MCP server an auth client is associated with.")

	Attribute("id", String, "The MCP server id.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "The MCP server's project.", func() { Format(FormatUUID) })
	Attribute("name", String, "The MCP server's name.")
	Attribute("slug", String, "The MCP server's slug.")
	Attribute("public", Boolean, "Whether the MCP server is public.")

	Required("id", "project_id", "public")
})

var AuthClient = Type("AuthClient", func() {
	Meta("struct:pkg:path", "types")
	Description("A configured way for Speakeasy to authenticate to an upstream MCP server. Who may use it is controlled by mcp:auth grants on the client, never by who configured it. Auth clients are typed auth configuration: they take effect only where typed auth client resolution is enforced, which is being rolled out in stages. Elsewhere, including in shadow mode, an MCP server's existing upstream configuration keeps serving requests.")

	Attribute("id", String, "The auth client id.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "The project the client belongs to. Omitted for a client shared across the organization's projects.", func() { Format(FormatUUID) })
	Attribute("client_type", String, "How the client authenticates upstream.", clientTypeEnum)
	Attribute("source", AuthClientSource, "backfill when the client was created from existing configuration, admin when an administrator created it.")
	Attribute("name", String, "The client's name.")
	Attribute("enabled", Boolean, "Whether the client is enabled. Where typed auth client resolution is enforced, a disabled client is never selected or resolved. Disabling revokes no upstream credentials, and elsewhere, including in shadow mode, an MCP server's existing upstream configuration keeps serving.")
	Attribute("remote_session_client_id", String, "The OAuth registration behind remote_session, service_account and ema clients.", func() { Format(FormatUUID) })
	Attribute("personal", Boolean, "Whether each caller uses their own connection through the client. A grant on a personal client never lets one user use another user's connection.")
	Attribute("headers", ArrayOf(AuthClientHeader), "The headers the client sends upstream.")
	Attribute("servers", ArrayOf(AuthClientServer), "The MCP servers that offer the client. Where typed auth client resolution is enforced, changing or disabling the client changes upstream behavior on every one of them.")
	Attribute("created_at", String, "When the client was created.", func() { Format(FormatDateTime) })
	Attribute("updated_at", String, "When the client was last updated.", func() { Format(FormatDateTime) })

	Required("id", "client_type", "source", "name", "enabled", "personal", "headers", "servers", "created_at", "updated_at")
})

var McpServerAuthentication = Type("McpServerAuthentication", func() {
	Meta("struct:pkg:path", "types")
	Description("How an MCP server authenticates to its upstream.")

	Attribute("mcp_server_id", String, "The MCP server id.", func() { Format(FormatUUID) })
	Attribute("project_id", String, "The MCP server's project.", func() { Format(FormatUUID) })
	Attribute("public", Boolean, "Whether the MCP server is public.")
	Attribute("requires_upstream_auth", Boolean, "Whether the upstream needs authentication. Omitted while undecided.")
	Attribute("status", String, "configured when authentication is required and at least one enabled client is offered; setup_incomplete when it is required and none is; no_auth_required when the upstream explicitly needs none; undecided when nobody has said.", upstreamAuthStatusEnum)
	Attribute("clients", ArrayOf(AuthClient), "The auth clients the server offers.")

	Required("mcp_server_id", "project_id", "public", "status", "clients")
})

func audienceLevelEnum() {
	Enum("use", "blocked")
}

var AuthClientAudienceEntry = Type("AuthClientAudienceEntry", func() {
	Meta("struct:pkg:path", "types")
	Description("One rule deciding who may use an auth client. The rule applies wherever the client is used, on every server that offers it.")

	Attribute("principal_urn", String, "The principal the rule names: user:<id>, user:all, role:<id> or agent:<id>.")
	Attribute("level", String, "use grants mcp:auth on the client; blocked writes mcp:blocked_auth, which outranks grants.", audienceLevelEnum)
	Attribute("applies_to", String, "client when the rule names this client and can be edited here; client_type when it covers every client of this type; all_clients when it covers every client. Rules that do not name this client are shown for context and edited elsewhere.", func() {
		Enum("client", "client_type", "all_clients")
	})
	Attribute("project_id", String, "The project the rule is confined to, when it is.", func() { Format(FormatUUID) })
	Attribute("connection_note", String, "For personal clients: how the rule relates to other people's connections. A grant never lets anyone use another user's connection.")

	Required("principal_urn", "level", "applies_to")
})

var AuthClientAudience = Type("AuthClientAudience", func() {
	Meta("struct:pkg:path", "types")
	Description("Who may use an auth client.")

	Attribute("auth_client_id", String, "The auth client id.", func() { Format(FormatUUID) })
	Attribute("entries", ArrayOf(AuthClientAudienceEntry), "The rules deciding who may use the client.")
	Attribute("version", String, "Pass back as expected_version when saving, so an edit made against a stale list is refused.")
	Attribute("public_servers", ArrayOf(String), "Ids of public servers that offer the client. Anonymous callers on them never acquire these rules.")

	Required("auth_client_id", "entries", "version", "public_servers")
})

func creatableClientTypeEnum() {
	Enum("remote_session", "service_account", "static_credential", "pass_through")
}

var AuthClientHeaderInput = Type("AuthClientHeaderInput", func() {
	Description("A header an auth client sends upstream. Exactly one of value and value_from_request_header is set, except that a header already storing a value may omit both to keep it, as long as is_secret is unchanged.")

	Attribute("name", String, "The header name.", func() { MinLength(1) })
	Attribute("description", String, "What the header is for.")
	Attribute("is_required", Boolean, "Whether a caller must supply the header when it is copied from the request.", func() { Default(false) })
	Attribute("is_secret", Boolean, "Whether the value is secret. Secret values are stored encrypted and never returned.", func() { Default(false) })
	Attribute("value", String, "The value to send. Omit it on a header that already stores one, with is_secret unchanged, to keep the stored value.")
	Attribute("value_from_request_header", String, "The caller's request header to copy the value from, for pass-through headers.")

	Required("name")
})

var AuthClientAudienceRule = Type("AuthClientAudienceRule", func() {
	Description("A rule to store for an auth client.")

	Attribute("principal_urn", String, "user:<id>, user:all, role:<id> or agent:<id>.")
	Attribute("level", String, "use or blocked.", audienceLevelEnum)

	Required("principal_urn", "level")
})

var _ = Service("authClients", func() {
	Description("Manage the auth clients Speakeasy uses to authenticate to upstream MCP servers, and which servers offer them. Managing a client never grants permission to use it. Stored header values are never returned. Changes are to typed auth configuration and take effect only where typed auth client resolution is enforced, which is being rolled out in stages. They revoke no upstream credentials and remove no existing upstream OAuth associations; elsewhere, including in shadow mode, existing upstream configuration keeps serving.")
	Security(security.Session)
	Security(security.ByKey, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("listAuthClients", func() {
		Description("List the auth clients in the caller's organization. Requires org:read.")

		Payload(func() {
			Attribute("mcp_server_id", String, "Only clients offered by this MCP server.", func() { Format(FormatUUID) })
			Attribute("client_type", String, "Only clients of this type.", clientTypeEnum)
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size.", func() {
				Default(50)
				Minimum(1)
				Maximum(100)
			})
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(func() {
			Attribute("items", ArrayOf(AuthClient))
			Attribute("next_cursor", String)
			Required("items")
		})

		HTTP(func() {
			GET("/rpc/authClients.list")
			Param("mcp_server_id")
			Param("client_type")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listAuthClients")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AuthClients"}`)
	})

	Method("getAuthClient", func() {
		Description("Get an auth client in the caller's organization. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The auth client id.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(AuthClient)

		HTTP(func() {
			GET("/rpc/authClients.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAuthClient")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AuthClient"}`)
	})

	Method("updateAuthClient", func() {
		Description("Rename, enable or disable an auth client. The change applies to the typed auth configuration of every MCP server that offers the client and takes effect only where typed auth client resolution is enforced. It revokes no upstream credentials; elsewhere, including in shadow mode, an MCP server's existing upstream configuration keeps serving. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The auth client id.", func() { Format(FormatUUID) })
			Attribute("name", String, "The new name.", func() { MinLength(1) })
			Attribute("enabled", Boolean, "Whether the client is enabled where typed auth client resolution is enforced.")
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(AuthClient)

		HTTP(func() {
			POST("/rpc/authClients.update")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateAuthClient")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateAuthClient"}`)
	})

	Method("createAuthClient", func() {
		Description("Create an auth client and, optionally, offer it on MCP servers. A remote_session or service_account client authenticates through an existing OAuth registration whose credential owner matches its type; a static_credential or pass_through client authenticates with its own headers. Creating a client gives nobody permission to use it: set its audience to decide who may. Takes effect only where typed auth client resolution is enforced. Requires org:admin.")

		Payload(func() {
			Attribute("client_type", String, "How the client authenticates upstream.", creatableClientTypeEnum)
			Attribute("name", String, "The client's name.", func() { MinLength(1) })
			Attribute("project_id", String, "Confine the client to one project. Omit to share it across the organization's projects.", func() { Format(FormatUUID) })
			Attribute("remote_session_client_id", String, "The OAuth registration behind a remote_session or service_account client.", func() { Format(FormatUUID) })
			Attribute("headers", ArrayOf(AuthClientHeaderInput), "The headers the client sends upstream. Required for static_credential and pass_through clients. Token-generating clients produce their own Authorization header, which cannot be configured.")
			Attribute("mcp_server_ids", ArrayOf(String, func() { Format(FormatUUID) }), "MCP servers that should offer the client.")
			Required("client_type", "name")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(AuthClient)

		HTTP(func() {
			POST("/rpc/authClients.create")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createAuthClient")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateAuthClient"}`)
	})

	Method("attachAuthClientToMcpServer", func() {
		Description("Offer an existing auth client on another MCP server instead of creating a copy. The client stays one client: changing or rotating it changes upstream behavior on every server that offers it. Offering a client grants nobody permission to use it or access to the server. Takes effect only where typed auth client resolution is enforced. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The auth client id.", func() { Format(FormatUUID) })
			Attribute("mcp_server_id", String, "The MCP server id.", func() { Format(FormatUUID) })
			Required("id", "mcp_server_id")
			security.SessionPayload()
			security.ByKeyPayload()
			// Named explicitly: detach takes the same body, and an unnamed copy
			// would be merged with it in the SDK.
			Meta("openapi:typename", "AttachAuthClientToMcpServerRequestBody")
		})

		Result(AuthClient)

		HTTP(func() {
			POST("/rpc/authClients.attachToMcpServer")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "attachAuthClientToMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "attachToMcpServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AttachAuthClientToMcpServer"}`)
	})

	Method("setAuthClientHeaders", func() {
		Description("Replace the headers an auth client sends upstream. Rotating a shared client's credential changes upstream behavior on every server that offers it. Token-generating clients produce their own Authorization header, which cannot be configured. Clients created from a server's existing settings follow those settings and are edited there. Takes effect only where typed auth client resolution is enforced. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The auth client id.", func() { Format(FormatUUID) })
			Attribute("headers", ArrayOf(AuthClientHeaderInput), "Every header the client should send.")
			Required("id", "headers")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(AuthClient)

		HTTP(func() {
			POST("/rpc/authClients.setHeaders")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setAuthClientHeaders")
		Meta("openapi:extension:x-speakeasy-name-override", "setHeaders")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetAuthClientHeaders"}`)
	})

	Method("detachAuthClientFromMcpServer", func() {
		Description("Stop an MCP server offering an auth client in its typed auth configuration. The client and its other servers are unchanged. Takes effect only where typed auth client resolution is enforced. It revokes no upstream credentials and does not remove the server's existing upstream OAuth association, which keeps serving elsewhere, including in shadow mode. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The auth client id.", func() { Format(FormatUUID) })
			Attribute("mcp_server_id", String, "The MCP server id.", func() { Format(FormatUUID) })
			Required("id", "mcp_server_id")
			security.SessionPayload()
			security.ByKeyPayload()
			// Named explicitly: attach takes the same body, and an unnamed copy
			// would be merged with it in the SDK.
			Meta("openapi:typename", "DetachAuthClientFromMcpServerRequestBody")
		})

		Result(AuthClient)

		HTTP(func() {
			POST("/rpc/authClients.detachFromMcpServer")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "detachAuthClientFromMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "detachFromMcpServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DetachAuthClientFromMcpServer"}`)
	})

	Method("getMcpServerAuthentication", func() {
		Description("Get how an MCP server authenticates to its upstream: whether authentication is required and which auth clients it offers. Requires org:read.")

		Payload(func() {
			Attribute("mcp_server_id", String, "The MCP server id.", func() { Format(FormatUUID) })
			Required("mcp_server_id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(McpServerAuthentication)

		HTTP(func() {
			GET("/rpc/authClients.getMcpServerAuthentication")
			Param("mcp_server_id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getMcpServerAuthentication")
		Meta("openapi:extension:x-speakeasy-name-override", "getMcpServerAuthentication")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpServerAuthentication"}`)
	})

	Method("setMcpServerUpstreamAuthRequirement", func() {
		Description("Declare whether an MCP server's upstream needs authentication. Where typed auth client resolution is enforced, a server that needs none is served without upstream credentials and a server that needs it and offers no enabled client is unavailable until setup is complete; elsewhere, including in shadow mode, the server's existing upstream configuration keeps serving. Not available for hosted toolsets. Requires org:admin.")

		Payload(func() {
			Attribute("mcp_server_id", String, "The MCP server id.", func() { Format(FormatUUID) })
			Attribute("requires_upstream_auth", Boolean, "Whether the upstream needs authentication.")
			Required("mcp_server_id", "requires_upstream_auth")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(McpServerAuthentication)

		HTTP(func() {
			POST("/rpc/authClients.setMcpServerUpstreamAuthRequirement")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setMcpServerUpstreamAuthRequirement")
		Meta("openapi:extension:x-speakeasy-name-override", "setMcpServerUpstreamAuthRequirement")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetMcpServerUpstreamAuthRequirement"}`)
	})
	Method("getAuthClientAudience", func() {
		Description("List who may use an auth client: the rules naming it and the broader rules that reach it. Requires org:read.")

		Payload(func() {
			Attribute("id", String, "The auth client id.", func() { Format(FormatUUID) })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(AuthClientAudience)

		HTTP(func() {
			GET("/rpc/authClients.getAudience")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAuthClientAudience")
		Meta("openapi:extension:x-speakeasy-name-override", "getAudience")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AuthClientAudience"}`)
	})

	Method("setAuthClientAudience", func() {
		Description("Replace the rules naming an auth client. Use grants mcp:auth on the client and Blocked writes mcp:blocked_auth; both apply on every server that offers the client. Server access is unchanged and still needs mcp:connect. Administering a client gives nobody permission to use it. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The auth client id.", func() { Format(FormatUUID) })
			Attribute("expected_version", String, "The version returned by getAudience.")
			Attribute("entries", ArrayOf(AuthClientAudienceRule), "Every rule that should name the client.")
			Required("id", "expected_version", "entries")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(AuthClientAudience)

		HTTP(func() {
			POST("/rpc/authClients.setAudience")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setAuthClientAudience")
		Meta("openapi:extension:x-speakeasy-name-override", "setAudience")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetAuthClientAudience"}`)
	})
})
