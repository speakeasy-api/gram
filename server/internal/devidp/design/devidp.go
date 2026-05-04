package design

import (
	. "goa.design/goa/v3/dsl"
)

// WorkosCurrentUser is the workos-mode payload returned from
// devIdp.getCurrentUser. The shape mirrors the live WorkOS users.get
// response (idp-design.md §6.2 / §7.2). Fields beyond `workos_sub` are
// best-effort — populated when the proxy round-trip succeeds, omitted
// otherwise.
var WorkosCurrentUser = Type("WorkosCurrentUser", func() {
	Attribute("workos_sub", String, "WorkOS user id (the `sub` stored in current_users).")
	Attribute("email", String, "Email address from the live WorkOS user record.")
	Attribute("first_name", String)
	Attribute("last_name", String)
	Attribute("profile_picture_url", String)
	Attribute("organization_id", String, "Default WorkOS organization id, when set.")

	Required("workos_sub")
})

// CurrentUser is the discriminated payload returned by devIdp.getCurrentUser.
// `mode` tells the consumer which mode was read. Local modes populate
// `user`; the workos mode populates `workos`.
var CurrentUser = Type("CurrentUser", func() {
	Attribute("mode", String, "Mode whose currentUser is being reported.", func() {
		Enum("local-speakeasy", "oauth2-1", "oauth2", "workos")
	})
	Attribute("user", User, "Local user record. Populated for local-speakeasy / oauth2-1 / oauth2.")
	Attribute("workos", WorkosCurrentUser, "Live WorkOS profile. Populated for workos mode only.")

	Required("mode")
})

var _ = Service("devIdp", func() {
	Description("Dev-only RPCs for the dev-idp itself. Per-mode currentUser get/set (idp-design.md §3, §6.2). Permanently unauthenticated.")

	Method("getCurrentUser", func() {
		Description("Read the per-mode currentUser. 404s when no row exists yet for that mode.")

		Payload(func() {
			Attribute("mode", String, "Which mode's currentUser to read.", func() {
				Enum("local-speakeasy", "oauth2-1", "oauth2", "workos")
			})
			Required("mode")
		})

		Result(CurrentUser)

		HTTP(func() {
			POST("/rpc/devIdp.getCurrentUser")
			Response(StatusOK)
		})
	})

	Method("setCurrentUser", func() {
		Description("UPSERT or clear the per-mode currentUser. Local modes accept `user_id` (a UUID into the local users table); workos mode accepts `workos_sub` (a literal WorkOS user id; not validated). Pass null (or omit both fields entirely) to clear the currentUser — the next identity-resolving request on the mode then falls through to the default-user bootstrap.")

		Payload(func() {
			Attribute("mode", String, "Which mode's currentUser to write.", func() {
				Enum("local-speakeasy", "oauth2-1", "oauth2", "workos")
			})
			Attribute("user_id", String, "Local user UUID. Required for non-workos modes.", func() {
				Format(FormatUUID)
			})
			Attribute("workos_sub", String, "WorkOS user id. Required for workos mode.")
			Required("mode")
		})

		Result(CurrentUser)

		HTTP(func() {
			POST("/rpc/devIdp.setCurrentUser")
			Response(StatusOK)
		})
	})
})
