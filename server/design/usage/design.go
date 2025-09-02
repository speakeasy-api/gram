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
	Attribute("servers", Int, "The number of servers used, according to the Polar meter")
	Attribute("max_servers", Int, "The maximum number of servers allowed")
	Attribute("actual_public_server_count", Int, "The number of servers set to public at the time of the request")

	Required("tool_calls", "max_tool_calls", "servers", "max_servers", "actual_public_server_count")
})

var TierLimits = Type("TierLimits", func() {
	Attribute("base_price", Float64, "The base price for the tier")
	Attribute("included_tool_calls", Int, "The number of tool calls included in the tier")
	Attribute("included_servers", Int, "The number of servers included in the tier")
	Attribute("price_per_additional_tool_call", Float64, "The price per additional tool call")
	Attribute("price_per_additional_server", Float64, "The price per additional server")
	Attribute("description_bullets", ArrayOf(String), "The description bullets of the tier")
	
	Required("base_price", "included_tool_calls", "included_servers", "price_per_additional_tool_call", "price_per_additional_server", "description_bullets")
})

var UsageTiers = Type("UsageTiers", func() {
	Attribute("free", TierLimits, "The limits for the free tier")	
	Attribute("pro", TierLimits, "The limits for the pro tier")
	Attribute("enterprise", TierLimits, "The limits for the enterprise tier")

	Required("free", "pro", "enterprise")
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

	Method("getUsageTiers", func() {
		Description("Get the usage tiers")

		NoSecurity()

		Result(UsageTiers)

		HTTP(func() {
			GET("/rpc/usage.getUsageTiers")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getUsageTiers")
		Meta("openapi:extension:x-speakeasy-name-override", "getUsageTiers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "getUsageTiers"}`)
	})

	Method("createCustomerSession", func() {
		Description("Create a customer session for the user")
		
		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(String)
		
		HTTP(func() {
			POST("/rpc/usage.createCustomerSession")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		
		Meta("openapi:operationId", "createCustomerSession")
		Meta("openapi:extension:x-speakeasy-name-override", "createCustomerSession")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "createCustomerSession"}`)
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
