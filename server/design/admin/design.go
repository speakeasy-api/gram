package admin

import (
	"fmt"

	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

var AdminOrganization = Type("AdminOrganization", func() {
	Description("Organization details surfaced to admin operators.")
	Required("id", "name", "slug", "account_type", "whitelisted", "member_count", "created_at", "updated_at")

	Attribute("id", String, "The ID of the organization")
	Attribute("name", String, "The name of the organization")
	Attribute("slug", String, "The slug of the organization")
	Attribute("account_type", String, "Gram account type (e.g. free, pro, enterprise).")
	Attribute("workos_id", String, "WorkOS organization ID, if linked.")
	Attribute("whitelisted", Boolean, "Whether the organization is whitelisted for full access.")
	Attribute("disabled_at", String, func() {
		Description("The time at which the organization was disabled, if any.")
		Format(FormatDateTime)
	})
	Attribute("free_trial_started_at", String, func() {
		Description("The time at which the free trial started.")
		Format(FormatDateTime)
	})
	Attribute("free_trial_ends_at", String, func() {
		Description("The time at which the free trial ends.")
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
	Required("id", "name", "slug", "created_at", "updated_at")

	Attribute("id", String, "The ID of the project")
	Attribute("name", String, "The name of the project")
	Attribute("slug", String, "The slug of the project")
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

var AdminListOrganizationProjectsResult = Type("AdminListOrganizationProjectsResult", func() {
	Required("projects")

	Attribute("projects", ArrayOf(AdminProject), "The projects belonging to the organization.")
})

var AdminListOrganizationsResult = Type("AdminListOrganizationsResult", func() {
	Required("organizations")

	Attribute("organizations", ArrayOf(AdminOrganization), "The page of organizations.")
	Attribute("next_cursor", String, "Cursor for the next page; empty when exhausted.")
})

var _ = Service("admin", func() {
	Description("Operations supporting admin tasks, protected by Google workspace auth.")
	Security(security.AdminAuth)
	shared.DeclareErrorResponses()

	Method("login", func() {
		NoSecurity()

		Payload(func() {
			Attribute("return_to", String, "Optional URL to return the user to after login. Must be relative and under the admin service's base URL.")
		})

		Result(func() {
			Required("location", "state_cookie")
			Attribute("location", String, "The URL to redirect the user to for Google authentication")
			Attribute("state_cookie", String, "Short-lived CSRF state value set as a cookie for sanity-checking the callback")
		})

		HTTP(func() {
			GET("/admin/auth.login")
			Param("return_to")

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
			Required("code", "state_param")
			Attribute("code", String, "The authorization code returned")
			Attribute("state_param", String, "The state parameter returned, which should match the one generated in the login step")
			Attribute("state_cookie", String, "The state cookie value for CSRF sanity checking against the state parameter")
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
		})

		Result(AdminProjectDetail)

		HTTP(func() {
			GET("/admin/project.get")

			Param("id_or_slug")
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
			Attribute("account_type", String, "New gram_account_type (e.g. free, pro, enterprise).")
			Attribute("whitelisted", Boolean, "New whitelisted flag.")
		})

		Result(AdminOrganization)

		HTTP(func() {
			POST("/admin/organization.update")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminUpdateOrganization")
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

	Method("listOrganizations", func() {
		Description("Lists organizations for admin operations with optional search and filters.")

		Payload(func() {
			security.AdminAuthPayload()

			Attribute("q", String, "Search term applied to name and slug (case-insensitive substring).")
			Attribute("account_type", String, "Filter by gram_account_type (e.g. free, pro, enterprise).")
			Attribute("include_disabled", Boolean, "Include organizations with disabled_at set. Defaults to false.")
			Attribute("cursor", String, "Pagination cursor: id of the last item from the previous page.")
			Attribute("limit", Int, "Page size (default 50, max 100).")
		})

		Result(AdminListOrganizationsResult)

		HTTP(func() {
			GET("/admin/organizations.list")

			Param("q")
			Param("account_type")
			Param("include_disabled")
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "adminListOrganizations")
	})
})
