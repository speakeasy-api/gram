package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

func attachmentScopeEnum() {
	Enum("project", "organization", "global")
}

var AdminMcpServerHealthServer = Type("AdminMcpServerHealthServer", func() {
	Description("The server the health report describes, as listProjectMcpServers lists it.")
	Required("id", "name", "source", "visibility", "created_at")

	Attribute("id", String, "The mcp_servers row ID, or the toolset ID for a toolset-only server.")
	Attribute("name", String, "Display name of the server.")
	Attribute("source", String, "What backs the server. toolset_only is a toolset with no mcp_servers row.", func() {
		Enum("toolset", "remote", "tunneled", "unproxied", "toolset_only")
	})
	Attribute("visibility", String, "The visibility of the server.", func() {
		Enum("disabled", "private", "public")
	})
	Attribute("created_at", String, func() { Format(FormatDateTime) })
})

var AdminMcpServerHealthCorrelation = Type("AdminMcpServerHealthCorrelation", func() {
	Description("The identities telemetry is matched on for this server.")
	Attribute("url_slug", String, "The slug in the server's /mcp/<slug> URL, matched against hook-observed calls. Absent when the server has no slug.")
	Attribute("mcp_server_id", String, "The mcp_servers row ID stamped on proxied calls. Absent for toolset-only servers.")
	Attribute("toolset_slug", String, "The toolset slug stamped on hosted calls. Absent when the server has no toolset or several live servers share it.")
})

var AdminMcpServerHealthTrustedRemoteSession = Type("AdminMcpServerHealthTrustedRemoteSession", func() {
	Description("The external authorization server whose assertions the issuer trusts.")
	Required("issuer_id", "client_id")

	Attribute("issuer_id", String, "The trusted remote session issuer ID.")
	Attribute("client_id", String, "The remote session client ID Gram uses with the trusted issuer.")
})

var AdminMcpServerHealthServerRef = Type("AdminMcpServerHealthServerRef", func() {
	Description("Another server in the project that shares the issuer.")
	Required("id", "name")

	Attribute("id", String, "The mcp_servers row ID, or the toolset ID for a toolset-only server.")
	Attribute("name", String, "Display name of the server.")
})

var AdminMcpServerHealthUserSessions = Type("AdminMcpServerHealthUserSessions", func() {
	Description("User sessions the issuer has minted. A refresh replaces a session row, so last_issued_at includes refreshes. For an organization- or global-attached issuer the counts span every project using the issuer.")
	Required("distinct_subjects_ever", "distinct_subjects_in_window", "live")

	Attribute("distinct_subjects_ever", Int64, "Distinct users that ever received a session from the issuer.")
	Attribute("distinct_subjects_in_window", Int64, "Distinct users that received a session inside the window.")
	Attribute("first_issued_at", String, "When the first session was issued.", func() { Format(FormatDateTime) })
	Attribute("last_issued_at", String, "When the latest session was issued or refreshed.", func() { Format(FormatDateTime) })
	Attribute("live", Int64, "Sessions whose refresh deadline has not passed.")
})

var AdminMcpServerHealthRemoteSessionIssuer = Type("AdminMcpServerHealthRemoteSessionIssuer", func() {
	Description("The upstream authorization server a remote session client is registered with. Error columns are exposed as timestamps only.")
	Required("id", "slug", "issuer", "attachment_scope", "networking", "oidc", "passthrough", "pkce", "cimd_supported")

	Attribute("id", String, "The remote session issuer ID.")
	Attribute("slug", String, "The issuer slug.")
	Attribute("name", String, "Display name of the issuer.")
	Attribute("issuer", String, "The upstream issuer URL.")
	Attribute("attachment_scope", String, "Where the row is attached. global is platform-wide.", attachmentScopeEnum)
	Attribute("networking", String, "Whether Gram reaches the issuer over the public internet or a tunnel.", func() {
		Enum("public", "tunneled")
	})
	Attribute("oidc", Boolean, "Whether the issuer is treated as an OpenID Connect provider.")
	Attribute("passthrough", Boolean, "Whether upstream tokens are passed through to the server.")
	Attribute("pkce", String, "PKCE support classified from the issuer's advertised code challenge methods.", func() {
		Enum("supported", "unsupported", "none", "uncaptured")
	})
	Attribute("cimd_supported", Boolean, "Whether the issuer accepts a Client ID Metadata Document URL as client_id.")
	Attribute("scope_override", ArrayOf(String), "Operator-pinned scopes sent in place of the discovered set. Absent when unset.")
	Attribute("omit_scope_fallback", Boolean, "Whether a login with no other scope source sends no scope instead of the issuer's whole scopes_supported. Absent when unset, which behaves as false.")
	Attribute("metadata_fetched_at", String, "Last successful metadata discovery.", func() { Format(FormatDateTime) })
	Attribute("metadata_last_error_at", String, "Last failed metadata discovery.", func() { Format(FormatDateTime) })
	Attribute("jwks_last_error_at", String, "Last failed JWK Set fetch.", func() { Format(FormatDateTime) })
})

var AdminMcpServerHealthRemoteSessions = Type("AdminMcpServerHealthRemoteSessions", func() {
	Description("Upstream sessions brokered through one remote session client, whichever issuer first linked them.")
	Required("linked_subjects", "reauthorizations", "validation_status_counts")

	Attribute("linked_subjects", Int64, "Distinct users holding a live upstream session.")
	Attribute("reauthorizations", Int64, "Fresh authorizations beyond each session's first.")
	Attribute("first_linked_at", String, "When the first upstream session was linked.", func() { Format(FormatDateTime) })
	Attribute("validation_status_counts", MapOf(String, Int64), "Live sessions per last validation status: valid, rejected_by_member, inactive or unknown. Sessions never validated are left out.")
})

var AdminMcpServerHealthRemoteSessionClient = Type("AdminMcpServerHealthRemoteSessionClient", func() {
	Description("A remote session client attached to the issuer, with its upstream issuer and session counts. Never carries secrets.")
	Required("id", "registration", "scope", "grant_types", "has_identity_provider_connection", "attachment_scope", "issuer", "sessions")

	Attribute("id", String, "The remote session client ID.")
	Attribute("registration", String, "How the client was registered upstream.", func() {
		Enum("cimd", "dcr", "static")
	})
	Attribute("token_endpoint_auth_method", String, "The client's token endpoint auth method. Absent when unset.", func() {
		Enum("client_secret_basic", "client_secret_post", "none", "private_key_jwt")
	})
	Attribute("scope", ArrayOf(String), "Scopes recorded for the client.")
	Attribute("grant_types", ArrayOf(String), "Grant types recorded for the client.")
	Attribute("has_identity_provider_connection", Boolean, "Whether the client is backed by an identity provider connection.")
	Attribute("attachment_scope", String, "Where the row is attached. global is platform-wide.", attachmentScopeEnum)
	Attribute("upstream_rejected_at", String, "When the upstream last rejected the client's credentials.", func() { Format(FormatDateTime) })
	Attribute("issuer", AdminMcpServerHealthRemoteSessionIssuer)
	Attribute("sessions", AdminMcpServerHealthRemoteSessions)
})

var AdminMcpServerHealthUserSessionIssuer = Type("AdminMcpServerHealthUserSessionIssuer", func() {
	Description("The user session issuer that authenticates the server's users.")
	Required("id", "slug", "classification", "authn_challenge_mode", "session_duration_hours", "attachment_scope", "use_authentication_host", "other_servers_using_issuer", "created_at", "sessions", "remote_session_clients")

	Attribute("id", String, "The user session issuer ID.")
	Attribute("slug", String, "The issuer slug.")
	Attribute("classification", String, "custom, or project_default_idp for the auto-provisioned issuer of private servers.", func() {
		Enum("custom", "project_default_idp")
	})
	Attribute("authn_challenge_mode", String, "How multi-remote authn challenges are presented.", func() {
		Enum("chain", "interactive")
	})
	Attribute("session_duration_hours", Int64, "How long a user session lasts, in whole hours.")
	Attribute("attachment_scope", String, "Where the row is attached. global is platform-wide.", attachmentScopeEnum)
	Attribute("client_id_metadata_admission_mode", String, "The stored CIMD admission mode. Absent when unset, which admits as open.", func() {
		Enum("disabled", "presets", "reporting", "open")
	})
	Attribute("use_authentication_host", Boolean, "Whether the issuer announces the authentication host as its origin.")
	Attribute("trusted_remote_session", AdminMcpServerHealthTrustedRemoteSession, "Set when the issuer trusts an external authorization server's assertions.")
	Attribute("other_servers_using_issuer", ArrayOf(AdminMcpServerHealthServerRef), "Other live servers in the project that share this issuer.")
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("sessions", AdminMcpServerHealthUserSessions)
	Attribute("remote_session_clients", ArrayOf(AdminMcpServerHealthRemoteSessionClient), "Remote session clients attached to the issuer.")
})

var AdminMcpServerToolCallOutcomes = Type("AdminMcpServerToolCallOutcomes", func() {
	Description("Tool calls inside the window by outcome class. In-band tool errors (isError inside HTTP 200) count as success.")
	Required("success", "unauthorized", "client_error", "server_error", "blocked", "failed", "unknown")

	Attribute("success", Int64)
	Attribute("unauthorized", Int64)
	Attribute("client_error", Int64)
	Attribute("server_error", Int64)
	Attribute("blocked", Int64)
	Attribute("failed", Int64)
	Attribute("unknown", Int64)
})

var AdminMcpServerToolCallBucket = Type("AdminMcpServerToolCallBucket", func() {
	Description("Tool calls in one bucket of the series.")
	Required("bucket_start", "total", "failed")

	Attribute("bucket_start", String, func() { Format(FormatDateTime) })
	Attribute("total", Int64, "Tool calls in the bucket.")
	Attribute("failed", Int64, "Failed tool calls in the bucket.")
})

var AdminMcpServerToolCalls = Type("AdminMcpServerToolCalls", func() {
	Description("One MCP server's tool calls over a window, discriminated on type. logging:disabled carries nothing else: the organization's logs are off, so calls were never recorded. logging:enabled carries every other field. Outcomes also count hook-observed calls; the daily series counts only calls that reached Gram directly.")
	Required("type")

	Attribute("type", String, func() {
		Enum("logging:disabled", "logging:enabled")
	})
	Attribute("window_days", Int, "Length of the window in days.")
	Attribute("watermark", String, "Telemetry is complete up to this time.", func() { Format(FormatDateTime) })
	Attribute("outcomes", AdminMcpServerToolCallOutcomes)
	Attribute("bucket_seconds", Int64, "Width of each series bucket.")
	Attribute("daily", ArrayOf(AdminMcpServerToolCallBucket), "Tool calls per bucket, oldest first.")
})

var AdminMcpServerResourceScopeClient = Type("AdminMcpServerResourceScopeClient", func() {
	Description("What a login through one remote session client bound to the server would request now.")
	Required("client_id", "scope_source", "requested_scopes", "unadvertised_pinned_scopes", "pin_would_decide")

	Attribute("client_id", String, "The remote session client ID.")
	Attribute("scope_source", String, "Which source decides the client's requested scopes.", func() {
		Enum("client_scope", "challenge_scope", "resource_pin", "live_resource", "cached_resource", "issuer_override", "issuer_omitted", "issuer_catalogue", "none")
	})
	Attribute("requested_scopes", ArrayOf(String), "The scopes a login would request.")
	Attribute("unadvertised_pinned_scopes", ArrayOf(String), "Pinned scopes the MCP server does not advertise. They are still requested.")
	Attribute("pin_would_decide", Boolean, "Whether a pin, if set, decides this client's request: the client owns the resource and neither its own scopes nor a challenge outranks the pin.")
})

var AdminMcpServerResourceScopes = Type("AdminMcpServerResourceScopes", func() {
	Description("The scopes logins through a remote-backed MCP server request, read from the cached protected resource without probing. Resolved as if the organization had the remote-session-live-resource-scopes rollout on: this view does not evaluate the flag, and with it off logins ignore the pin and the resource's scopes.")
	Required("resource_url", "pinned_scopes", "advertised_scopes_known", "challenge_scopes", "shared_server_count", "clients")

	Attribute("resource_url", String, "The upstream URL the protected resource is keyed by.")
	Attribute("pinned_scopes", ArrayOf(String), "Scopes pinned on the resource. Empty when there is no pin.")
	Attribute("advertised_scopes_known", Boolean, "Whether the resource's advertised scopes are known from a fresh RFC 9728 read.")
	Attribute("advertised_scopes", ArrayOf(String), "The RFC 9728 scopes_supported the resource advertises. Absent when unknown.")
	Attribute("challenge_scopes", ArrayOf(String), "Scopes named by the resource's last WWW-Authenticate challenge.")
	Attribute("shared_server_count", Int, "Other live MCP servers in the project with the same upstream URL. A pin applies to all of them.")
	Attribute("clients", ArrayOf(AdminMcpServerResourceScopeClient), "Remote session clients bound to the server's user session issuer.")
})

var AdminMcpServerHealth = Type("AdminMcpServerHealth", func() {
	Description("Health of one MCP server: its authentication configuration and session counts. Tool calls are read separately through getMcpServerToolCalls. Never carries secrets, error text, subjects, users or emails.")
	Required("server", "correlation")

	Attribute("server", AdminMcpServerHealthServer)
	Attribute("correlation", AdminMcpServerHealthCorrelation)
	Attribute("legacy_auth", String, "Legacy authentication in force. Set only when the server has no user session issuer.", func() {
		Enum("external_oauth", "oauth_proxy", "gram_private")
	})
	Attribute("user_session_issuer", AdminMcpServerHealthUserSessionIssuer)
	Attribute("resource_scopes", AdminMcpServerResourceScopes, "Set only when a remote MCP server backs the server.")
})

// MCP parity: both methods are exposed through Staff Admin MCP (S-1121), the
// audience they serve. Deliberately not a Platform MCP
// tool: that surface serves an organization's own administrators, who already
// have get_mcp_diagnostics for the same outcomes, and this read spans
// issuer, session and client configuration only staff may see.
func mcpServerHealthMethods() {
	Method("describeMcpServerHealth", func() {
		Description("Describes one MCP server's health: authentication configuration and session counts (admin view, no auth scoping). Tool calls come from getMcpServerToolCalls.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id", "project_id", "mcp_server_id")

			Attribute("organization_id", String, "Organization the project must belong to. A project outside it is reported as not found.")
			Attribute("project_id", String, "Project ID.", func() { Format(FormatUUID) })
			Attribute("mcp_server_id", String, "The server id from listProjectMcpServers; the toolset ID for toolset-only servers.", func() { Format(FormatUUID) })
			Attribute("window_days", Int, "Window in days for distinct_subjects_in_window.", func() {
				Enum(14, 30, 90)
				Default(14)
			})
		})

		Result(AdminMcpServerHealth)

		HTTP(func() {
			GET("/admin/project.mcpServerHealth")

			Param("organization_id")
			Param("project_id")
			Param("mcp_server_id")
			Param("window_days")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminDescribeMcpServerHealth")
		Meta("openapi:extension:x-speakeasy-name-override", "describeMcpServerHealth")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminDescribeMcpServerHealth"}`)
	})

	// MCP parity: describe_mcp_server_health returns resource_scopes. Writing a
	// pin stays dashboard-only: an Admin MCP write needs the proposal and
	// approval flow, and the flag gating whether logins honor a pin is still
	// rolling out.
	Method("setMcpServerScopePin", func() {
		Description("Sets or clears the scopes pinned on the protected resource behind a remote-backed MCP server (admin view, no auth scoping). The pin applies to every live server in the project with the same upstream URL. Audited as the staff member.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id", "project_id", "mcp_server_id", "scopes")

			Attribute("organization_id", String, "Organization the project must belong to. A project outside it is reported as not found.")
			Attribute("project_id", String, "Project ID.", func() { Format(FormatUUID) })
			Attribute("mcp_server_id", String, "The mcp_servers row ID.", func() { Format(FormatUUID) })
			Attribute("scopes", ArrayOf(String), "The new pin. Trimmed and de-duplicated; an empty list clears the pin.")
		})

		Result(AdminMcpServerResourceScopes)

		HTTP(func() {
			POST("/admin/project.setMcpServerScopePin")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminSetMcpServerScopePin")
		Meta("openapi:extension:x-speakeasy-name-override", "setMcpServerScopePin")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminSetMcpServerScopePin"}`)
	})

	Method("getMcpServerToolCalls", func() {
		Description("Reads one MCP server's tool call outcomes and series over a window (admin view, no auth scoping). Returns logging:disabled without reading telemetry when the organization's logs are off.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id", "project_id", "mcp_server_id")

			Attribute("organization_id", String, "Organization the project must belong to. A project outside it is reported as not found.")
			Attribute("project_id", String, "Project ID.", func() { Format(FormatUUID) })
			Attribute("mcp_server_id", String, "The server id from listProjectMcpServers; the toolset ID for toolset-only servers.", func() { Format(FormatUUID) })
			Attribute("window_days", Int, "Window in days. 90 days is bucketed weekly, shorter windows daily.", func() {
				Enum(14, 30, 90)
				Default(14)
			})
		})

		Result(AdminMcpServerToolCalls)

		HTTP(func() {
			GET("/admin/project.mcpServerToolCalls")

			Param("organization_id")
			Param("project_id")
			Param("mcp_server_id")
			Param("window_days")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetMcpServerToolCalls")
		Meta("openapi:extension:x-speakeasy-name-override", "getMcpServerToolCalls")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminGetMcpServerToolCalls"}`)
	})
}
