package design

import (
	. "goa.design/goa/v3/dsl"
)

var ListOrganizationsResult = Type("ListOrganizationsResult", func() {
	Attribute("items", ArrayOf(Organization), "Organizations on this page.")
	PaginationResult()
	Required("items")
})

var _ = Service("organizations", func() {
	Description("Dev-idp organizations CRUD. Backs both mock-speakeasy and oauth2 modes' org metadata. Permanently unauthenticated (idp-design.md §6).")

	Method("create", func() {
		Description("Create an organization.")

		Payload(func() {
			Attribute("name", String, "Display name.")
			Attribute("slug", String, "URL slug. Must be unique.")
			Attribute("account_type", String, "Plan tier; defaults to `free`.")
			Attribute("workos_id", String, "Optional WorkOS organization id echoed by mock-speakeasy validate.")
			Required("name", "slug")
		})

		Result(Organization)

		HTTP(func() {
			POST("/rpc/organizations.create")
			Response(StatusOK)
		})
	})

	Method("update", func() {
		Description("Patch an organization. Empty `workos_id` clears the field.")

		Payload(func() {
			Attribute("id", String, "Organization UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("name", String, "Display name.")
			Attribute("slug", String, "URL slug.")
			Attribute("account_type", String, "Plan tier.")
			Attribute("workos_id", String, "WorkOS organization id; pass empty string to clear.")
			Required("id")
		})

		Result(Organization)

		HTTP(func() {
			POST("/rpc/organizations.update")
			Response(StatusOK)
		})
	})

	Method("list", func() {
		Description("List organizations.")

		Payload(func() {
			PaginationPayload()
		})

		Result(ListOrganizationsResult)

		HTTP(func() {
			POST("/rpc/organizations.list")
			Response(StatusOK)
		})
	})

	Method("delete", func() {
		Description("Hard-delete an organization. Cascades to memberships.")

		Payload(func() {
			Attribute("id", String, "Organization UUID.", func() {
				Format(FormatUUID)
			})
			Required("id")
		})

		HTTP(func() {
			POST("/rpc/organizations.delete")
			Response(StatusNoContent)
		})
	})
})
