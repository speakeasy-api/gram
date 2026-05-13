package tools

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/urn"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("tools", func() {
	Description("Dashboard API for interacting with tools.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listTools", func() {
		Description("List all tools for a project")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("deployment_id", String, "The deployment ID. If unset, latest deployment will be used.")
			Attribute("cursor", String, "The cursor to fetch results from")
			Attribute("limit", Int32, "The number of tools to return per page")
			Attribute("urn_prefix", String, "Filter tools by URN prefix (e.g. 'tools:http:kitchen-sink' to match all tools starting with that prefix)")
			Attribute("tool_types", ArrayOf(shared.ToolType), func() {
				Default([]string{
					string(urn.ToolKindHTTP),
					string(urn.ToolKindFunction),
					string(urn.ToolKindPrompt),
					string(urn.ToolKindPlatform),
				})
			})
		})

		Result(ListToolsResult)

		HTTP(func() {
			GET("/rpc/tools.list")
			Param("cursor")
			Param("limit")
			Param("deployment_id")
			Param("urn_prefix")
			Param("tool_types")
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
	Attribute("tools", ArrayOf(shared.Tool), "The list of tools (polymorphic union of HTTP tools and prompt templates)")
	Required("tools")
})
