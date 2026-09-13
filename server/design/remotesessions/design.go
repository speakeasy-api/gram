package remotesessions

import (
	. "goa.design/goa/v3/dsl"

	remotesessionclients "github.com/speakeasy-api/gram/server/design/remotesessionclients"
	remotesessionissuers "github.com/speakeasy-api/gram/server/design/remotesessionissuers"
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("remoteSessions", func() {
	Description("Operator visibility into remote_sessions Gram is holding on a principal's behalf. Read + revoke; sessions are written by /mcp/{slug}/remote_login_callback and the silent-refresh path. access_token_encrypted and refresh_token_encrypted are never returned.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("commitServerUserIdentityConfiguration", func() {
		Description("Atomically configure user identity for a Remote MCP-backed MCP server. The complete plan selects or creates a project Remote Identity Provider and links an existing client, creates a manual client, or automatically prefers CIMD over DCR. Existing-client linking requires mcp:write on the target; creating a provider or client additionally requires project:write. Unsupported automatic registration returns manual_setup_required without changing local state.")

		Payload(func() {
			Extend(CommitServerUserIdentityConfigurationForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(CommitServerUserIdentityConfigurationResult)

		HTTP(func() {
			POST("/rpc/remoteSessions.commitServerUserIdentityConfiguration")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "commitServerUserIdentityConfiguration")
		Meta("openapi:extension:x-speakeasy-name-override", "commitServerUserIdentityConfiguration")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CommitServerUserIdentityConfiguration"}`)
	})

	Method("listRemoteSessions", func() {
		Description("List remote_sessions in the caller's project. access_token_encrypted and refresh_token_encrypted are never returned — only metadata (access_expires_at, refresh_expires_at, scopes).")

		Payload(func() {
			Attribute("subject_urn", String, "Exact-match filter on subject URN.")
			Attribute("remote_session_client_id", String, "Filter by remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListRemoteSessionsResult)

		HTTP(func() {
			GET("/rpc/remoteSessions.list")
			Param("subject_urn")
			Param("remote_session_client_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listRemoteSessions")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoteSessions"}`)
	})

	Method("revokeRemoteSession", func() {
		Description("Drop a remote_session row. The next /mcp call by that principal triggers a fresh authn challenge.")

		Payload(func() {
			Attribute("id", String, "The remote_session id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/remoteSessions.revoke")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeRemoteSession")
		Meta("openapi:extension:x-speakeasy-name-override", "revoke")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeRemoteSession"}`)
	})
})

var CommitServerUserIdentityConfigurationForm = Type("CommitServerUserIdentityConfigurationForm", func() {
	Description("A complete plan for configuring user identity on one Remote MCP-backed MCP server. Exactly one of provider_id or create_provider is required. client_mode controls which client fields are accepted: existing requires existing_client_id and no client_configuration; auto and manual require client_configuration and no existing_client_id.")

	Attribute("mcp_server_id", String, "The target MCP server. It must be backed directly by a Remote MCP source.", func() {
		Format(FormatUUID)
	})
	Attribute("provider_id", String, "An existing Remote Identity Provider visible to the target project.", func() {
		Format(FormatUUID)
	})
	Attribute("create_provider", remotesessionissuers.CreateRemoteSessionIssuerForm, "A new project-scoped Remote Identity Provider to create.")
	Attribute("client_mode", String, "How to provide the OAuth client.", func() {
		Enum("auto", "existing", "manual")
	})
	Attribute("existing_client_id", String, "An existing visible remote-session client to link. Required only for existing mode.", func() {
		Format(FormatUUID)
	})
	Attribute("client_configuration", ServerUserIdentityClientConfiguration, "Client settings for auto or manual mode. Forbidden for existing mode.")

	Required("mcp_server_id", "client_mode")
})

var ServerUserIdentityClientConfiguration = Type("ServerUserIdentityClientConfiguration", func() {
	Description("Configuration for a newly-created project remote-session client. Manual mode requires client_id. Auto mode forbids client_id and client_secret and uses scope, audience, and token_endpoint_auth_method as registration preferences.")

	Attribute("client_id", String, "The out-of-band OAuth client identifier. Manual mode only.")
	Attribute("client_secret", String, "The out-of-band OAuth client secret. Manual mode only; encrypted before persistence.")
	Attribute("token_endpoint_auth_method", String, "How the client authenticates at the provider token endpoint, or the preferred method for DCR.", func() {
		Enum("client_secret_basic", "client_secret_post", "none")
	})
	Attribute("scope", ArrayOf(String), "Explicit upstream OAuth scopes for this client.", func() {
		remotesessionclients.ScopeAttribute("Each value must be an RFC 6749 scope token.")
	})
	Attribute("audience", String, "Optional upstream OAuth audience.", remotesessionclients.AudienceAttribute)
})

var ServerUserIdentityRegistrationFailure = Type("ServerUserIdentityRegistrationFailure", func() {
	Description("A completed automatic registration failure using the bounded OAuth registration taxonomy.")

	Attribute("outcome", String, func() {
		Enum("unreachable", "refused")
	})
	Attribute("reason", String, func() {
		Enum("dns_error", "tls_error", "timeout", "network_error", "rate_limited", "upstream_unavailable", "authorization_rejected", "invalid_success_response")
	})
	Attribute("retryable", Boolean)
	Attribute("provider_message", String, "Optional sanitized and truncated provider-controlled message.")
	Attribute("http_status", Int, "Optional upstream HTTP status.")

	Required("outcome", "reason", "retryable")
})

var CommitServerUserIdentityConfigurationResult = Type("CommitServerUserIdentityConfigurationResult", func() {
	Description("The outcome of committing a Remote MCP server user-identity plan. registered and linked are successful local commits. A registration failure is returned in failure. manual_setup_required is a preparation signal, not a registration outcome, and means auto mode found neither usable CIMD nor DCR support; no local state changed.")

	Attribute("status", String, "Successful commit status. Present only after local commit.", func() {
		Enum("registered", "linked")
	})
	Attribute("registration_method", String, "How the client was obtained. Present on success and registration failure.", func() {
		Enum("cimd", "dcr", "manual", "existing")
	})
	Attribute("manual_setup_required", Boolean, "True only when auto mode found neither usable CIMD nor DCR support. This is not a registration failure and no local state was changed.")
	Attribute("provider", remotesessionissuers.RemoteSessionIssuer, "The selected or created provider on success, or the selected existing provider when manual setup is required or DCR fails.")
	Attribute("client", remotesessionclients.RemoteSessionClient, "The created or linked client on success. Never includes a client secret.")
	Attribute("provider_tier", String, "The provider ownership tier.", func() {
		Enum("project-specific", "organization-level", "platform-level")
	})
	Attribute("client_tier", String, "The client ownership tier.", func() {
		Enum("project-specific", "organization-level")
	})
	Attribute("provider_path", String, "Organization-relative dashboard path to the Remote Identity Provider detail surface.")
	Attribute("client_path", String, "Organization-relative dashboard path to the client detail surface.")
	Attribute("failure", ServerUserIdentityRegistrationFailure, "Completed DCR failure. Mutually exclusive with status and manual_setup_required=true.")

	Required("manual_setup_required")
})

// organizationRemoteSessions manages remote_sessions from the
// organization-administrator surface (AIS-119). Unlike the project-scoped
// remoteSessions service above, these methods span every project in the
// caller's organization, scoped exclusively by organization_id. Security is
// organization-scoped (no ProjectSlug); RBAC gates writes on org:admin and
// reads on org:read.
var _ = Service("organizationRemoteSessions", func() {
	Description("Organization-administrator visibility into remote_sessions Gram is holding on a principal's behalf, across every project in the caller's organization. access_token_encrypted and refresh_token_encrypted are never returned.")
	Security(security.Session)
	Security(security.ByKey, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("listClientSessions", func() {
		Description("List the remote_sessions minted against a remote_session_client in the caller's organization. access_token_encrypted and refresh_token_encrypted are never returned. Requires org:read.")

		Payload(func() {
			Attribute("client_id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("client_id")
			Attribute("cursor", String, "Pagination cursor.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(ListOrganizationRemoteSessionsResult)

		HTTP(func() {
			GET("/rpc/organizationRemoteSessions.list")
			Param("client_id")
			Param("cursor")
			Param("limit")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listOrganizationRemoteSessionClientSessions")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "OrganizationRemoteSessionClientSessions"}`)
	})

	Method("revokeSession", func() {
		Description("Revoke (soft-delete) a single remote_session in the caller's organization. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		HTTP(func() {
			POST("/rpc/organizationRemoteSessions.revoke")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeOrganizationRemoteSession")
		Meta("openapi:extension:x-speakeasy-name-override", "revoke")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeOrganizationRemoteSession"}`)
	})

	Method("refreshSession", func() {
		Description("Force an upstream token refresh on a single remote_session in the caller's organization, regardless of current access-token expiry. Returns the updated remote_session so callers can reflect the new expiry without a refetch. Fails with a bad-request error when the session holds no refresh token. Requires org:admin.")

		Payload(func() {
			Attribute("id", String, "The remote_session id.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RemoteSession)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessions.refresh")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "refreshOrganizationRemoteSession")
		Meta("openapi:extension:x-speakeasy-name-override", "refresh")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RefreshOrganizationRemoteSession"}`)
	})

	Method("revokeAllClientSessions", func() {
		Description("Revoke (soft-delete) all remote_sessions minted against a remote_session_client in the caller's organization. Requires org:admin.")

		Payload(func() {
			Attribute("client_id", String, "The remote_session_client id.", func() {
				Format(FormatUUID)
			})
			Required("client_id")
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(RevokeAllRemoteSessionsResult)

		HTTP(func() {
			POST("/rpc/organizationRemoteSessions.revokeAll")
			Param("client_id")
			security.SessionHeader()
			security.ByKeyHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "revokeAllOrganizationRemoteSessionClientSessions")
		Meta("openapi:extension:x-speakeasy-name-override", "revokeAll")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RevokeAllOrganizationRemoteSessionClientSessions"}`)
	})
})

var RemoteSession = Type("RemoteSession", func() {
	Meta("struct:pkg:path", "types")

	Description("A remote_session record — Gram's upstream OAuth session for a (principal, remote_session_client) pair. access_token_encrypted and refresh_token_encrypted are never returned.")

	Attribute("id", String, "The remote_session id.", func() {
		Format(FormatUUID)
	})
	Attribute("subject_urn", String, "The session's subject URN (user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>).")
	Attribute("subject_display_name", String, "Resolved display name when the subject is a Gram user. Absent for apikey/anonymous subjects or unresolved users.")
	Attribute("subject_email", String, "Resolved email when the subject is a Gram user. Absent for apikey/anonymous subjects or unresolved users.")
	Attribute("user_session_issuer_id", String, "The user_session_issuer this session is bound to.", func() {
		Format(FormatUUID)
	})
	Attribute("remote_session_client_id", String, "The remote_session_client this session was minted against.", func() {
		Format(FormatUUID)
	})
	Attribute("access_expires_at", String, "Upstream access-token expiry. Independent of refresh_expires_at.", func() {
		Format(FormatDateTime)
	})
	Attribute("refresh_expires_at", String, "Upstream refresh-token expiry. Null when the session has no refresh token.", func() {
		Format(FormatDateTime)
	})
	Attribute("has_refresh_token", Boolean, "Whether the session holds an upstream refresh token. Gates the 'Refresh now' action; refresh_expires_at is insufficient because an upstream may issue a non-expiring refresh token. The token itself is never returned.")
	Attribute("scopes", ArrayOf(String), "Scopes held by this session.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})

	Required("id", "subject_urn", "user_session_issuer_id", "remote_session_client_id", "access_expires_at", "has_refresh_token", "scopes", "created_at", "updated_at")
})

var ListRemoteSessionsResult = Type("ListRemoteSessionsResult", func() {
	Description("Result type for listing remote_sessions.")

	Attribute("items", ArrayOf(RemoteSession))
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")

	Required("items")
})
