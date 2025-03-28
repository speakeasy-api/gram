package auth

import (
	"github.com/speakeasy-api/gram/design/sessions"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("auth", func() {
	Description("Managed auth for gram producers and dashboard.")

	Method("auth callback", func() {
		Description("Handles the authentication callback.")

		Payload(func() {
			Attribute("shared_token", String, "The shared token for authentication from the speakeasy system")
			Required("shared_token")
		})

		Result(func() {
			Attribute("location", String, "The URL to redirect to after authentication")
			Attribute("gram_session", String, "The authentication session")
			Attribute("gram_session_cookie", String, "The authentication session")
			Required("location", "gram_session", "gram_session_cookie")
		})

		HTTP(func() {
			GET("/rpc/auth.callback")
			Param("shared_token")

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String, func() {
				})
				sessions.WriteSessionCookie()
				sessions.SessionHeader()
			})
		})
	})

	Method("auth switch scopes", func() {
		Description("Switches the authentication scope to a different organization.")

		Security(sessions.GramSession)

		Payload(func() {
			Attribute("organization_id", String, "The organization slug to switch scopes")
			Attribute("project_id", String, "The project id to switch scopes too")
			Extend(sessions.SessionPayload)
		})

		Result(func() {
			Attribute("gram_session", String, "The authentication session")
			Attribute("gram_session_cookie", String, "The authentication session")
			Required("gram_session", "gram_session_cookie")
		})

		HTTP(func() {
			POST("/rpc/auth.switch")
			Param("organization_id")
			Param("project_id")
			sessions.SessionHeader()
			Response(StatusOK, func() {
				sessions.WriteSessionCookie()
				sessions.SessionHeader()
			})
		})
	})

	Method("auth logout", func() {
		Description("Logs out the current user by clearing their session.")
		Security(sessions.GramSession)

		Payload(func() {
			Extend(sessions.SessionPayload)
		})

		Result(func() {
			Attribute("gram_session", String, "Empty string to clear the session")
			Required("gram_session")
		})

		HTTP(func() {
			GET("/rpc/auth.logout")
			sessions.SessionHeader()

			Response(StatusOK, func() {
				sessions.DeleteSessionCookie()
			})
		})
	})

	Method("auth info", func() {

		Description("Provides information about the current authentication status.")
		Security(sessions.GramSession)

		Payload(func() {
			Extend(sessions.SessionPayload)
		})

		Result(func() {
			Attribute("user_id", String)
			Attribute("user_email", String)
			Attribute("active_organization_id", String)
			Attribute("active_project_id", String)
			Attribute("organizations", ArrayOf("Organization")) // <-- here too
			Attribute("gram_session", String, "The authentication session")
			Attribute("gram_session_cookie", String, "The authentication session")
			Required("user_id", "user_email", "active_organization_id", "active_project_id", "organizations", "gram_session", "gram_session_cookie")
		})

		HTTP(func() {
			GET("/rpc/auth.info")
			sessions.SessionHeader()

			Response(StatusOK, func() {
				sessions.WriteSessionCookie()
				sessions.SessionHeader()
			})
		})
	})
})

var Project = Type("Project", func() {
	Attribute("project_id", String)
	Required("project_id")
})

var Organization = Type("Organization", func() {
	Attribute("organization_id", String)
	Attribute("organization_name", String)
	Attribute("organization_slug", String)
	Attribute("account_type", String)
	Attribute("projects", ArrayOf("Project"))
	Required("organization_id", "organization_name", "organization_slug", "account_type", "projects")
})
