package usage

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

// PeriodUsage represents the usage of a project for a given period.
var PeriodUsage = Type("PeriodUsage", func() {
	Attribute("tool_calls", Int, "The number of tool calls used")
	Attribute("included_tool_calls", Int, "The number of tool calls included in the tier")

	Attribute("servers", Int, "The number of servers used, according to the Polar meter")
	Attribute("included_servers", Int, "The number of servers included in the tier")
	Attribute("actual_enabled_server_count", Int, "The number of servers enabled at the time of the request")

	Attribute("credits", Int, "The number of credits used")
	Attribute("included_credits", Int, "The number of credits included in the tier")

	Attribute("has_active_subscription", Boolean, "Whether the project has an active subscription")

	Required("tool_calls", "included_tool_calls", "servers", "included_servers", "actual_enabled_server_count", "credits", "included_credits", "has_active_subscription")
})

var TierLimits = Type("TierLimits", func() {
	Attribute("base_price", Float64, "The base price for the tier")
	Attribute("included_tool_calls", Int, "The number of tool calls included in the tier")
	Attribute("included_servers", Int, "The number of servers included in the tier")
	Attribute("included_credits", Int, "The number of credits included in the tier for playground and other dashboard activities")
	Attribute("price_per_additional_tool_call", Float64, "The price per additional tool call")
	Attribute("price_per_additional_server", Float64, "The price per additional server")
	Attribute("feature_bullets", ArrayOf(String), "Key feature bullets of the tier")
	Attribute("included_bullets", ArrayOf(String), "Included items bullets of the tier")
	Attribute("add_on_bullets", ArrayOf(String), "Add-on items bullets of the tier (optional)")

	Required("base_price", "included_tool_calls", "included_servers", "included_credits", "price_per_additional_tool_call", "price_per_additional_server", "feature_bullets", "included_bullets")
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
