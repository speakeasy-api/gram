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
		Description("Resolve the marketplaces and plugins assigned to the enrolled user. The device agent reconciles these into whichever AI developer tools it manages (Claude Code today), so each tool's own plugin manager fetches and installs the bundles. The response is tool-agnostic: it names what to install, and each tool's syncer decides how to render it into that tool's native configuration.")

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
			// Required when authenticating with an org install key (`agent`
			// scope): that key's owner is an admin, not the enrolled developer,
			// so the MDM profile vouches the developer's email here. Ignored for
			// a per-user key (`agent_user`), whose owner is the enrolled user.
			Attribute("email", String, "Email address of the enrolled user. Required when authenticating with an org-scoped agent install key (the MDM zero-touch path); ignored for a per-user key, whose owner is the enrolled user.", func() {
				Example("dev@acme.corp")
			})
		})

		Result(GetPluginsResult)

		HTTP(func() {
			GET("/rpc/agent.getPlugins")
			security.ByKeyHeader()
			Param("email")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAgentPlugins")
		Meta("openapi:extension:x-speakeasy-name-override", "getPlugins")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentPlugins"}`)
	})
})

// --- Types ---

var GetPluginsResult = Type("GetPluginsResult", func() {
	Required("etag", "marketplaces", "plugins")
	Attribute("etag", String, "Opaque revision identifier covering the marketplace + plugin set. The agent stores this to detect changes between polls.")
	Attribute("marketplaces", ArrayOf(AgentMarketplaceModel), "Plugin marketplaces the agent should register with the tools it manages. Sorted by name.")
	Attribute("plugins", ArrayOf(AgentPluginModel), "Plugins the agent should enable. Each entry references one of the marketplaces above by name.")
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
