package auth

import (
	"github.com/speakeasy-api/gram/design/security"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("auth", func() {
	Description("Managed auth for gram producers and dashboard.")
	Security(security.Session)

	Method("callback", func() {
		Description("Handles the authentication callback.")

		NoSecurity()

		Payload(func() {
			Attribute("shared_token", String, "The shared token for authentication from the speakeasy system")
			Required("shared_token")
		})

		Result(func() {
			Attribute("location", String, "The URL to redirect to after authentication")
			Attribute("session_token", String, "The authentication session")
			Attribute("session_cookie", String, "The authentication session")
			Required("location", "session_token", "session_cookie")
		})

		HTTP(func() {
			GET("/rpc/auth.callback")
			Param("shared_token")

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String, func() {
				})
				security.WriteSessionCookie()
				security.SessionHeader()
			})
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})

	Method("switchScopes", func() {
		Description("Switches the authentication scope to a different organization.")

		Payload(func() {
			Attribute("organization_id", String, "The organization slug to switch scopes")
			Attribute("project_id", String, "The project id to switch scopes too")
			security.SessionPayload()
		})

		Result(func() {
			Attribute("session_token", String, "The authentication session")
			Attribute("session_cookie", String, "The authentication session")
			Required("session_token", "session_cookie")
		})

		HTTP(func() {
			POST("/rpc/auth.switchScopes")
			Param("organization_id")
			Param("project_id")
			security.SessionHeader()
			Response(StatusOK, func() {
				security.WriteSessionCookie()
				security.SessionHeader()
			})
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SwitchScopes"}`)
	})

	Method("logout", func() {
		Description("Logs out the current user by clearing their session.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(func() {
			Attribute("session_cookie", String, "Empty string to clear the session")
			Required("session_cookie")
		})

		HTTP(func() {
			GET("/rpc/auth.logout")
			security.SessionHeader()

			Response(StatusOK, func() {
				security.DeleteSessionCookie()
			})
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Logout"}`)
	})

	Method("info", func() {

		Description("Provides information about the current authentication status.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(func() {
			Attribute("user_id", String)
			Attribute("user_email", String)
			Attribute("active_organization_id", String)
			Attribute("organizations", ArrayOf("Organization")) // <-- here too

			// Response headers
			Attribute("session_token", String, "The authentication session")
			Attribute("session_cookie", String, "The authentication session")

			Required("user_id", "user_email", "active_organization_id", "organizations", "session_token", "session_cookie")
		})

		HTTP(func() {
			GET("/rpc/auth.info")
			security.SessionHeader()

			Response(StatusOK, func() {
				security.WriteSessionCookie()
				security.SessionHeader()
			})
		})

		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SessionInfo"}`)
	})
})

var Project = Type("Project", func() {
	Attribute("project_id", String)
	Attribute("project_name", String)
	Attribute("project_slug", String)
	Required("project_id", "project_name", "project_slug")
})

var Organization = Type("Organization", func() {
	Attribute("organization_id", String)
	Attribute("organization_name", String)
	Attribute("organization_slug", String)
	Attribute("account_type", String)
	Attribute("projects", ArrayOf("Project"))
	Required("organization_id", "organization_name", "organization_slug", "account_type", "projects")
})
