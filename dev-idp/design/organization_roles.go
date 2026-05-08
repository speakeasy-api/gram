package design

import (
	. "goa.design/goa/v3/dsl"
)

var ListOrganizationRolesResult = Type("ListOrganizationRolesResult", func() {
	Attribute("items", ArrayOf(OrganizationRole), "Roles for the organization.")
	Required("items")
})

var _ = Service("organizationRoles", func() {
	Description("Dev-idp organization-role CRUD. Mirrors the WorkOS authorization-role surface (idp-design.md §7.1, /authorization/organizations/{id}/roles[/{slug}]). Keyed on `(organization_id, slug)` since slug is the natural identifier; the underlying row's UUID is also returned for completeness. Permanently unauthenticated.")

	Method("create", func() {
		Description("Create a role on an organization.")

		Payload(func() {
			Attribute("organization_id", String, "Organization UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "Role slug — unique within the org.")
			Attribute("name", String, "Display name.")
			Attribute("description", String, "Free-form description.")
			Required("organization_id", "slug", "name")
		})

		Result(OrganizationRole)

		HTTP(func() {
			POST("/rpc/organizationRoles.create")
			Response(StatusOK)
		})
	})

	Method("update", func() {
		Description("Patch a role's name and/or description by (organization_id, slug).")

		Payload(func() {
			Attribute("organization_id", String, "Organization UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "Role slug.")
			Attribute("name", String)
			Attribute("description", String)
			Required("organization_id", "slug")
		})

		Result(OrganizationRole)

		HTTP(func() {
			POST("/rpc/organizationRoles.update")
			Response(StatusOK)
		})
	})

	Method("list", func() {
		Description("List every role on an organization. No pagination — roles are a small per-org set.")

		Payload(func() {
			Attribute("organization_id", String, "Organization UUID.", func() {
				Format(FormatUUID)
			})
			Required("organization_id")
		})

		Result(ListOrganizationRolesResult)

		HTTP(func() {
			POST("/rpc/organizationRoles.list")
			Response(StatusOK)
		})
	})

	Method("delete", func() {
		Description("Delete a role from an organization by (organization_id, slug).")

		Payload(func() {
			Attribute("organization_id", String, "Organization UUID.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "Role slug.")
			Required("organization_id", "slug")
		})

		HTTP(func() {
			POST("/rpc/organizationRoles.delete")
			Response(StatusNoContent)
		})
	})
})
