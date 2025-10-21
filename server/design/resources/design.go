package resources

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("resources", func() {
	Description("Dashboard API for interacting with resources.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listResources", func() {
		Description("List all resources for a project")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("deployment_id", String, "The deployment ID. If unset, latest deployment will be used.")
			Attribute("cursor", String, "The cursor to fetch results from")
			Attribute("limit", Int32, "The number of resources to return per page")
		})

		Result(ListResourcesResult)

		HTTP(func() {
			GET("/rpc/resources.list")
			Param("cursor")
			Param("limit")
			Param("deployment_id")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listResources")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListResources"}`)
	})
})

var ListResourcesResult = Type("ListResourcesResult", func() {
	Attribute("next_cursor", String, "The cursor to fetch results from")
	Attribute("resources", ArrayOf(shared.Resource), "The list of resources")
	Required("resources")
})
