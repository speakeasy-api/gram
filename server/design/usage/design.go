package usage

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/constants"
	. "goa.design/goa/v3/dsl"
)

// PeriodUsage represents the usage of a project for a given period.
var PeriodUsage = Type("PeriodUsage", func() {
	Attribute("tool_calls", Int, "The number of tool calls used")
	Attribute("included_tool_calls", Int, "The number of tool calls included in the tier")

	Attribute("servers", Int, "The number of servers used, according to the Polar meter")
	Attribute("included_servers", Int, "The number of servers included in the tier")
	Attribute("actual_enabled_server_count", Int, "The number of servers enabled at the time of the request")

	Attribute("credits", Int, "The number of credits used. Only populated for platform admins.")
	Attribute("included_credits", Int, "The number of credits included in the tier. Only populated for platform admins.")

	Attribute("has_active_subscription", Boolean, "Whether the project has an active subscription")

	Required("tool_calls", "included_tool_calls", "servers", "included_servers", "actual_enabled_server_count", "has_active_subscription")
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
	Attribute("tum_price_per_million_usd", String, "Exact USD list price per million tokens under management (optional)")

	Required("base_price", "included_tool_calls", "included_servers", "included_credits", "price_per_additional_tool_call", "price_per_additional_server", "feature_bullets", "included_bullets")
})

var UsageTiers = Type("UsageTiers", func() {
	Attribute("free", TierLimits, "The limits for the free tier")
	Attribute("pro", TierLimits, "The limits for the pro tier")
	Attribute("payg", TierLimits, "The limits for the pay-as-you-go tier")
	Attribute("enterprise", TierLimits, "The limits for the enterprise tier")

	Required("free", "pro", "payg", "enterprise")
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

// BillingEmail is the optional billing notification address configured for a
// PAYG organization.
var BillingEmail = Type("BillingEmail", func() {
	Attribute("email", String, "The configured billing notification email. Omitted when organization administrators receive billing notifications.", func() {
		Format(FormatEmail)
	})
})

// SpendCap is the monthly USD ceiling enforced by one of the organization's
// platform-managed inference keys.
var SpendCap = Type("SpendCap", func() {
	Attribute("key_type", String, "The platform-managed inference key whose cap is reported", func() {
		Enum("chat", "internal")
	})
	Attribute("monthly_credits", Int, "The monthly inference spend cap in USD", func() {
		Minimum(constants.MinimumPaygSpendCapUSD)
		Maximum(constants.MaximumPaygSpendCapUSD)
	})

	Required("key_type", "monthly_credits")
})

// InferenceSpendCap reports current usage and the enforced monthly ceiling for
// one materialized platform-managed inference key.
var InferenceSpendCap = Type("InferenceSpendCap", func() {
	Attribute("key_type", String, "The platform-managed inference function", func() {
		Enum("chat", "internal")
	})
	Attribute("credits_used", Float64, "Monthly usage in USD")
	Attribute("monthly_credits", Int, "The enforced monthly spend cap in USD")
	Attribute("disabled", Boolean, "Whether the platform-managed key is disabled")

	Required("key_type", "credits_used", "monthly_credits", "disabled")
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
			Attribute("tunneled_mcp_server_limit", Int, "The contracted tunneled MCP server source cap. Omit to leave the configured value unchanged; never-configured orgs use the plan default.", func() {
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

	Method("getBillingEmail", func() {
		Description("Get the billing notification email for a PAYG organization")

		Payload(func() {
			security.SessionPayload()
		})

		Result(BillingEmail)

		HTTP(func() {
			GET("/rpc/usage.getBillingEmail")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getBillingEmail")
		Meta("openapi:extension:x-speakeasy-name-override", "getBillingEmail")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "getBillingEmail"}`)
	})

	Method("setBillingEmail", func() {
		Description("Set or clear the billing notification email for a PAYG organization")

		Payload(func() {
			security.SessionPayload()
			Attribute("email", String, "The billing notification email. Omit to notify organization administrators.", func() {
				Format(FormatEmail)
			})
		})

		Result(BillingEmail)

		HTTP(func() {
			POST("/rpc/usage.setBillingEmail")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setBillingEmail")
		Meta("openapi:extension:x-speakeasy-name-override", "setBillingEmail")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "setBillingEmail"}`)
	})

	Method("setSpendCap", func() {
		Description("Set the monthly spend cap for one of a PAYG organization's platform-managed inference keys")

		Payload(func() {
			security.SessionPayload()
			Attribute("key_type", String, "The platform-managed inference key to update. Defaults to chat for compatibility.", func() {
				Enum("chat", "internal")
			})
			Attribute("monthly_credits", Int, "The monthly inference spend cap in USD", func() {
				Minimum(constants.MinimumPaygSpendCapUSD)
				Maximum(constants.MaximumPaygSpendCapUSD)
			})
			Required("monthly_credits")
		})

		Result(SpendCap)

		HTTP(func() {
			POST("/rpc/usage.setSpendCap")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setSpendCap")
		Meta("openapi:extension:x-speakeasy-name-override", "setSpendCap")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "setSpendCap"}`)
	})

	Method("getInferenceSpendCaps", func() {
		Description("List current usage and caps for the organization's materialized platform-managed inference keys")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ArrayOf(InferenceSpendCap))

		HTTP(func() {
			GET("/rpc/usage.getInferenceSpendCaps")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getInferenceSpendCaps")
		Meta("openapi:extension:x-speakeasy-name-override", "getInferenceSpendCaps")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "getInferenceSpendCaps"}`)
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

	Method("createStripeCheckout", func() {
		Description("Create a Stripe Checkout link for starting PAYG billing")

		Payload(func() {
			security.SessionPayload()
		})

		Result(String)

		HTTP(func() {
			POST("/rpc/usage.createStripeCheckout")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createStripeCheckout")
		Meta("openapi:extension:x-speakeasy-name-override", "createStripeCheckout")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "createStripeCheckout"}`)
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
