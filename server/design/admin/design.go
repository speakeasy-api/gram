package admin

import (
	"fmt"

	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/auditlogs"
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/design/usage"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var MarkEnterpriseTrialConvertedResult = Type("MarkEnterpriseTrialConvertedResult", func() {
	Description("Privacy-minimal result of recording an enterprise trial conversion.")
	Required("organization_id", "converted_at")

	Attribute("organization_id", String, "The converted organization ID.")
	Attribute("converted_at", String, func() {
		Description("The time at which the enterprise trial was recorded as converted.")
		Format(FormatDateTime)
	})
})

var AdminOrganization = Type("AdminOrganization", func() {
	Description("Organization details surfaced to admin operators.")
	Required("id", "name", "slug", "account_type", "whitelisted", "member_count", "created_at", "updated_at")

	Attribute("id", String, "The ID of the organization")
	Attribute("name", String, "The name of the organization")
	Attribute("slug", String, "The slug of the organization")
	Attribute("account_type", String, "Gram account type (e.g. free, pro, payg, enterprise).")
	Attribute("workos_id", String, "WorkOS organization ID, if linked.")
	Attribute("stripe_customer_id", String, "Stripe customer ID, if billing metadata has a customer.")
	Attribute("stripe_subscription_id", String, "Current Stripe subscription ID, if subscribed.")
	Attribute("whitelisted", Boolean, "Whether the organization is whitelisted for full access.")
	Attribute("disabled_at", String, func() {
		Description("The time at which the organization was disabled, if any.")
		Format(FormatDateTime)
	})
	Attribute("trial_state", String, func() {
		Description("Lifecycle state of the organization's enterprise trial.")
		Enum("none", "running", "ending_soon", "expired", "demoted", "converted")
	})
	Attribute("trial_tier", String, "The trial tier. Absent when the organization never trialled.")
	Attribute("trial_ends_at", String, func() {
		Description("The time at which the enterprise trial ends. Absent when the organization never trialled.")
		Format(FormatDateTime)
	})
	Attribute("trial_converted_at", String, func() {
		Description("The time at which the trial converted to a paid plan, if any.")
		Format(FormatDateTime)
	})
	Attribute("trial_demoted_at", String, func() {
		Description("The time at which the organization was demoted after its trial, if any.")
		Format(FormatDateTime)
	})
	Attribute("member_count", Int, "Number of active members in the organization.")
	// Deliberately not an Enum. The value is whatever the creating flow
	// recorded, so a new flow added on the server would otherwise fail response
	// validation until this list caught up. Absent means no flow recorded one.
	Attribute("creation_source", String, "The flow that created the organization (e.g. signup, assistants, platform_admin). Absent when nothing recorded one. Informational only.")
	Attribute("created_at", String, func() {
		Description("The creation date of the organization.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the organization.")
		Format(FormatDateTime)
	})
})

var AdminProject = Type("AdminProject", func() {
	Description("Project summary surfaced to admin operators.")
	Required("id", "name", "slug", "mcp_server_count", "created_at", "updated_at")

	Attribute("id", String, "The ID of the project")
	Attribute("name", String, "The name of the project")
	Attribute("slug", String, "The slug of the project")
	Attribute("mcp_server_count", Int, "Number of MCP servers in the project, counting both toolset-backed servers and mcp_servers rows.")
	Attribute("created_at", String, func() {
		Description("The creation date of the project.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the project.")
		Format(FormatDateTime)
	})
})

var AdminProjectDetail = Type("AdminProjectDetail", func() {
	Description("Full project detail surfaced to admin operators, including aggregated counts of child resources.")
	Required(
		"id",
		"name",
		"slug",
		"organization_id",
		"toolset_count",
		"deployment_count",
		"http_tool_count",
		"environment_count",
		"api_key_count",
		"assistant_count",
		"created_at",
		"updated_at",
	)

	Attribute("id", String, "Project ID.")
	Attribute("name", String, "Project name.")
	Attribute("slug", String, "Project slug.")
	Attribute("organization_id", String, "Owning organization ID.")
	Attribute("logo_asset_id", String, "Project logo asset ID, if set.")
	Attribute("functions_runner_version", String, "Functions runner version pin, if set.")
	Attribute("toolset_count", Int, "Number of active toolsets in the project.")
	Attribute("deployment_count", Int, "Total number of deployments in the project.")
	Attribute("http_tool_count", Int, "Number of active HTTP tool definitions in the project.")
	Attribute("environment_count", Int, "Number of active environments in the project.")
	Attribute("api_key_count", Int, "Number of active API keys in the project.")
	Attribute("assistant_count", Int, "Number of active assistants in the project.")
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
})

var AdminOrganizationMember = Type("AdminOrganizationMember", func() {
	Description("Organization member surfaced to admin operators.")
	Required("id", "email", "display_name", "created_at", "updated_at")

	Attribute("id", String, "User ID.")
	Attribute("email", String, "User email address.")
	Attribute("display_name", String, "User display name.")
	Attribute("last_login", String, func() {
		Description("The time the user last logged in, if any.")
		Format(FormatDateTime)
	})
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
})

var AdminListOrganizationMembersResult = Type("AdminListOrganizationMembersResult", func() {
	Required("members")

	Attribute("members", ArrayOf(AdminOrganizationMember), "The members of the organization.")
})

var AdminListOrganizationProjectsResult = Type("AdminListOrganizationProjectsResult", func() {
	Required("projects")

	Attribute("projects", ArrayOf(AdminProject), "The projects belonging to the organization.")
})

var AdminListOrganizationsResult = Type("AdminListOrganizationsResult", func() {
	Required("organizations", "total")

	Attribute("organizations", ArrayOf(AdminOrganization), "The page of organizations.")
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted. Omitted in offset mode.")
	Attribute("total", Int64, "Number of organizations matching the filters, before paging.")
})

var AdminListOrganizationActivityResult = Type("AdminListOrganizationActivityResult", func() {
	Required("logs")

	Attribute("logs", ArrayOf(auditlogs.AuditLog), "List of organization activity.")
	Attribute("next_cursor", String, "Cursor for the next page of results.")
})

var AdminOrganizationStats = Type("AdminOrganizationStats", func() {
	Description("Platform-wide organization counts surfaced above the admin organizations list.")
	Required("total", "created_last_7_days", "customers", "customers_created_last_7_days", "trials_ending_soon", "disabled", "disabled_last_7_days")

	Attribute("total", Int64, "Every organization on the platform, disabled ones included.")
	Attribute("created_last_7_days", Int64, "Organizations created in the last 7 days, whatever their current status.")
	Attribute("customers", Int64, "Organizations on a paid account type (payg or enterprise), disabled ones included.")
	Attribute("customers_created_last_7_days", Int64, "Customers created in the last 7 days, whatever their current status.")
	Attribute("trials_ending_soon", Int64, "Organizations whose trial_state is ending_soon.")
	Attribute("disabled", Int64, "Organizations with disabled_at set.")
	Attribute("disabled_last_7_days", Int64, "Organizations disabled in the last 7 days.")
})

var AdminBulkUpdateAccountTypeResult = Type("AdminBulkUpdateAccountTypeResult", func() {
	Description("Outcome of a bulk account type change.")
	Required("updated_ids", "missing_ids")

	Attribute("updated_ids", ArrayOf(String), "IDs of the organizations whose account type was set. Order is unspecified: do not rely on it.")
	Attribute("missing_ids", ArrayOf(String), "IDs from the request that matched no organization, deduplicated and in request order. Nothing was written for these.")
})

var AdminStripeCustomer = Type("AdminStripeCustomer", func() {
	Description("Stripe customer details shown to an admin before assignment.")
	Required("id", "livemode")
	Attribute("id", String)
	Attribute("name", String)
	Attribute("email", String)
	Attribute("description", String)
	Attribute("livemode", Boolean)
})

var AdminStripeSubscription = Type("AdminStripeSubscription", func() {
	Attribute("status", String, func() {
		Enum("incomplete", "incomplete_expired", "trialing", "active", "past_due", "canceled", "unpaid", "paused")
	})
	Attribute("current_period_start", String, func() { Format(FormatDateTime) })
	Attribute("current_period_end", String, func() { Format(FormatDateTime) })
	Attribute("trial_start", String, func() { Format(FormatDateTime) })
	Attribute("trial_end", String, func() { Format(FormatDateTime) })
	Attribute("cancel_at_period_end", Boolean)
	Attribute("cancel_at", String, func() { Format(FormatDateTime) })
	Attribute("canceled_at", String, func() { Format(FormatDateTime) })
	Attribute("payment_failed", Boolean)
	Required("status", "current_period_start", "current_period_end", "cancel_at_period_end", "payment_failed")
})

var AdminInferenceKey = Type("AdminInferenceKey", func() {
	Description("Current usage and configured state for one materialized platform-managed OpenRouter key, without key material or provider identifiers.")
	Attribute("key_type", String)
	Attribute("credits_used", Float64, "Credits spent this month in USD.")
	Attribute("monthly_credits", Int64)
	Attribute("disabled", Boolean)
	Attribute("disable_causes", ArrayOf(String), "Active internal disable causes. Omitted for legacy unclassified rows.")
	Attribute("disable_causes_classified", Boolean, "Whether disable_causes is classified, including an explicitly empty cause set.")
	Required("key_type", "credits_used", "monthly_credits", "disabled", "disable_causes_classified")
})

var AdminInferenceKeyLimit = Type("AdminInferenceKeyLimit", func() {
	Description("The configured monthly limit for one materialized platform-managed OpenRouter key.")
	Attribute("key_type", String)
	Attribute("monthly_credits", Int64)
	Required("key_type", "monthly_credits")
})

var AdminInferenceSpendMonth = Type("AdminInferenceSpendMonth", func() {
	Attribute("period_start", String, func() { Format(FormatDate) })
	Attribute("period_end", String, "Exclusive end of the UTC calendar month.", func() { Format(FormatDate) })
	Attribute("spend_usd", String)
	Required("period_start", "period_end", "spend_usd")
})

var AdminPaygBillingSummary = Type("AdminPaygBillingSummary", func() {
	Attribute("period_start", String, func() { Format(FormatDateTime) })
	Attribute("period_end", String, func() { Format(FormatDateTime) })
	Attribute("tum_tokens", Int64)
	Attribute("tum_unit_price_usd", String)
	Attribute("tum_cost_usd", String)
	Attribute("other_inference_spend_usd", String)
	Attribute("recorded_through", String, func() { Format(FormatDate) })
	Attribute("estimated_total_usd", String)
	Required("period_start", "period_end", "tum_tokens", "tum_unit_price_usd", "tum_cost_usd", "other_inference_spend_usd", "estimated_total_usd")
})
var AdminMeterUsageBucket = Type("AdminMeterUsageBucket", func() {
	Attribute("from", String, "Inclusive UTC day boundary", func() { Format(FormatDateTime) })
	Attribute("to", String, "Exclusive UTC day boundary", func() { Format(FormatDateTime) })
	Attribute("total", String, "Exact integer ordinary usage quantity as a decimal string")
	Required("from", "to", "total")
})

var AdminMeterUsageResponse = Type("AdminMeterUsageResponse", func() {
	Attribute("family", String, func() {
		Enum("agent_session_storage", "mcp_bandwidth", "risk_content_scans")
	})
	Attribute("window", usage.MeterUsageWindow)
	Attribute("billing_cycles", ArrayOf(usage.MeterUsageWindow), "Trailing twelve billing-cycle date windows")
	Attribute("unit", String, func() { Enum("stokens", "bytes") })
	Attribute("total", String, "Exact integer ordinary usage period total as a decimal string")
	Attribute("buckets", ArrayOf(AdminMeterUsageBucket), "Dense UTC daily ordinary usage buckets")
	Attribute("queried_at", String, "Retrieval timestamp, not an ingestion watermark", func() { Format(FormatDateTime) })
	Attribute("measurement_method", String)
	Required("family", "window", "billing_cycles", "unit", "total", "buckets", "queried_at", "measurement_method")
})

var AdminSpendBreakdownResponse = Type("AdminSpendBreakdownResponse", func() {
	Attribute("window", usage.MeterUsageWindow)
	Attribute("billing_cycles", ArrayOf(usage.MeterUsageWindow), "Trailing twelve billing-cycle date windows")
	Attribute("currency", String, func() { Enum("USD") })
	Attribute("pricing_basis", String, func() { Enum("current_payg_list_price") })
	Attribute("queried_at", String, "Retrieval timestamp used to distinguish current and future buckets", func() { Format(FormatDateTime) })
	Attribute("total_cost_usd", String, "Exact estimated total at current PAYG list prices")
	Attribute("products", ArrayOf(usage.SpendProduct), "The three metered products in stable display order")
	Required("window", "billing_cycles", "currency", "pricing_basis", "queried_at", "total_cost_usd", "products")
})

var AdminSession = Type("AdminSession", func() {
	Attribute("email", String)
	Attribute("name", String)
	Required("email")
})

var AdminChatAnalysisSettings = Type("AdminChatAnalysisSettings", func() {
	Attribute("organization_id", String)
	Attribute("work_units_enabled", Boolean)
	Attribute("work_units_daily_cap", Int)
	Attribute("business_memory_enabled", Boolean)
	Attribute("business_memory_daily_cap", Int)
	Attribute("is_default", Boolean)
	Required("organization_id", "work_units_enabled", "work_units_daily_cap", "business_memory_enabled", "business_memory_daily_cap", "is_default")
})

var AdminChatAnalysisTriggerResult = Type("AdminChatAnalysisTriggerResult", func() {
	Attribute("projects_signaled", Int)
	Required("projects_signaled")
})

var AdminDashboardRedirect = Type("AdminDashboardRedirect", func() {
	Attribute("location", String)
	Attribute("cache_control", String)
	Required("location", "cache_control")
})

// Shared so the two write paths, and the service's own copy of the check,
// cannot drift into accepting different sets.
var accountTypes = conv.AnySlice(constants.AccountTypes)

func declareUnavailable() {
	Error(string(oops.CodeUnavailable), func() {
		Description(oops.CodeUnavailable.UserMessage())
		Fault()
	})
}

func declareUnavailableResponse() {
	Response(string(oops.CodeUnavailable), StatusServiceUnavailable, func() { ContentType("application/json") })
}

var _ = Service("admin", func() {
	Description("Operations supporting admin tasks, protected by Google workspace auth.")
	Security(security.AdminAuth)
	shared.DeclareErrorResponses()

	Method("login", func() {
		NoSecurity()

		Payload(func() {
			Attribute("return_to", String, "Optional URL to return the user to after login. Relative paths and absolute URLs whose origin is in the admin allowed-origins list are accepted.")
			Attribute("prompt", String, "Optional OAuth prompt parameter forwarded to the provider. Pass 'none' to attempt silent re-authentication.")
		})

		Result(func() {
			Required("location", "state_cookie")
			Attribute("location", String, "The URL to redirect the user to for Google authentication")
			Attribute("state_cookie", String, "Short-lived CSRF state value set as a cookie for sanity-checking the callback")
		})

		HTTP(func() {
			GET("/admin/auth.login")
			Param("return_to")
			Param("prompt")

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String)
				Cookie(fmt.Sprintf("state_cookie:%s", constants.AdminLoginStateCookie), String, "CSRF state cookie for sanity-checking the callback")
				CookieMaxAge(600)
				CookieHTTPOnly()
				CookieSameSite(CookieSameSiteLax)
				CookieSecure()
			})
		})
	})

	Method("callback", func() {
		NoSecurity()

		Payload(func() {
			Required("state_param")
			Attribute("code", String, "The authorization code returned by the provider on success")
			Attribute("state_param", String, "The state parameter returned, which should match the one generated in the login step")
			Attribute("state_cookie", String, "The state cookie value for CSRF sanity checking against the state parameter")
			Attribute("error", String, "OAuth error code returned by the provider (e.g. login_required for prompt=none failures)")
			Attribute("error_description", String, "Human-readable OAuth error description")
		})

		Result(func() {
			Required("location", "session_id")
			Attribute("location", String, "The URL to redirect the client to after processing the callback")
			Attribute("session_id", String, "The admin session cookie value")
		})

		HTTP(func() {
			GET("/admin/auth.callback")
			Param("code")
			Param("state_param:state")
			Param("error")
			Param("error_description")
			Cookie(fmt.Sprintf("state_cookie:%s", constants.AdminLoginStateCookie), String)

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String)
				Cookie(fmt.Sprintf("session_id:%s", constants.AdminSessionCookie), String, "Admin session cookie")
				CookieHTTPOnly()
				CookieSameSite(CookieSameSiteLax)
				CookieSecure()
			})
		})
	})

	Method("logout", func() {
		NoSecurity()

		Payload(func() {
			Attribute("session_id", String, "The session cookie value to clear for logging out")
		})

		HTTP(func() {
			POST("/admin/auth.logout")
			Cookie(fmt.Sprintf("session_id:%s", constants.AdminSessionCookie), String)

			Response(StatusNoContent)
		})
	})

	Method("getSession", func() {
		Payload(func() { security.AdminAuthPayload() })
		Result(AdminSession)
		HTTP(func() { GET("/admin/session.get"); Response(StatusOK) })
		Meta("openapi:operationId", "adminGetSession")
	})

	Method("getOrganizationFeatures", func() {
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String)
			Required("organization_id")
		})
		Result(shared.ProductFeatures)
		HTTP(func() { GET("/admin/organization.features"); Param("organization_id"); Response(StatusOK) })
		Meta("openapi:operationId", "adminGetOrganizationFeatures")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminOrganizationFeatures"}`)
	})

	Method("setOrganizationFeature", func() {
		Payload(func() {
			Meta("openapi:typename", "SetOrganizationFeatureRequestBody")
			security.AdminAuthPayload()
			Attribute("organization_id", String)
			Attribute("feature_name", shared.ProductFeatureName)
			Attribute("enabled", Boolean)
			Required("organization_id", "feature_name", "enabled")
		})
		Result(shared.ProductFeatures)
		HTTP(func() { POST("/admin/organization.features"); Response(StatusOK) })
		Meta("openapi:operationId", "adminSetOrganizationFeature")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"SetAdminOrganizationFeature"}`)
	})

	Method("getOrganizationChatAnalysisSettings", func() {
		Payload(func() { security.AdminAuthPayload(); Attribute("organization_id", String); Required("organization_id") })
		Result(AdminChatAnalysisSettings)
		HTTP(func() { GET("/admin/organization.chatAnalysisSettings"); Param("organization_id"); Response(StatusOK) })
		Meta("openapi:operationId", "adminGetOrganizationChatAnalysisSettings")
	})

	Method("setOrganizationChatAnalysisSettings", func() {
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String)
			Attribute("judge", String, func() { Enum("work_units", "business_memory") })
			Attribute("enabled", Boolean)
			Attribute("daily_cap", Int, func() { Minimum(0); Maximum(10000) })
			Required("organization_id", "judge", "enabled", "daily_cap")
		})
		Result(AdminChatAnalysisSettings)
		HTTP(func() { POST("/admin/organization.chatAnalysisSettings"); Response(StatusOK) })
		Meta("openapi:operationId", "adminSetOrganizationChatAnalysisSettings")
	})

	Method("triggerOrganizationChatAnalysis", func() {
		Payload(func() {
			Meta("openapi:typename", "TriggerOrganizationChatAnalysisRequestBody")
			security.AdminAuthPayload()
			Attribute("organization_id", String)
			Required("organization_id")
		})
		Result(AdminChatAnalysisTriggerResult)
		HTTP(func() { POST("/admin/organization.chatAnalysisTrigger"); Response(StatusOK) })
		Meta("openapi:operationId", "adminTriggerOrganizationChatAnalysis")
	})

	Method("openOrganizationInDashboard", func() {
		Payload(func() { security.AdminAuthPayload(); Attribute("organization_id", String); Required("organization_id") })
		Result(AdminDashboardRedirect)
		HTTP(func() {
			POST("/admin/organization.open-dashboard")
			Param("organization_id")
			Response(StatusSeeOther, func() { Header("location:Location"); Header("cache_control:Cache-Control") })
		})
		Meta("openapi:operationId", "adminOpenOrganizationInDashboard")
	})

	Method("getProject", func() {
		Description("Returns full admin details for a project by id or slug, including aggregated counts of child resources.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("id_or_slug")

			Attribute("id_or_slug", String, "Project ID or slug.")
			Attribute("organization_id_or_slug", String, "Organization the project must belong to, by id or slug. A project outside it is reported as not found. Optional, because the global project lookup has no organization to scope by.")
		})

		Result(AdminProjectDetail)

		HTTP(func() {
			GET("/admin/project.get")

			Param("id_or_slug")
			Param("organization_id_or_slug")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetProject")
	})

	Method("updateOrganization", func() {
		Description("Updates admin-managed fields on an organization. At least one of account_type or whitelisted must be supplied.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("id")

			Attribute("id", String, "Organization ID.")
			Attribute("account_type", String, "New gram_account_type (free, pro, payg, or enterprise).", func() {
				Enum(accountTypes...)
			})
			Attribute("whitelisted", Boolean, "New whitelisted flag.")
		})

		Result(AdminOrganization)

		HTTP(func() {
			POST("/admin/organization.update")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminUpdateOrganization")
	})

	Method("bulkUpdateAccountType", func() {
		Description("Sets one account type on many organizations in a single statement. An ID that matches no organization is reported back rather than failing the batch, so a stale ID costs the operator that row and not the whole call.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("ids", "account_type")

			Attribute("ids", ArrayOf(String, func() {
				MinLength(1)
			}), "Organization IDs to update.", func() {
				MinLength(1)
				MaxLength(constants.MaxBulkAccountTypeIDs)
			})
			Attribute("account_type", String, "New gram_account_type for every listed organization.", func() {
				Enum(accountTypes...)
			})
		})

		Result(AdminBulkUpdateAccountTypeResult)

		HTTP(func() {
			POST("/admin/organizations.bulkUpdateAccountType")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminBulkUpdateAccountType")
	})

	Method("disableOrganization", func() {
		Description("Disables an organization, recording the moment of the action in disabled_at. Idempotent: disabling an already-disabled organization keeps the original timestamp.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("id")

			// Disable and enable take structurally identical payloads, and Goa's
			// OpenAPI emitter deduplicates request bodies by shape, so without an
			// explicit typename both endpoints publish the same schema name.
			Meta("openapi:typename", "DisableOrganizationRequestBody")

			Attribute("id", String, "Organization ID.", func() {
				MinLength(1)
			})
		})

		Result(AdminOrganization)

		HTTP(func() {
			POST("/admin/organization.disable")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminDisableOrganization")
	})

	Method("enableOrganization", func() {
		Description("Re-enables a disabled organization by clearing disabled_at. Idempotent: an organization that is already active is unaffected.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("id")

			// See disableOrganization for why this needs an explicit typename.
			Meta("openapi:typename", "EnableOrganizationRequestBody")

			Attribute("id", String, "Organization ID.", func() {
				MinLength(1)
			})
		})

		Result(AdminOrganization)

		HTTP(func() {
			POST("/admin/organization.enable")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminEnableOrganization")
	})

	Method("getOrganization", func() {
		Description("Returns full admin details for a single organization by id or slug.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("id_or_slug")

			Attribute("id_or_slug", String, "Organization ID or slug.")
		})

		Result(AdminOrganization)

		HTTP(func() {
			GET("/admin/organization.get")

			Param("id_or_slug")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetOrganization")
	})

	Method("listOrganizationMembers", func() {
		Description("Lists members of an organization (admin view, no auth scoping).")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "Organization ID.")
		})

		Result(AdminListOrganizationMembersResult)

		HTTP(func() {
			GET("/admin/organization.members")

			Param("organization_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminListOrganizationMembers")
	})

	Method("listOrganizationProjects", func() {
		Description("Lists projects belonging to an organization (admin view, no auth scoping).")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "Organization ID.")
		})

		Result(AdminListOrganizationProjectsResult)

		HTTP(func() {
			GET("/admin/organization.projects")

			Param("organization_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminListOrganizationProjects")
	})

	Method("listOrganizationActivity", func() {
		Description("Lists activity belonging to an organization for admin operators.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "Organization ID.")
			Attribute("cursor", String, "Cursor for paginating through organization activity.")
		})

		Result(AdminListOrganizationActivityResult)

		HTTP(func() {
			GET("/admin/organization.activity")

			Param("organization_id")
			Param("cursor")
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "adminListOrganizationActivity")
	})

	Method("listOrganizations", func() {
		Description("Lists organizations for platform admin operations with optional search and filters. Defaults to created_at descending, with id ascending to break ties.")

		Payload(func() {
			security.AdminAuthPayload()

			Attribute("q", String, "Search term, trimmed of surrounding whitespace. Matches name and slug as a case-insensitive substring, with % and _ taken literally, and matches organization id and WorkOS id exactly, ignoring case. All filters apply even to exact ID matches.")
			Attribute("account_type", String, "Filter by a single gram_account_type (e.g. free, pro, payg, enterprise). Superseded by account_types, which it joins as one more member of the same set.")
			Attribute("account_types", ArrayOf(String), "Match any of these gram_account_type values. Empty matches every account type. A value no organization carries matches nothing rather than failing the request.")
			Attribute("trial_states", ArrayOf(String), "Match any of running, ending_soon, expired, demoted, converted or none. Empty matches every trial state. An unrecognised value matches nothing rather than failing the request.")
			Attribute("disabled_status", String, "Organization status: all (default), active (disabled_at IS NULL), or disabled (disabled_at IS NOT NULL). Applies even to exact ID matches.", func() { Enum("all", "active", "disabled") })
			Attribute("min_members", Int64, "Inclusive minimum active member count, from 0 through 9223372036854775807. The generated TypeScript SDK accepts bigint. The handwritten admin client accepts safe integers or decimal strings; use decimal strings above Number.MAX_SAFE_INTEGER.", func() { Minimum(0) })
			Attribute("max_members", Int64, "Inclusive maximum active member count, from 0 through 9223372036854775807. Must be at least min_members. The generated TypeScript SDK accepts bigint. The handwritten admin client accepts safe integers or decimal strings; use decimal strings above Number.MAX_SAFE_INTEGER.", func() { Minimum(0) })
			Attribute("created_from", String, "Inclusive creation date in strict YYYY-MM-DD UTC calendar format. Each date bound is optional; must not be after created_to.")
			Attribute("created_to", String, "Inclusive creation date in strict YYYY-MM-DD UTC calendar format. Includes the entire UTC day, implemented as an exclusive bound at the following midnight.")
			Attribute("cursor", String, "Pagination cursor: id of the last item from the previous page in created_at descending, id ascending order. The anchor is resolved regardless of filters; a deleted or unknown id returns an empty page. Ignored when sort or page is supplied.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			Attribute("sort", String, "Column to sort by: name, slug, account_type, member_count, created_at, disabled_at or trial_ends_at. Omitted or unknown values use created_at descending. Ties always sort by id ascending. Supplying it selects offset paging.")
			Attribute("direction", String, "Sort direction, asc or desc, applied to the column named by sort. Any other value sorts ascending. Ignored when sort is omitted or unknown, preserving the newest-first default. On its own it does not select offset paging.")
			Attribute("page", Int, "1-based page number for offset paging (default 1). Supplying it selects offset paging.")
		})

		Result(AdminListOrganizationsResult)

		HTTP(func() {
			GET("/admin/organizations.list")

			Param("q")
			Param("account_type")
			Param("account_types")
			Param("trial_states")
			Param("disabled_status")
			Param("min_members")
			Param("max_members")
			Param("created_from")
			Param("created_to")
			Param("cursor")
			Param("limit")
			Param("sort")
			Param("direction")
			Param("page")
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "adminListOrganizations")
	})

	// Appended rather than inserted mid-block, and that is a diff-size choice
	// and nothing more. Generated type names come from the method name, so
	// position cannot rename anything; appending only keeps goa from reordering
	// the declarations below it. Measured on this change, appending cost 19
	// deleted lines under server/gen where the disable and enable slice's
	// mid-block insert churned 3777 lines of types.go.
	//
	// The one positional effect that is real is the one disableOrganization
	// above documents: the OpenAPI emitter deduplicates structurally identical
	// request bodies and names the shared schema after whichever method it met
	// first. rearmTrial below shares this {id, days} shape and carries the
	// explicit typename, so this one keeps the generated name.
	Method("extendTrial", func() {
		Description("Extends a running enterprise trial by adding days to its current end date. Only a running trial can be extended: one that has converted, has been demoted, or has already expired is rejected rather than re-armed.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("id", "days")

			Attribute("id", String, "Organization ID.", func() {
				MinLength(1)
			})
			Attribute("days", Int, "Number of days to add to the trial's current end date.", func() {
				Minimum(constants.MinTrialExtensionDays)
				Maximum(constants.MaxTrialExtensionDays)
			})
		})

		Result(AdminOrganization)

		HTTP(func() {
			POST("/admin/trial.extend")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminExtendTrial")
	})

	// Appended rather than inserted, for the diff-size reason the note above
	// extendTrial gives: position cannot rename a generated type, but inserting
	// mid-block makes goa reorder every declaration below it. A new method goes
	// after this one.
	Method("createOrganization", func() {
		Description("Creates an organization in WorkOS and in Gram, so an operator does not have to leave the admin app for the WorkOS dashboard. The organization starts with no members, is not whitelisted, and gets no trial. Idempotent against the WorkOS organization webhook: the Gram ID is derived from the WorkOS ID, so both writers converge on one row.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("url", "ownership_confirmed")
			Meta("openapi:typename", "CreateOrganizationRequestBody")
			Attribute("url", String, "Company HTTP(S) URL or bare hostname. The exact normalized hostname becomes the name and verified email domain.", func() {
				MinLength(1)
			})
			Attribute("ownership_confirmed", Boolean, "The operator confirms that domain ownership was established outside this form.")
		})

		Result(AdminOrganization)

		HTTP(func() {
			POST("/admin/organization.create")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminCreateOrganization")
	})

	// Appended, not inserted: see the note above extendTrial. New methods go last.
	Method("rearmTrial", func() {
		Description("Puts a demoted enterprise trial back on: restores the organization's account type and whitelist flag, revives its model provider keys, and gives the trial a fresh run of the given length counted from now. Only a demoted trial can be re-armed; one that has converted or is already running is rejected.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("id", "days")

			// Shares extendTrial's body shape, and the OpenAPI emitter names a
			// deduplicated schema after the first method it met.
			Meta("openapi:typename", "RearmTrialRequestBody")

			Attribute("id", String, "Organization ID.", func() {
				MinLength(1)
			})
			Attribute("days", Int, "Number of days the re-armed trial runs for, counted from now.", func() {
				Minimum(constants.MinTrialRearmDays)
				Maximum(constants.MaxTrialRearmDays)
			})
		})

		Result(AdminOrganization)

		HTTP(func() {
			POST("/admin/trial.rearm")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminRearmTrial")
	})

	Method("getOrganizationStats", func() {
		Description("Returns platform-wide organization counts for the strip above the organizations list. Every figure counts the whole platform: none of them narrows to the caller's list filters, so the strip does not move when an operator filters.")

		Payload(func() {
			security.AdminAuthPayload()
		})

		Result(AdminOrganizationStats)

		HTTP(func() {
			GET("/admin/organizations.stats")

			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminGetOrganizationStats")
	})

	Method("getInferenceKeys", func() {
		Description("Returns the configured state of every materialized platform-managed OpenRouter key for an organization.")
		Payload(func() { security.AdminAuthPayload(); Required("organization_id"); Attribute("organization_id", String) })
		Result(ArrayOf(AdminInferenceKey))
		HTTP(func() { GET("/admin/organization.inferenceKeys"); Param("organization_id"); Response(StatusOK) })
		Meta("openapi:operationId", "adminGetInferenceKeys")
	})

	Method("setInferenceKeyMonthlyLimit", func() {
		Description("Sets the monthly limit for one materialized platform-managed OpenRouter key.")
		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id", "key_type", "monthly_credits")
			Attribute("organization_id", String)
			Attribute("key_type", String, func() { Enum("chat", "internal") })
			Attribute("monthly_credits", Int, func() {
				Minimum(constants.MinimumPaygSpendCapUSD)
				Maximum(constants.MaximumPaygSpendCapUSD)
			})
			Meta("openapi:typename", "SetInferenceKeyMonthlyLimitRequestBody")
		})
		Result(AdminInferenceKeyLimit)
		HTTP(func() { POST("/admin/organization.setInferenceKeyMonthlyLimit"); Response(StatusOK) })
		Meta("openapi:operationId", "adminSetInferenceKeyMonthlyLimit")
	})

	Method("getInferenceSpendHistory", func() {
		Description("Returns up to twelve complete UTC calendar months of recorded inference spend for an organization.")
		Payload(func() { security.AdminAuthPayload(); Required("organization_id"); Attribute("organization_id", String) })
		Result(ArrayOf(AdminInferenceSpendMonth))
		HTTP(func() { GET("/admin/organization.inferenceSpendHistory"); Param("organization_id"); Response(StatusOK) })
		Meta("openapi:operationId", "adminGetInferenceSpendHistory")
	})

	Method("getPaygBillingSummary", func() {
		Description("Returns current PAYG usage and estimated cost for an organization.")
		Payload(func() { security.AdminAuthPayload(); Required("organization_id"); Attribute("organization_id", String) })
		Result(AdminPaygBillingSummary)
		declareUnavailable()
		HTTP(func() {
			GET("/admin/organization.paygBillingSummary")
			Param("organization_id")
			Response(StatusOK)
			declareUnavailableResponse()
		})
		Meta("openapi:operationId", "adminGetPaygBillingSummary")
	})

	Method("getStripeCustomer", func() {
		Description("Returns Stripe customer details for confirmation before assigning the customer to an organization.")
		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id", "stripe_customer_id")
			Attribute("organization_id", String)
			Attribute("stripe_customer_id", String, func() {
				Pattern(`^cus_[A-Za-z0-9_]+$`)
				MaxLength(255)
			})
		})
		Result(AdminStripeCustomer)
		declareUnavailable()
		HTTP(func() {
			GET("/admin/organization.stripeCustomer")
			Param("organization_id")
			Param("stripe_customer_id")
			Response(StatusOK)
			declareUnavailableResponse()
		})
		Meta("openapi:operationId", "adminGetStripeCustomer")
	})

	Method("setStripeCustomer", func() {
		Description("Sets an organization's Stripe customer ID when it has no existing Stripe customer or subscription.")
		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id", "stripe_customer_id")
			Attribute("organization_id", String)
			Attribute("stripe_customer_id", String, func() {
				Pattern(`^cus_[A-Za-z0-9_]+$`)
				MaxLength(255)
			})
			Meta("openapi:typename", "SetStripeCustomerRequestBody")
		})
		Result(AdminOrganization)
		declareUnavailable()
		HTTP(func() {
			POST("/admin/organization.setStripeCustomer")
			Response(StatusOK)
			declareUnavailableResponse()
		})
		Meta("openapi:operationId", "adminSetStripeCustomer")
	})

	Method("getStripeSubscription", func() {
		Description("Returns the live Stripe subscription and payment state for an organization.")
		Payload(func() { security.AdminAuthPayload(); Required("organization_id"); Attribute("organization_id", String) })
		Result(AdminStripeSubscription)
		declareUnavailable()
		HTTP(func() {
			GET("/admin/organization.stripeSubscription")
			Param("organization_id")
			Response(StatusOK)
			declareUnavailableResponse()
		})
		Meta("openapi:operationId", "adminGetStripeSubscription")
	})

	Method("cancelStripeSubscription", func() {
		Description("Schedules an organization's PAYG subscription to cancel at period end.")
		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")
			Attribute("organization_id", String)
			Meta("openapi:typename", "CancelStripeSubscriptionRequestBody")
		})
		Result(AdminStripeSubscription)
		declareUnavailable()
		HTTP(func() {
			POST("/admin/organization.cancelStripeSubscription")
			Response(StatusOK)
			declareUnavailableResponse()
		})
		Meta("openapi:operationId", "adminCancelStripeSubscription")
	})

	Method("resumeStripeSubscription", func() {
		Description("Removes a scheduled period-end cancellation from an organization's PAYG subscription.")
		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")
			Attribute("organization_id", String)
			Meta("openapi:typename", "ResumeStripeSubscriptionRequestBody")
		})
		Result(AdminStripeSubscription)
		declareUnavailable()
		HTTP(func() {
			POST("/admin/organization.resumeStripeSubscription")
			Response(StatusOK)
			declareUnavailableResponse()
		})
		Meta("openapi:operationId", "adminResumeStripeSubscription")
	})

	Method("markEnterpriseTrialConverted", func() {
		Description("Records that an organization's enterprise trial converted to a signed contract.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("id")
			Meta("openapi:typename", "MarkEnterpriseTrialConvertedRequestBody")

			Attribute("id", String, "Organization ID.", func() {
				MinLength(1)
			})
		})

		Result(MarkEnterpriseTrialConvertedResult)

		HTTP(func() {
			POST("/admin/trial.convert")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminMarkEnterpriseTrialConverted")
	})
	remoteSessionIssuerMethods()
	platformAssetMethods()

	// Appended, not inserted: see the note above extendTrial. New methods go last.
	Method("startTrial", func() {
		Description("Starts a new enterprise trial for an organization that has never trialled, or restarts one that has expired without converting or being demoted. Sets the account type, whitelist flag, trial entitlements and a fresh runway counted from now. A running, demoted or converted trial is rejected: those are extend, re-arm and a contract.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("id", "days")

			// Shares extendTrial's body shape, and the OpenAPI emitter names a
			// deduplicated schema after the first method it met.
			Meta("openapi:typename", "StartTrialRequestBody")

			Attribute("id", String, "Organization ID.", func() {
				MinLength(1)
			})
			Attribute("days", Int, "Number of days the trial runs for, counted from now.", func() {
				Minimum(constants.MinTrialStartDays)
				Maximum(constants.MaxTrialStartDays)
			})
		})

		Result(AdminOrganization)

		HTTP(func() {
			POST("/admin/trial.start")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminStartTrial")
	})

	Method("changeTrialEndDate", func() {
		Description("Sets a running trial's end date to a future instant, shortening or extending it without restarting the trial.")
		Payload(func() {
			security.AdminAuthPayload()
			Required("id", "ends_at")
			Attribute("id", String, "Organization ID.", func() { MinLength(1) })
			Attribute("ends_at", String, "New trial end date in UTC.", func() { Format(FormatDateTime) })
		})
		Result(AdminOrganization)
		HTTP(func() {
			POST("/admin/trial.changeEndDate")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminChangeTrialEndDate")
	})

	Method("getMeterUsage", func() {
		Description("Returns totals-only ordinary meter usage for an organization over a bounded UTC-day window.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String, "Organization ID or canonical slug.")
			Attribute("family", String, func() {
				Enum("agent_session_storage", "mcp_bandwidth", "risk_content_scans")
			})
			Attribute("from", String, "Inclusive UTC midnight reporting boundary. Must be paired with to.", func() {
				Format(FormatDateTime)
			})
			Attribute("to", String, "Exclusive UTC midnight reporting boundary. Must be paired with from and no later than three calendar months after from.", func() {
				Format(FormatDateTime)
			})
			Required("organization_id", "family")
		})
		Result(AdminMeterUsageResponse)
		declareUnavailable()
		HTTP(func() {
			GET("/admin/organizations.getMeterUsage")
			Param("organization_id")
			Param("family")
			Param("from")
			Param("to")
			Response(StatusOK)
			declareUnavailableResponse()
		})
		Meta("openapi:operationId", "adminGetMeterUsage")
	})

	Method("getSpendBreakdown", func() {
		Description("Returns exact current PAYG list-price estimates for an organization's three metered products over a maximum of three calendar months. Available for every organization regardless of account type or subscription state.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String, "Organization ID or canonical slug.")
			Attribute("from", String, "Inclusive UTC midnight reporting boundary. Must be paired with to.", func() {
				Format(FormatDateTime)
			})
			Attribute("to", String, "Exclusive UTC midnight reporting boundary. Must be paired with from and no later than three calendar months after from, clamped to the target month's last day.", func() {
				Format(FormatDateTime)
			})
			Required("organization_id")
		})
		Result(AdminSpendBreakdownResponse)
		declareUnavailable()
		HTTP(func() {
			GET("/admin/organization.spendBreakdown")
			Param("organization_id")
			Param("from")
			Param("to")
			Response(StatusOK)
			declareUnavailableResponse()
		})
		Meta("openapi:operationId", "adminGetSpendBreakdown")
		Meta("openapi:extension:x-speakeasy-name-override", "getSpendBreakdown")
	})

	supportMatrixMethods()

})
