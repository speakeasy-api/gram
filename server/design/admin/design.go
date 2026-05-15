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
		Description("Returns the project with the given id or slug.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("id_or_slug")

			Attribute("id_or_slug")
		})

		Result(func() {
			Required("id", "slug")

			Attribute("id", String)
			Attribute("slug", String)
		})

		HTTP(func() {
			GET("/admin/project.get")

			Param("id_or_slug")
			Response(StatusOK)
		})
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
