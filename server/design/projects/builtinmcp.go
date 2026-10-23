package projects

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

func builtinMCPMethods() {
	Method("listBuiltinMcpServers", func() {
		Description("List code-owned MCP servers and their project preferences. Only live organization administrators may discover built-ins. Hidden servers are included for settings management; hiding never changes runtime authorization.")
		Security(security.Session, security.ProjectSlug)
		Payload(func() { security.ProjectPayload(); security.SessionPayload() })
		Result(func() { Attribute("servers", ArrayOf(BuiltinMcpServer)); Required("servers") })
		HTTP(func() { GET("/rpc/projects.listBuiltinMcpServers"); security.SessionHeader(); security.ProjectHeader() })
		Meta("openapi:operationId", "listBuiltinMcpServers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"BuiltinMcpServers"}`)
	})
	Method("updateBuiltinMcpServer", func() {
		Description("Update visual visibility and the default environment of a built-in MCP server. Only live organization administrators may update settings; humans cannot connect to built-ins. Requires the current version.")
		Security(security.Session, security.ProjectSlug)
		Payload(func() {
			security.ProjectPayload()
			security.SessionPayload()
			Attribute("key", String, "Immutable built-in key")
			Attribute("hidden", Boolean, "Hide from discovery only; runtime access and gateway membership do not change")
			Attribute("environment_id", String, "Project environment override; omit to use the project's default environment", func() { Format(FormatUUID) })
			Attribute("expected_version", String, "Version returned by the latest read")
			Required("key", "hidden", "expected_version")
			Meta("openapi:typename", "UpdateBuiltinMcpServerForm")
		})
		Result(BuiltinMcpServer)
		HTTP(func() {
			POST("/rpc/projects.updateBuiltinMcpServer")
			security.SessionHeader()
			security.ProjectHeader()
		})
		Meta("openapi:operationId", "updateBuiltinMcpServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"UpdateBuiltinMcpServer"}`)
	})
}

var BuiltinMcpTool = Type("BuiltinMcpTool", func() {
	Attribute("name", String)
	Attribute("description", String)
	Attribute("input_schema", String, "JSON input schema")
	Required("name", "description", "input_schema")
})
var BuiltinMcpServer = Type("BuiltinMcpServer", func() {
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("key", String)
	Attribute("name", String)
	Attribute("description", String)
	Attribute("url", String)
	Attribute("hidden", Boolean)
	Attribute("environment_id", String, func() { Format(FormatUUID) })
	Attribute("version", String)
	Attribute("tools", ArrayOf(BuiltinMcpTool))
	Attribute("credential_variables", ArrayOf(String))
	Required("id", "key", "name", "description", "url", "hidden", "version", "tools", "credential_variables")
})
