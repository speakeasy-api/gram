package design

import (
	. "goa.design/goa/v3/dsl"
)

var ListMembershipsResult = Type("ListMembershipsResult", func() {
	Attribute("items", ArrayOf(Membership), "Memberships on this page.")
	PaginationResult()
	Required("items")
})

var _ = Service("memberships", func() {
	Description("Dev-idp memberships CRUD. Idempotent on (user_id, organization_id) for create. Permanently unauthenticated.")

	Method("create", func() {
		Description("Idempotently create a membership for (user_id, organization_id).")

		Payload(func() {
			Attribute("user_id", String, "User UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("organization_id", String, "Organization UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("role", String, "Role within the organization; defaults to `member`.")
			Required("user_id", "organization_id")
		})

		Result(Membership)

		HTTP(func() {
			POST("/rpc/memberships.create")
			Response(StatusOK)
		})
	})

	Method("update", func() {
		Description("Patch a membership's role.")

		Payload(func() {
			Attribute("id", String, "Membership UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("role", String, "New role.")
			Required("id", "role")
		})

		Result(Membership)

		HTTP(func() {
			POST("/rpc/memberships.update")
			Response(StatusOK)
		})
	})

	Method("list", func() {
		Description("List memberships. Optional `user_id` and/or `organization_id` filters (idp-design.md §6.1).")

		Payload(func() {
			PaginationPayload()
			Attribute("user_id", String, "Optional user filter.", func() {
				Format(FormatUUID)
			})
			Attribute("organization_id", String, "Optional organization filter.", func() {
				Format(FormatUUID)
			})
		})

		Result(ListMembershipsResult)

		HTTP(func() {
			POST("/rpc/memberships.list")
			Response(StatusOK)
		})
	})

	Method("delete", func() {
		Description("Hard-delete a membership.")

		Payload(func() {
			Attribute("id", String, "Membership UUID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			POST("/rpc/memberships.delete")
			Response(StatusNoContent)
		})
	})
})
