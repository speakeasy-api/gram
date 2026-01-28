package tools

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("instances", func() {
	Description("Consumer APIs for interacting with all relevant data for an instance of a toolset and environment.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("consumer")
	})
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("chat")
	})
	Security(security.ChatSessionsToken)
	shared.DeclareErrorResponses()

	Method("getInstance", func() {
		Description("Load all relevant data for an instance of a toolset and environment")

		Payload(GetInstanceForm)

		Result(GetInstanceResult)

		HTTP(func() {
			GET("/rpc/instances.get")
			Param("toolset_slug")
			security.SessionHeader()
			security.ProjectHeader()
			security.ByKeyHeader()
			security.ChatSessionsTokenHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getInstance")
		Meta("openapi:extension:x-speakeasy-name-override", "getBySlug")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Instance"}`)
	})
})

var GetInstanceForm = Type("GetInstanceForm", func() {
	security.SessionPayload()
	security.ByKeyPayload()
	security.ProjectPayload()
	security.ChatSessionsTokenPayload()
	Attribute("toolset_slug", shared.Slug, "The slug of the toolset to load")
	Required("toolset_slug")
})

var InstanceMcpServer = Type("InstanceMcpServer", func() {
	Attribute("url", String, "The address of the MCP server")
	Required("url")
})

var GetInstanceResult = Type("GetInstanceResult", func() {
	Attribute("name", String, "The name of the toolset")
	Attribute("description", String, "The description of the toolset")
	Attribute("tools", ArrayOf(shared.Tool), "The list of tools")
	Attribute("prompt_templates", ArrayOf(shared.PromptTemplate), "The list of prompt templates")
	Attribute("security_variables", ArrayOf(shared.SecurityVariable), "The security variables that are relevant to the toolset")
	Attribute("server_variables", ArrayOf(shared.ServerVariable), "The server variables that are relevant to the toolset")
	Attribute("function_environment_variables", ArrayOf(shared.FunctionEnvironmentVariable), "The function environment variables that are relevant to the toolset")
	Attribute("external_mcp_header_definitions", ArrayOf(shared.ExternalMCPHeaderDefinition), "The external MCP header definitions that are relevant to the toolset")
	Attribute("mcp_servers", ArrayOf(InstanceMcpServer), "The MCP servers that are relevant to the toolset")
	Required("name", "tools", "mcp_servers")
})
