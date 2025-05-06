package tools

import (
	"github.com/speakeasy-api/gram/design/security"
	"github.com/speakeasy-api/gram/design/shared"
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
		})

		Result(ListToolsResult)

		HTTP(func() {
			GET("/rpc/tools.list")
			Param("cursor")
			Param("deployment_id")
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
	Attribute("tools", ArrayOf(ToolEntry), "The list of tools")
	Required("tools")
})

var ToolEntry = Type("ToolEntry", func() {
	Required("id", "deploymentId", "name", "summary", "openapiv3DocumentId", "created_at")

	Attribute("id", String, "The tool ID")
	Attribute("deploymentId", String, "The deployment ID")
	Attribute("name", String, "The tool name")
	Attribute("summary", String, "The tool summary")
	Attribute("openapiv3DocumentId", String, "The OpenAPI v3 document ID")
	Attribute("packageName", String, "The package name")
	Attribute("created_at", String, func() {
		Description("The creation date of the tool.")
		Format(FormatDateTime)
	})
})
