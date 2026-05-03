package design

import (
	. "goa.design/goa/v3/dsl"
)

var ListUsersResult = Type("ListUsersResult", func() {
	Attribute("items", ArrayOf(User), "Users on this page.")
	PaginationResult()
	Required("items")
})

var _ = Service("users", func() {
	Description("Dev-idp users CRUD. The local-mode currentUser pointers (mock-speakeasy / oauth2-1 / oauth2) reference rows in this table by id (idp-design.md §3, §5). Permanently unauthenticated.")

	Method("create", func() {
		Description("Create a user.")

		Payload(func() {
			Attribute("email", String, "Email address. Must be unique.")
			Attribute("display_name", String, "Display name.")
			Attribute("photo_url", String, "Optional photo URL.")
			Attribute("github_handle", String, "Optional GitHub handle.")
			Attribute("admin", Boolean, "Admin flag; defaults false.")
			Attribute("whitelisted", Boolean, "Whitelist flag; defaults true.")
			Required("email", "display_name")
		})

		Result(User)

		HTTP(func() {
			POST("/rpc/users.create")
			Response(StatusOK)
		})
	})

	Method("update", func() {
		Description("Patch a user.")

		Payload(func() {
			Attribute("id", String, "User UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("email", String)
			Attribute("display_name", String)
			Attribute("photo_url", String)
			Attribute("github_handle", String)
			Attribute("admin", Boolean)
			Attribute("whitelisted", Boolean)
			Required("id")
		})

		Result(User)

		HTTP(func() {
			POST("/rpc/users.update")
			Response(StatusOK)
		})
	})

	Method("list", func() {
		Description("List users. Optional `email` exact-match filter (idp-design.md §6.1).")

		Payload(func() {
			PaginationPayload()
			Attribute("email", String, "Optional exact-match email filter.")
		})

		Result(ListUsersResult)

		HTTP(func() {
			POST("/rpc/users.list")
			Response(StatusOK)
		})
	})

	Method("delete", func() {
		Description("Hard-delete a user. Cascades to memberships, auth_codes, tokens, and any current_users row whose subject_ref matches (idp-design.md §6.1).")

		Payload(func() {
			Attribute("id", String, "User UUID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			POST("/rpc/users.delete")
			Response(StatusNoContent)
		})
	})
})
