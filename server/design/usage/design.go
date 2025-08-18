package usage

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

// PeriodUsage represents the usage of a project for a given period.
var PeriodUsage = Type("PeriodUsage", func() {
	Attribute("tool_calls", Int, "The number of tool calls used")
	Attribute("max_tool_calls", Int, "The maximum number of tool calls allowed")
	Attribute("servers", Int, "The number of servers used")
	Attribute("max_servers", Int, "The maximum number of servers allowed")

	Required("tool_calls", "max_tool_calls", "servers", "max_servers")
})

var _ = Service("usage", func() {
	Description("Read usage for gram.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("getPeriodUsage", func() {
		Description("Get the usage for a project for a given period")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(PeriodUsage)

		HTTP(func() {
			GET("/rpc/usage.getPeriodUsage")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getPeriodUsage")
		Meta("openapi:extension:x-speakeasy-name-override", "getPeriodUsage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "getPeriodUsage"}`)
	})

	Method("createCheckout", func() {
		Description("Create a checkout link for upgrading to the business plan")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(String)

		HTTP(func() {
			POST("/rpc/usage.createCheckout")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createCheckout")
		Meta("openapi:extension:x-speakeasy-name-override", "createCheckout")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "createCheckout"}`)
	})
})
