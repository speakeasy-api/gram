package auth

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("auth", func() {
	Description("Managed auth for gram producers and dashboard.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("callback", func() {
		Description("Handles the authentication callback.")

		NoSecurity()

		Payload(func() {
			Attribute("code", String, "The auth code for authentication from the speakeasy system")
			Attribute("state", String, "The opaque state string optionally provided during initialization.")
			Required("code")
		})

		Result(func() {
			Attribute("location", String, "The URL to redirect to after authentication")
			Attribute("session_token", String, "The authentication session")
			Attribute("session_cookie", String, "The authentication session")
			Required("location", "session_token", "session_cookie")
		})

		HTTP(func() {
			GET("/rpc/auth.callback")
			Param("code")
			Param("state")

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

		Payload(func() {
			Attribute("redirect", String, "Optional URL to redirect to after successful authentication")
			Attribute("invite_token", String, "Optional invite token to process after authentication")
		})

		Result(func() {
			Attribute("location", String, "The URL to redirect to after authentication")
			Required("location")
		})

		HTTP(func() {
			GET("/rpc/auth.login")
			Param("redirect")
			Param("invite_token")

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

	Method("register", func() {
		Description("Register a new org for a user with their session information.")

		Payload(func() {
			security.SessionPayload()
			Attribute("org_name", String, "The name of the org to register")
			Required("org_name")
		})

		HTTP(func() {
			POST("/rpc/auth.register")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "register")
		Meta("openapi:extension:x-speakeasy-name-override", "register")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Register"}`)
	})

	Method("info", func() {
		Description("Provides information about the current authentication status.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(func() {
			Attribute("user_id", String)
			Attribute("user_email", String)
			Attribute("user_signature", String)
			Attribute("user_display_name", String)
			Attribute("user_photo_url", String)
			Attribute("is_admin", Boolean)
			Attribute("active_organization_id", String)
			Attribute("gram_account_type", String)
			Attribute("organizations", ArrayOf(shared.OrganizationEntry))

			Attribute("session_token", String, "The authentication session")
			Attribute("session_cookie", String, "The authentication session")

			Required("user_id", "user_email", "is_admin", "active_organization_id", "organizations", "session_token", "session_cookie", "gram_account_type")
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
