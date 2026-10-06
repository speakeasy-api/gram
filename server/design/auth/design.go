package auth

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var Trial = Type("Trial", func() {
	Attribute("started_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("ends_at", String, func() {
		Format(FormatDateTime)
	})
	Required("started_at", "ends_at")
})

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
			Attribute("org_name", String, "Optional organization name. When set, the organization is created for a new user during the auth callback.")
			Attribute("email", String, "Optional email address. Pre-fills the email field on the identity provider's sign-up screen. Never stored.")
			Attribute("support_handoff", String, "Opaque, one-time organization support handoff. Never included in OAuth state.")
		})

		Result(func() {
			Attribute("location", String, "The URL to redirect to after authentication")
			Required("location")
		})

		HTTP(func() {
			GET("/rpc/auth.login")
			Param("redirect")
			Param("org_name")
			// A top-level browser navigation reaches this endpoint and carries
			// no custom headers, so `email` has to be a query param. The
			// request logging middleware redacts it from logged URLs.
			Param("email")
			Param("support_handoff")

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

	Method("enterDemo", func() {
		Description("Switches the current session into the shared read-only demo organization.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(func() {
			Attribute("session_token", String, "The authentication session")
			Attribute("session_cookie", String, "The authentication session")
			Required("session_token", "session_cookie")
		})

		HTTP(func() {
			POST("/rpc/auth.enterDemo")
			security.SessionHeader()
			Response(StatusOK, func() {
				security.WriteSessionCookie()
				security.SessionHeader()
			})
		})

		Meta("openapi:operationId", "enterDemo")
		Meta("openapi:extension:x-speakeasy-name-override", "enterDemo")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "EnterDemo"}`)
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
			Attribute("impersonator_email", String, "The WorkOS Dashboard operator who initiated this impersonation session. Empty for ordinary authentication.")
			Attribute("organization_override", Boolean, "Whether this is a validated, time-bounded organization support session.")
			Attribute("organization_override_expires_at", String, func() {
				Description("Fixed expiration of the organization support session.")
				Format(FormatDateTime)
			})
			Attribute("active_organization_id", String)
			Attribute("active_organization_dashboard_url", String, func() {
				Description("Dashboard base URL of the platform host the active organization lives on. Set only for an ordinary session whose request arrived on a different platform host; the dashboard moves there.")
				Format(FormatURI)
			})
			Attribute("gram_account_type", String)
			Attribute("has_active_subscription", Boolean, "Whether the organization has an active billing subscription")
			Attribute("whitelisted", Boolean, "Whether the organization is whitelisted to access the platform")
			Attribute("trial", Trial, func() {
				Meta("struct:tag:json", "trial")
			})
			Attribute("organizations", ArrayOf(shared.OrganizationEntry))

			Attribute("session_token", String, "The authentication session")
			Attribute("session_cookie", String, "The authentication session")

			Required("user_id", "user_email", "is_admin", "organization_override", "active_organization_id", "organizations", "session_token", "session_cookie", "gram_account_type", "has_active_subscription", "whitelisted")
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

	Method("transferStart", func() {
		Description("Starts a cross-domain session transfer on the target platform host. Sets a short-lived cookie that binds the transfer to this browser and redirects to the source platform host's transferOut endpoint. Used to share session cookies seamlessly between platform hosts (e.g. app.getgram.ai and ai.speakeasy.com).")

		NoSecurity()

		Payload(func() {
			Attribute("source_host", String, "The platform host that holds the session to transfer (e.g. app.getgram.ai)")
			Attribute("redirect", String, "Optional URL path to redirect to after the transfer completes on this host")
			Required("source_host")
		})

		Result(func() {
			Attribute("location", String, "The URL to redirect to (the source host's transferOut endpoint)")
			Attribute("transfer_nonce_cookie", String, "The browser binding for this transfer")
			Required("location", "transfer_nonce_cookie")
		})

		HTTP(func() {
			GET("/rpc/auth.transferStart")
			Param("source_host")
			Param("redirect")

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String, func() {
				})
				security.WriteSessionTransferNonceCookie()
			})
		})

		Meta("openapi:operationId", "authTransferStart")
		Meta("openapi:extension:x-speakeasy-name-override", "transferStart")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})

	Method("transferOut", func() {
		Description("Continues a cross-domain session transfer on the source platform host. Stores a one-time transfer code server-side and redirects to the target platform host's transferIn endpoint.")

		Payload(func() {
			Attribute("target_host", String, "The target platform host to transfer the session to (e.g. ai.speakeasy.com)")
			Attribute("nonce", String, "The browser binding nonce from the target host's transferStart endpoint")
			Attribute("redirect", String, "Optional URL path to redirect to after the transfer completes on the target host")
			security.SessionPayload()
			Required("target_host", "nonce")
		})

		Result(func() {
			Attribute("location", String, "The URL to redirect to (the target host's transferIn endpoint with the transfer code)")
			Required("location")
		})

		HTTP(func() {
			GET("/rpc/auth.transferOut")
			Param("target_host")
			Param("nonce")
			Param("redirect")
			security.SessionHeader()

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String, func() {
				})
			})
		})

		Meta("openapi:operationId", "authTransferOut")
		Meta("openapi:extension:x-speakeasy-name-override", "transferOut")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})

	Method("transferIn", func() {
		Description("Completes a cross-domain session transfer. Redeems the transfer code, checks it against the browser binding cookie set by transferStart, and creates a new session cookie on this host. The code is one-time-use and expires after 60 seconds.")

		NoSecurity()

		Payload(func() {
			Attribute("token", String, "The opaque one-time transfer code from the source host's transferOut endpoint")
			Attribute("redirect", String, "Optional URL path to redirect to after the session is established")
			Required("token")
		})

		Result(func() {
			Attribute("location", String, "The URL to redirect to after the session is established")
			Attribute("session_token", String, "The new authentication session on this host")
			Attribute("session_cookie", String, "The new authentication session on this host")
			Required("location", "session_token", "session_cookie")
		})

		HTTP(func() {
			GET("/rpc/auth.transferIn")
			Param("token")
			Param("redirect")

			Response(StatusTemporaryRedirect, func() {
				Header("location:Location", String, func() {
				})
				security.WriteSessionCookie()
				security.SessionHeader()
			})
		})

		Meta("openapi:operationId", "authTransferIn")
		Meta("openapi:extension:x-speakeasy-name-override", "transferIn")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"disabled": true}`)
	})
})
