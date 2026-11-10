package hiddenmcpservers

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var HiddenServer = Type("HiddenMcpServer", func() {
	Description("An MCP server the signed-in user hid.")
	Required("mcp_server_id", "project_id", "project_slug", "hidden_at")
	Attribute("mcp_server_id", String, "The hidden MCP server.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The server's project.", func() {
		Format(FormatUUID)
	})
	Attribute("project_slug", String, "The server's project slug.")
	Attribute("slug", String, "The server's slug, when it has one.")
	Attribute("name", String, "The server's display name, when it has one.")
	Attribute("hidden_at", String, "When the user hid the server.", func() {
		Format(FormatDateTime)
	})
})

var ListResult = Type("ListHiddenMcpServersResult", func() {
	Required("servers")
	Attribute("servers", ArrayOf(HiddenServer), "Hidden servers, oldest first.")
})

var _ = Service("hiddenMcpServers", func() {
	Description("MCP servers the signed-in user hid. Gateways leave hidden servers out of the user's tools and list them last on the consent page. Each user manages only their own list.")

	shared.DeclareErrorResponses()

	Method("list", func() {
		Description("List the MCP servers the signed-in user hid in the active organization.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListResult)

		HTTP(func() {
			GET("/rpc/hiddenMcpServers.list")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listHiddenMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "HiddenMcpServers"}`)
	})

	Method("hide", func() {
		Description("Hide an MCP server for the signed-in user. Hiding a hidden server changes nothing.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "HideMcpServerRequestBody")
			Attribute("mcp_server_id", String, "The MCP server.", func() {
				Format(FormatUUID)
			})
			Required("mcp_server_id")
		})

		HTTP(func() {
			POST("/rpc/hiddenMcpServers.hide")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "hideMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "hide")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "HideMcpServer"}`)
	})

	Method("unhide", func() {
		Description("Show a hidden MCP server again for the signed-in user. Unhiding a server that is not hidden changes nothing.")

		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Meta("openapi:typename", "UnhideMcpServerRequestBody")
			Attribute("mcp_server_id", String, "The MCP server.", func() {
				Format(FormatUUID)
			})
			Required("mcp_server_id")
		})

		HTTP(func() {
			POST("/rpc/hiddenMcpServers.unhide")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "unhideMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "unhide")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UnhideMcpServer"}`)
	})
})
