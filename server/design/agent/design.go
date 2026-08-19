package agent

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// --- Service ---

var _ = Service("agent", func() {
	Description("Endpoints consumed by the Speakeasy device agent running on developer machines. Authenticates via an API key carrying the 'agent_user' scope — the per-user credential minted by token-exchange. An org key with the broader 'agent' scope also satisfies most of these endpoints (it implies 'agent_user'), so existing installs keep working during the transition. The content-bearing session-portability endpoints — getSessionMeta and createSessionHandoff — refuse it and require a per-user key.")
	Security(security.ByKey, func() {
		Scope("agent_user")
	})
	shared.DeclareErrorResponses()

	Method("getPlugins", func() {
		Description("Resolve the marketplaces, plugins, and optional organization configuration assigned to the enrolled user. The device agent reconciles these into the AI developer tools it manages. Organization configuration is delivered on this existing poll so agents do not need a second control-plane request.")

		// Authenticated with an API key carrying the `agent_user` scope — the
		// per-user key minted by token-exchange, so the enrolled user is the key
		// owner and the org derives from the key. An org `agent` install key also
		// passes (it implies `agent_user`; see auth.Authorize); the MDM profile
		// then vouches the enrolled user's email via the Gram-User-Email header.
		Security(security.ByKey, func() {
			Scope("agent_user")
		})

		Payload(func() {
			security.ByKeyPayload()
			// Authoritative when authenticating with an org install key (`agent`
			// scope): that key's owner is an admin, not the enrolled developer, so
			// the MDM profile vouches the developer's email here. Ignored for a
			// per-user key (`agent_user`), whose owner is the enrolled user.
			Attribute("email", String, "Email address of the enrolled user, sent in the Gram-User-Email header. Required when authenticating with an org-scoped agent install key (the MDM zero-touch path); ignored for a per-user key, whose owner is the enrolled user.", func() {
				Example("dev@acme.corp")
			})
			// Deployed org-key agents predate the header and vouch via `?email=`;
			// an updated server must keep accepting it or their polls 400 until
			// the device updates — which auto-update settings can defer
			// indefinitely. Remove once org-key polls no longer carry it.
			Attribute("legacy_email", String, "Deprecated: the vouched email as the `?email=` query parameter, sent by agents predating the Gram-User-Email header. Used only when the header is absent.", func() {
				Example("dev@acme.corp")
			})
			// Hardware identity, reported by agents that can read it. Both are
			// optional and MUST stay that way: agents predating the capability
			// omit them entirely, and Goa rejects a request missing a Required
			// attribute — marking either one Required would break every older
			// agent's poll. An agent that cannot read a serial (common on
			// white-box PCs, which report blank or a placeholder) also sends
			// nothing, and coverage falls back to the per-user email match.
			Attribute("serial_number", String, "Hardware serial number of the machine the agent runs on, when it can be read. Lets device coverage attest this specific machine rather than its assigned user.", func() {
				Example("C02XK1ABCDEF")
			})
			Attribute("hostname", String, "Hostname of the machine the agent runs on, when it can be read.", func() {
				Example("dev-macbook-pro")
			})
		})

		Result(GetPluginsResult)

		HTTP(func() {
			GET("/rpc/agent.getPlugins")
			security.ByKeyHeader()
			// Headers rather than query params: all three identify a person or
			// a machine, and the request logger records URLs but not headers.
			// The deprecated `?email=` fallback stays redacted by the logging
			// middleware.
			Header("email:Gram-User-Email")
			Param("legacy_email:email")
			Header("serial_number:Gram-Device-Serial")
			Header("hostname:Gram-Device-Hostname")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAgentPlugins")
		Meta("openapi:extension:x-speakeasy-name-override", "getPlugins")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentPlugins"}`)
	})

	Method("listSyncedUsers", func() {
		Description("List users in the current organization who are actively running the Speakeasy device agent, attributed by the email each agent reports on sync. Dashboard-only; requires an org admin session.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListSyncedUsersResult)

		HTTP(func() {
			GET("/rpc/agent.listSyncedUsers")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listSyncedAgentUsers")
		Meta("openapi:extension:x-speakeasy-name-override", "listSyncedUsers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SyncedAgentUsers"}`)
	})

	Method("getConfiguration", func() {
		Description("Get the organization-wide device-agent configuration for the dashboard. Requires a session with the org:admin scope. An unconfigured organization returns an empty document with is_configured=false; enrolled agents do not receive a remote layer until an administrator saves one.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
		})

		Result(DeviceAgentConfigurationModel)

		HTTP(func() {
			GET("/rpc/agent.getConfiguration")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getDeviceAgentConfiguration")
		Meta("openapi:extension:x-speakeasy-name-override", "getConfiguration")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeviceAgentConfiguration"}`)
	})

	Method("updateConfiguration", func() {
		Description("Create or replace the organization-wide, non-secret device-agent configuration. Requires a session with the org:admin scope. Known settings are replaced wholesale — omitting one removes it — while stored keys this server does not recognize are preserved for forward compatibility; identity and credential keys are rejected.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Attribute("config", MapOf(String, Any), "Shareable device-agent settings. Supported keys include platforms, update_channel, auto_update, pinned_target, blocked_versions, and sync_interval_seconds. update_channel and blocked_versions can only be set by Speakeasy platform administrators; per-device identity and secret keys are forbidden.")
			Required("config")
		})

		Result(DeviceAgentConfigurationModel)

		HTTP(func() {
			POST("/rpc/agent.updateConfiguration")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateDeviceAgentConfiguration")
		Meta("openapi:extension:x-speakeasy-name-override", "updateConfiguration")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateDeviceAgentConfiguration"}`)
	})

	Method("getSessionMeta", func() {
		Description("Resolve display metadata (Gram chat id, generated title, last activity) for captured agent sessions the calling user owns. Used by the device agent's session picker to overlay server-generated titles on locally discovered transcripts; unknown or non-owned session ids are silently omitted, so the picker degrades gracefully. Requires a per-user key: the fleet-shared org install key is refused because session metadata is per-user data.")

		// Deliberately NOT reachable with the org install key (`agent` scope):
		// unlike getPlugins, this returns per-user chat data, and the org key +
		// vouched-email pattern would let any key holder enumerate any
		// employee's session titles (the DNO-383 blast-radius concern). The
		// handler enforces the refusal; the scope here only sets the floor.
		Security(security.ByKey, func() {
			Scope("agent_user")
		})

		Payload(func() {
			security.ByKeyPayload()
			Attribute("session_ids", ArrayOf(String), "Native harness session identifiers (e.g. Claude Code session UUIDs, Codex rollout ids) to resolve. Gram derives its chat ids from these the same way hook ingest does.", func() {
				MaxLength(50)
			})
			Required("session_ids")
		})

		Result(GetSessionMetaResult)

		HTTP(func() {
			GET("/rpc/agent.getSessionMeta")
			security.ByKeyHeader()
			Param("session_ids")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAgentSessionMeta")
		Meta("openapi:extension:x-speakeasy-name-override", "getSessionMeta")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentSessionMeta"}`)
	})

	Method("reportSessionMoved", func() {
		Description("Record that a captured agent session was moved to another harness on a device (session portability). Carries no session content — only the session identity, the target harness, and device attribution — and lands as a chat_session:move audit event so organizations retain governance visibility over local-first moves. Accepts both the per-user key and the org install key (with a vouched email), mirroring getPlugins, because fleet devices must be able to report moves. Fire-and-forget from the agent's perspective: the daemon must never fail a move because this call failed.")

		Security(security.ByKey, func() {
			Scope("agent_user")
		})

		Payload(func() {
			security.ByKeyPayload()
			Attribute("session_id", String, "Native harness session identifier of the moved session. Gram derives its chat id from this the same way hook ingest does; the move is recorded even if the session has not been captured yet.", func() {
				MaxLength(256)
			})
			Attribute("target_harness", String, "Harness the session was moved to (e.g. cursor, codex, claude-code).", func() {
				MaxLength(64)
			})
			Attribute("source_surface", String, "Harness the session originated in, as detected by the agent (e.g. claude-code, codex).", func() {
				MaxLength(64)
			})
			Attribute("email", String, "Email of the enrolled user. Authoritative when authenticating with an org-scoped agent install key (the MDM zero-touch path); ignored for a per-user key, whose owner is the enrolled user.")
			Attribute("serial_number", String, "Hardware serial number of the machine the move happened on, when the agent can read it.")
			Attribute("hostname", String, "Hostname of the machine the move happened on, when the agent can read it.")
			Required("session_id", "target_harness")
		})

		HTTP(func() {
			POST("/rpc/agent.reportSessionMoved")
			security.ByKeyHeader()
			// Device identity rides in headers, not the body, for the same
			// access-log hygiene reason getPlugins uses headers for them.
			Header("serial_number:Gram-Device-Serial")
			Header("hostname:Gram-Device-Hostname")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "reportAgentSessionMoved")
		Meta("openapi:extension:x-speakeasy-name-override", "reportSessionMoved")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ReportAgentSessionMoved"}`)
	})

	Method("createSessionHandoff", func() {
		Description("Mint a short-lived capability URL for a rendered session-handoff document (session portability). The device agent uploads the handoff it rendered from the local transcript; the returned URL serves the markdown exactly once (burn-after-read) until expiry, so a cloud agent or another machine can continue the session. Content transits the server only for this purpose and stops being served at first read or expiry, whichever comes first. Requires a per-user key: the fleet-shared org install key is refused because minting a fetch-by-token URL for uploaded content is a per-user, content-bearing surface (the same DNO-383 blast-radius rule as getSessionMeta).")

		// Content-bearing, so the refusal posture is getSessionMeta's, not
		// reportSessionMoved's: an org install key plus vouched email must not
		// be able to publish content in an arbitrary employee's name.
		Security(security.ByKey, func() {
			Scope("agent_user")
		})

		Payload(func() {
			security.ByKeyPayload()
			Attribute("session_id", String, "Native harness session identifier the handoff was rendered from. Gram derives its chat id from this the same way hook ingest does; a not-yet-captured session can still mint a link.", func() {
				MaxLength(256)
			})
			Attribute("content", String, "The rendered handoff document (markdown). Size-capped; the daemon renders deterministically from the local transcript.", func() {
				MaxLength(262144)
			})
			Attribute("source_surface", String, "Harness the session originated in, as detected by the agent (e.g. claude-code, codex).", func() {
				MaxLength(64)
			})
			Attribute("ttl_seconds", Int, "Requested link lifetime in seconds. Clamped to [60, 3600]; defaults to 900 when omitted.", func() {
				Example(900)
			})
			Attribute("serial_number", String, "Hardware serial number of the machine minting the link, when the agent can read it.")
			Attribute("hostname", String, "Hostname of the machine minting the link, when the agent can read it.")
			Required("session_id", "content")
		})

		Result(CreateSessionHandoffResult)

		HTTP(func() {
			POST("/rpc/agent.createSessionHandoff")
			security.ByKeyHeader()
			// Device identity rides in headers for the access-log hygiene
			// reason getPlugins documents.
			Header("serial_number:Gram-Device-Serial")
			Header("hostname:Gram-Device-Hostname")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createAgentSessionHandoff")
		Meta("openapi:extension:x-speakeasy-name-override", "createSessionHandoff")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateAgentSessionHandoff"}`)
	})
})

// --- Types ---

var GetPluginsResult = Type("GetPluginsResult", func() {
	Required("etag", "marketplaces", "plugins")
	Attribute("etag", String, "Opaque revision identifier covering the marketplace, plugin, and remote-configuration set. The agent stores this to detect changes between polls.")
	Attribute("marketplaces", ArrayOf(AgentMarketplaceModel), "Plugin marketplaces the agent should register with the tools it manages. Sorted by name.")
	Attribute("plugins", ArrayOf(AgentPluginModel), "Plugins the agent should enable. Each entry references one of the marketplaces above by name.")
	Attribute("configuration", DeviceAgentConfigurationModel, "Organization-wide remote configuration. Absent until an administrator saves a configuration, allowing an agent with no cached remote layer to keep using its local configuration.")
})

var DeviceAgentConfigurationModel = Type("DeviceAgentConfiguration", func() {
	Description("Versioned organization-wide settings delivered to enrolled device agents. Agents must ignore unknown config keys. Remote settings override the shareable local/MDM settings after a successful fetch; per-device identity and secrets always remain local.")
	Required("schema_version", "config", "is_configured", "etag")
	Attribute("schema_version", Int, "Schema version for this remote configuration envelope.", func() {
		Minimum(1)
	})
	Attribute("config", MapOf(String, Any), "Forward-compatible non-secret settings document. Platform values use false, user, or managed enforcement layers.")
	Attribute("is_configured", Boolean, "Whether an administrator has saved a remote configuration. False means agents should not add a remote resolver layer.")
	Attribute("etag", String, "Opaque revision identifier for this configuration.")
	Attribute("updated_at", String, func() {
		Description("When this remote configuration was last saved. Absent when is_configured is false.")
		Format(FormatDateTime)
	})
})

var AgentMarketplaceModel = Type("AgentMarketplace", func() {
	Required("name", "url")
	Attribute("name", String, "Stable identifier for the marketplace, used as its key when the agent registers it with a managed tool. Matches the name written into the published marketplace.json, derived from the organization name (for example, `<org-slug>-gram`), so plugin references resolve deterministically across polls.")
	Attribute("url", String, "Git URL for the marketplace, served by the marketplace proxy.")
})

var AgentPluginModel = Type("AgentPlugin", func() {
	Required("slug", "marketplace_name")
	Attribute("slug", String, "Plugin slug. Combined with marketplace_name, this identifies the plugin the agent enables in the managed tool.")
	Attribute("marketplace_name", String, "Name of the marketplace this plugin lives in. Always equals the `name` of one of the marketplaces in the same response.")
})

var SyncedAgentUserModel = Type("SyncedAgentUser", func() {
	Required("email", "first_seen_at", "last_seen_at")
	Attribute("email", String, "Email the device agent reported on sync. Resolve against org members for display.", func() {
		Example("dev@acme.corp")
	})
	Attribute("first_seen_at", String, func() {
		Description("First time this email was seen syncing the device agent.")
		Format(FormatDateTime)
	})
	Attribute("last_seen_at", String, func() {
		Description("Most recent time this email was seen syncing the device agent.")
		Format(FormatDateTime)
	})
})

var ListSyncedUsersResult = Type("ListSyncedUsersResult", func() {
	Required("users")
	Attribute("users", ArrayOf(SyncedAgentUserModel), "Emails seen syncing the device agent, most recently active first.")
})

var AgentSessionMetaModel = Type("AgentSessionMeta", func() {
	Required("session_id", "chat_id", "updated_at")
	Attribute("session_id", String, "The native harness session identifier this entry resolves, echoed from the request.")
	Attribute("chat_id", String, "Gram chat id for the captured session.", func() {
		Format(FormatUUID)
	})
	Attribute("title", String, "Generated (or manually set) chat title. Absent when no title has been generated yet.")
	Attribute("updated_at", String, func() {
		Description("Last activity recorded for the captured session.")
		Format(FormatDateTime)
	})
})

var GetSessionMetaResult = Type("GetSessionMetaResult", func() {
	Required("sessions")
	Attribute("sessions", ArrayOf(AgentSessionMetaModel), "Metadata for the requested sessions that exist and are owned by the calling user. Requested ids with no captured chat or another owner are omitted.")
})

var CreateSessionHandoffResult = Type("CreateSessionHandoffResult", func() {
	Required("url", "expires_at")
	Attribute("url", String, "Capability URL serving the uploaded handoff markdown. Unauthenticated by design — the unguessable token is the credential — and dead after the first read or expiry.")
	Attribute("expires_at", String, func() {
		Description("When the link stops being served regardless of reads.")
		Format(FormatDateTime)
	})
})
