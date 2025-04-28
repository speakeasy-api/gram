package auth

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/design/security"
	"github.com/speakeasy-api/gram/design/shared"
)

var _ = Service("auth", func() {
	Description("Managed auth for gram producers and dashboard.")
	Security(security.Session)

	Method("callback", func() {
		Description("Handles the authentication callback.")

		NoSecurity()

		Payload(func() {
			Attribute("id_token", String, "The id token for authentication from the speakeasy system")
			Required("id_token")
		})

		Result(func() {
			Attribute("location", String, "The URL to redirect to after authentication")
			Attribute("session_token", String, "The authentication session")
			Attribute("session_cookie", String, "The authentication session")
			Required("location", "session_token", "session_cookie")
		})

		HTTP(func() {
			GET("/rpc/auth.callback")
			Param("id_token")

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String, func() {
				})
				security.WriteSessionCookie()
				security.SessionHeader()
			})
		})

		Meta("openapi:operationId", "authCallback")
		Meta("openapi:extension:x-speakeasy-name-override", "callback")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})

	Method("login", func() {
		Description("Proxies to auth login through speakeasy oidc.")

		NoSecurity()

		Result(func() {
			Attribute("location", String, "The URL to redirect to after authentication")
			Required("location")
		})

		HTTP(func() {
			GET("/rpc/auth.login")

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String, func() {
				})
			})
		})

		Meta("openapi:operationId", "authLogin")
		Meta("openapi:extension:x-speakeasy-name-override", "login")
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

		Meta("openapi:operationId", "switchAuthScopes")
		Meta("openapi:extension:x-speakeasy-name-override", "switchScopes")
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
			POST("/rpc/auth.logout")
			security.SessionHeader()

			Response(StatusOK, func() {
				security.DeleteSessionCookie()
			})
		})

		Meta("openapi:operationId", "logout")
		Meta("openapi:extension:x-speakeasy-name-override", "logout")
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
			Attribute("organizations", ArrayOf(shared.OrganizationEntry))

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

		Meta("openapi:operationId", "sessionInfo")
		Meta("openapi:extension:x-speakeasy-name-override", "info")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SessionInfo"}`)
	})
})
