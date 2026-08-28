package admin

import (
	"fmt"

	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/auditlogs"
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

var AdminOrganization = Type("AdminOrganization", func() {
	Description("Organization details surfaced to admin operators.")
	Required("id", "name", "slug", "account_type", "whitelisted", "member_count", "created_at", "updated_at")

	Attribute("id", String, "The ID of the organization")
	Attribute("name", String, "The name of the organization")
	Attribute("slug", String, "The slug of the organization")
	Attribute("account_type", String, "Gram account type (e.g. free, pro, payg, enterprise).")
	Attribute("workos_id", String, "WorkOS organization ID, if linked.")
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
	Required("key_type", "credits_used", "monthly_credits", "disabled")
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

// Shared so the two write paths, and the service's own copy of the check,
// cannot drift into accepting different sets.
var accountTypes = conv.AnySlice(constants.AccountTypes)

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
		Description("Lists organizations for admin operations with optional search and filters.")

		Payload(func() {
			security.AdminAuthPayload()

			Attribute("q", String, "Search term, trimmed of surrounding whitespace. Matches name and slug as a case-insensitive substring, with % and _ taken literally, and matches organization id and WorkOS id exactly, ignoring case. An id match also returns an organization that disabled_states or include_disabled would otherwise hide; it still respects account_type, account_types, trial_states and cursor.")
			Attribute("account_type", String, "Filter by a single gram_account_type (e.g. free, pro, payg, enterprise). Superseded by account_types, which it joins as one more member of the same set.")
			Attribute("account_types", ArrayOf(String), "Match any of these gram_account_type values. Empty matches every account type. A value no organization carries matches nothing rather than failing the request.")
			Attribute("trial_states", ArrayOf(String), "Match any of running, ending_soon, expired, demoted, converted or none. Empty matches every trial state. An unrecognised value matches nothing rather than failing the request.")
			Attribute("disabled_states", ArrayOf(String), "Match any of active or disabled. Empty falls back to include_disabled. An unrecognised value matches nothing rather than failing the request.")
			Attribute("include_disabled", Boolean, "Include organizations with disabled_at set. Defaults to false. Superseded by disabled_states, which overrides it outright when supplied.")
			Attribute("cursor", String, "Pagination cursor: id of the last item from the previous page. Ignored when sort or page is supplied.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
			Attribute("sort", String, "Column to sort by: name, slug, account_type, member_count, created_at, disabled_at or trial_ends_at. Any other value sorts by id. Supplying it selects offset paging.")
			Attribute("direction", String, "Sort direction, asc or desc, applied to the column named by sort. Any other value sorts ascending. On its own it does nothing: without sort there is no column to reverse, so it neither reorders the results nor selects offset paging.")
			Attribute("page", Int, "1-based page number for offset paging (default 1). Supplying it selects offset paging.")
		})

		Result(AdminListOrganizationsResult)

		HTTP(func() {
			GET("/admin/organizations.list")

			Param("q")
			Param("account_type")
			Param("account_types")
			Param("trial_states")
			Param("disabled_states")
			Param("include_disabled")
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
			Required("name")

			// A body of one required string is structurally identical to several
			// others in this design, and Goa's OpenAPI emitter deduplicates
			// request bodies by shape, reusing whichever name it registered
			// first. MinLength makes this shape its own, and an explicit
			// typename stops a future identically-shaped body from taking it.
			Meta("openapi:typename", "CreateOrganizationRequestBody")

			// The length and character rules live in orgprovision.ValidateName,
			// which the handler runs and which the signup path runs too. Only
			// the emptiness floor is repeated here.
			Attribute("name", String, "Display name for the new organization.", func() {
				MinLength(1)
			})
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
		Description("Puts a demoted enterprise trial back on: restores the organization's account type and whitelist flag, removes only the trial-demotion block from its model provider keys, and gives the trial a fresh run of the given length counted from now. Other key protections remain in force. Retrying a committed re-arm reconciles key state without extending the trial again; a converted or independently running trial is rejected.")

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
		HTTP(func() { GET("/admin/organization.paygBillingSummary"); Param("organization_id"); Response(StatusOK) })
		Meta("openapi:operationId", "adminGetPaygBillingSummary")
	})

	Method("getStripeSubscription", func() {
		Description("Returns the live Stripe subscription and payment state for an organization.")
		Payload(func() { security.AdminAuthPayload(); Required("organization_id"); Attribute("organization_id", String) })
		Result(AdminStripeSubscription)
		HTTP(func() { GET("/admin/organization.stripeSubscription"); Param("organization_id"); Response(StatusOK) })
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
		HTTP(func() { POST("/admin/organization.cancelStripeSubscription"); Response(StatusOK) })
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
		HTTP(func() { POST("/admin/organization.resumeStripeSubscription"); Response(StatusOK) })
		Meta("openapi:operationId", "adminResumeStripeSubscription")
	})
})
