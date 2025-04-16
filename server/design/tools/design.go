package tools

import (
	"github.com/speakeasy-api/gram/design/security"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("tools", func() {
	Description("Dashboard API for interacting with tools.")
	Security(security.Session, security.ProjectSlug)

	Method("listTools", func() {
		Description("List all tools for a project")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("cursor", String, "The cursor to fetch results from")
		})

		Result(ListToolsResult)

		HTTP(func() {
			GET("/rpc/tools.list")
			Param("cursor")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listTools")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListTools"}`)
	})
})

var ListToolsResult = Type("ListToolsResult", func() {
	Attribute("next_cursor", String, "The cursor to fetch results from")
	Attribute("tools", ArrayOf(HTTPToolDefinition), "The list of tools")
	Required("tools")
})
