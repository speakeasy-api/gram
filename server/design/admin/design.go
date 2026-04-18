package admin

import (
	"fmt"

	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

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
			Required("location")
			Attribute("location", String, "The URL to redirect the user to for Google authentication")
			// Attribute("state_cookie", String, "The state cookie value to set for sanity checking in the callback")
		})

		HTTP(func() {
			GET("/admin/auth.login")
			Param("return_to")

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String)
				// Cookie("state_cookie:gram_admin_login_state", String, "The state cookie value to set for sanity checking in the callback")
				// CookieMaxAge(600)
				// CookieHTTPOnly()
				// CookieSameSite(CookieSameSiteLax)
				// CookieSecure()
			})
		})
	})

	Method("callback", func() {
		NoSecurity()

		Payload(func() {
			Required("code", "state_param")
			Attribute("code", String, "The authorization code returned")
			Attribute("state_param", String, "The state parameter returned, which should match the one generated in the login step")
			// Attribute("state_cookie", String, "The state cookie value, which should match the state parameter for sanity checking")
		})

		Result(func() {
			Required("location", "session_id")
			Attribute("location", String, "The URL to redirect the client to after processing the callback")
			Attribute("session_id", String, "The session cookie value to set for the admin session")
		})

		HTTP(func() {
			GET("/admin/auth.callback")
			Param("code")
			Param("state_param:state")
			// Cookie("state_cookie:gram_admin_login_state", String)

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String)
				Header("session_id:Gram-Admin-Session", String, "The session cookie value to set for the admin session")
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
})
