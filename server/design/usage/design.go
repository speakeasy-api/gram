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

// TUMPeriodDay is one UTC day of tokens under management within a billing
// cycle.
var TUMPeriodDay = Type("TUMPeriodDay", func() {
	Attribute("date", String, "The UTC day", func() {
		Format(FormatDate)
	})
	Attribute("tokens", Int64, "Tokens under management consumed on this day")

	Required("date", "tokens")
})

// TUMPeriod is tokens under management for one billing cycle.
var TUMPeriod = Type("TUMPeriod", func() {
	Attribute("period_start", String, "Start of the billing cycle", func() {
		Format(FormatDateTime)
	})
	Attribute("period_end", String, "End of the billing cycle (exclusive)", func() {
		Format(FormatDateTime)
	})
	Attribute("tokens", Int64, "Tokens under management consumed during the cycle")
	Attribute("days", ArrayOf(TUMPeriodDay), "Daily breakdown of TUM within the cycle. Days without usage are omitted.")

	Required("period_start", "period_end", "tokens", "days")
})

// TokensUnderManagement reports TUM consumption for the active billing cycle
// alongside the contracted terms for the organization.
var TokensUnderManagement = Type("TokensUnderManagement", func() {
	Attribute("period_start", String, "Start of the active billing cycle", func() {
		Format(FormatDateTime)
	})
	Attribute("period_end", String, "End of the active billing cycle (exclusive)", func() {
		Format(FormatDateTime)
	})
	Attribute("tokens", Int64, "Tokens under management consumed during the active billing cycle")
	Attribute("monthly_token_limit", Int64, "The contracted monthly tokens under management limit, if one has been configured")
	Attribute("tunneled_mcp_server_limit", Int, "The contracted tunneled MCP server source cap, if one has been configured")
	Attribute("billing_cycle_anchor_day", Int, "Day of month (1-31) the billing cycle starts, at 00:00 UTC")
	Attribute("alert_email", String, "Email address to notify on TUM threshold events. Only populated for platform admins.")
	Attribute("history", ArrayOf(TUMPeriod), "TUM usage per billing cycle for the trailing cycles, oldest first. The last entry is the active cycle.")

	Required("period_start", "period_end", "tokens", "billing_cycle_anchor_day", "history")
})

var _ = Service("usage", func() {
	Description("Read usage for gram.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("getPeriodUsage", func() {
		Description("Get the usage for an organization for a given period")

		Payload(func() {
			security.SessionPayload()
		})

		Result(PeriodUsage)

		HTTP(func() {
			GET("/rpc/usage.getPeriodUsage")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getPeriodUsage")
		Meta("openapi:extension:x-speakeasy-name-override", "getPeriodUsage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "getPeriodUsage"}`)
	})

	Method("getTokensUnderManagement", func() {
		Description("Get tokens under management for the active billing cycle alongside the contracted terms")

		Payload(func() {
			security.SessionPayload()
		})

		Result(TokensUnderManagement)

		HTTP(func() {
			GET("/rpc/usage.getTokensUnderManagement")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getTokensUnderManagement")
		Meta("openapi:extension:x-speakeasy-name-override", "getTokensUnderManagement")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "getTokensUnderManagement"}`)
	})

	Method("setBillingMetadata", func() {
		Description("Set an organization's billing contract terms. Restricted to platform admins.")

		Payload(func() {
			security.SessionPayload()
			Attribute("monthly_token_limit", Int64, "The contracted monthly tokens under management limit. Omit to clear.", func() {
				Minimum(0)
			})
			Attribute("tunneled_mcp_server_limit", Int, "The contracted tunneled MCP server source cap. Omit to use the plan default.", func() {
				Minimum(0)
			})
			Attribute("alert_email", String, "Email address to notify on TUM threshold events. Omit to clear.", func() {
				Format(FormatEmail)
			})
			Attribute("billing_cycle_anchor_day", Int, "Day of month (1-31) the billing cycle starts, at 00:00 UTC", func() {
				Minimum(1)
				Maximum(31)
			})

			Required("billing_cycle_anchor_day")
		})

		Result(TokensUnderManagement)

		HTTP(func() {
			POST("/rpc/usage.setBillingMetadata")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setBillingMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "setBillingMetadata")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "setBillingMetadata"}`)
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
		})

		Result(String)

		HTTP(func() {
			POST("/rpc/usage.createCustomerSession")
			security.SessionHeader()
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
		})

		Result(String)

		HTTP(func() {
			POST("/rpc/usage.createCheckout")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createCheckout")
		Meta("openapi:extension:x-speakeasy-name-override", "createCheckout")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "createCheckout"}`)
	})

	Method("createTopUpCheckout", func() {
		Description("Create a checkout link for a one-time credit top-up purchase")

		Payload(func() {
			security.SessionPayload()
		})

		Result(String)

		HTTP(func() {
			POST("/rpc/usage.createTopUpCheckout")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createTopUpCheckout")
		Meta("openapi:extension:x-speakeasy-name-override", "createTopUpCheckout")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "createTopUpCheckout"}`)
	})
})
