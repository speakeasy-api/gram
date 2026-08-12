package agent

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// --- Service ---

var _ = Service("agent", func() {
	Description("Endpoints consumed by the Speakeasy device agent running on developer machines. Authenticates via an API key carrying the 'agent_user' scope — the per-user credential minted by token-exchange. An org key with the broader 'agent' scope also satisfies these endpoints (it implies 'agent_user'), so existing installs keep working during the transition.")
	Security(security.ByKey, func() {
		Scope("agent_user")
	})
	shared.DeclareErrorResponses()

	Method("getPlugins", func() {
		Description("Resolve the marketplaces, plugins, and optional organization configuration assigned to the enrolled user. The device agent reconciles these into the AI developer tools it manages. Organization configuration is delivered on this existing poll so agents do not need a second control-plane request.")

		// Authenticated with an API key carrying the `agent_user` scope — the
		// per-user key minted by token-exchange, so the enrolled user is the key
		// owner and the org derives from the key. An org `agent` install key also
		// passes (it implies `agent_user`; see auth.Authorize), so agents still
		// on the shared org key keep working during the transition. `email` is
		// retained as an optional param for the legacy vouched-email path.
		Security(security.ByKey, func() {
			Scope("agent_user")
		})

		Payload(func() {
			security.ByKeyPayload()
			// Authoritative when authenticating with an org install key (`agent`
			// scope): that key's owner is an admin, not the enrolled developer, so
			// the MDM profile vouches the developer's email here. Ignored for a
			// per-user key (`agent_user`), whose owner is the enrolled user.
			Attribute("email", String, "Email address of the enrolled user. Authoritative when authenticating with an org-scoped agent install key (the MDM zero-touch path); ignored for a per-user key, whose owner is the enrolled user.", func() {
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
			Required("email")
		})

		Result(GetPluginsResult)

		HTTP(func() {
			GET("/rpc/agent.getPlugins")
			security.ByKeyHeader()
			Param("email")
			// Carried as headers rather than query params: a serial is a
			// durable hardware identifier and a hostname frequently embeds a
			// person's name, and the request logger records URLs (see
			// middleware/logging.go) but not these headers. `email` is already
			// a query param for compatibility; that is not a reason to widen
			// what lands in access logs.
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
		Description("Get the organization-wide device-agent configuration for the dashboard. Requires a session with the org:read scope. An unconfigured organization returns an empty document with is_configured=false; enrolled agents do not receive a remote layer until an administrator saves one.")

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
