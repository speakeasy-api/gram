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
		Description("Resolve the set of plugins assigned to the enrolled user and return them as a plain description the agent can render into local AI-tool configs (Claude Code, etc.).")

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
	Required("etag", "plugins")
	Attribute("etag", String, "Opaque revision identifier covering the plugin set. The agent stores this and can use it to detect whether a re-fetch produced any changes.")
	Attribute("plugins", ArrayOf(AgentPluginModel), "Plugins assigned to the resolved principal set, sorted by slug.")
})

var AgentPluginModel = Type("AgentPlugin", func() {
	Required("id", "slug", "name", "servers")

	Attribute("id", String, func() {
		Description("Plugin id.")
		Format(FormatUUID)
	})
	Attribute("slug", String, "URL-safe identifier, unique per (org, project).")
	Attribute("name", String, "Display name.")
	Attribute("description", String, "Optional description.")
	Attribute("servers", ArrayOf(AgentPluginServerModel), "MCP servers bundled in this plugin, ordered by sort_order.")
})

var AgentPluginServerModel = Type("AgentPluginServer", func() {
	Required("display_name", "policy", "mcp_url", "is_public")

	Attribute("display_name", String, "Display name shown in the generated AI-tool config.")
	Attribute("policy", String, func() {
		Description("Whether the agent should treat this server as required or optional.")
		Enum("required", "optional")
	})
	Attribute("mcp_url", String, "Gram-hosted MCP URL the AI tool should connect to.")
	Attribute("is_public", Boolean, "True when the MCP server is publicly accessible and does not require a Gram credential.")
})
