package agent

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// --- Service ---

var _ = Service("agent", func() {
	Description("Endpoints consumed by the Speakeasy device agent running on developer machines. Authenticates via an org-scoped API key carrying the 'agent' scope.")
	Security(security.ByKey, func() {
		Scope("agent")
	})
	shared.DeclareErrorResponses()

	Method("getPlugins", func() {
		Description("Resolve the set of plugins assigned to the enrolled user and return them as Claude Code marketplace + plugin references. The agent merges these into the local Claude Code settings so Claude Code's own plugin manager fetches and installs the bundles.")

		Payload(func() {
			security.ByKeyPayload()
			Attribute("email", String, "Email address of the enrolled user. Used to resolve plugin assignments against principal URNs.", func() {
				Example("dev@acme.corp")
			})
			Required("email")
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
	Attribute("marketplaces", ArrayOf(AgentMarketplaceModel), "Marketplaces the agent should register in Claude Code's `extraKnownMarketplaces`. Sorted by name.")
	Attribute("plugins", ArrayOf(AgentPluginModel), "Plugins the agent should list in Claude Code's `enabledPlugins`. Each entry references one of the marketplaces above by name.")
})

var AgentMarketplaceModel = Type("AgentMarketplace", func() {
	Required("name", "url", "auto_update")
	Attribute("name", String, "Stable identifier used as the key in Claude Code's `extraKnownMarketplaces` map. Matches the name written into the published marketplace.json, derived from the organization name (for example, `<org-slug>-gram`) so plugin references resolve deterministically across polls.")
	Attribute("url", String, "Git URL for the marketplace, served by Gram's marketplace proxy.")
	Attribute("auto_update", Boolean, "Whether Claude Code should auto-update the marketplace.")
})

var AgentPluginModel = Type("AgentPlugin", func() {
	Required("slug", "marketplace_name")
	Attribute("slug", String, "Plugin slug. Combined with marketplace_name this is what goes into Claude Code's `enabledPlugins` entries.")
	Attribute("marketplace_name", String, "Name of the marketplace this plugin lives in. Always equals the `name` of one of the marketplaces in the same response.")
})
