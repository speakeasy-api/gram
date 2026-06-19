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
		Description("Resolve the marketplaces and plugins assigned to the enrolled user. The device agent reconciles these into whichever AI developer tools it manages (Claude Code today), so each tool's own plugin manager fetches and installs the bundles. The response is tool-agnostic: it names what to install, and each tool's syncer decides how to render it into that tool's native configuration.")

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
