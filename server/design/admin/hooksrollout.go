package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

// maxHooksRolloutVersion bounds a pin in the design. The handler also rejects
// a pin above the hooks generator version the server publishes, which the
// design cannot express.
const maxHooksRolloutVersion = 100000

var AdminHooksRolloutSource = Type("AdminHooksRolloutSource", String, func() {
	Description("What decides an organization's hooks version: canary organizations always get the current version, organization and default name the pin that applies, and legacy_flag means no pin applies yet and the hooks-rollout PostHog flag decides.")
	Enum("canary", "organization", "default", "legacy_flag")
})

var AdminHooksRolloutPin = Type("AdminHooksRolloutPin", func() {
	Description("A hooks version rollout pin: the highest hooks generator version its scope is cleared to receive.")
	Required("version", "set_by", "set_at")

	Attribute("version", Int, "Highest hooks generator version the pin clears.")
	Attribute("set_by", String, "Email of the staff operator who set the pin.")
	Attribute("set_at", String, "When the pin was set.", func() { Format(FormatDateTime) })
})

var AdminHooksRolloutOverride = Type("AdminHooksRolloutOverride", func() {
	Description("An organization pinned to its own hooks version instead of the default pin.")
	Required("organization_id", "organization_name", "organization_slug", "pin")

	Attribute("organization_id", String)
	Attribute("organization_name", String)
	Attribute("organization_slug", String)
	Attribute("pin", AdminHooksRolloutPin)
})

var AdminHooksRolloutChange = Type("AdminHooksRolloutChange", func() {
	Description("One change to a hooks rollout pin.")
	Required("set_by", "set_at")

	Attribute("organization_id", String, "Organization whose override changed. Absent for a change to the default pin.")
	Attribute("organization_slug", String, "Slug of organization_id. Absent for a change to the default pin.")
	Attribute("version", Int, "Version the change set. Absent when the change cleared an organization override.")
	Attribute("set_by", String, "Email of the staff operator who made the change.")
	Attribute("set_at", String, "When the change was made.", func() { Format(FormatDateTime) })
})

var AdminHooksRollout = Type("AdminHooksRollout", func() {
	Description("Platform-wide hooks version rollout state.")
	Required("current_version", "canary_organization_slugs", "overrides", "recent_changes")

	Attribute("current_version", Int, "The hooks generator version this server build publishes to an organization once its pin reaches it.")
	Attribute("default_pin", AdminHooksRolloutPin, "The pin for every organization without an override. Absent until one is set; until then those organizations follow the legacy hooks-rollout PostHog flag.")
	Attribute("canary_organization_slugs", ArrayOf(String), "Organizations that always receive current_version and ignore pins.")
	Attribute("overrides", ArrayOf(AdminHooksRolloutOverride), "Organizations with an override, by slug.")
	Attribute("recent_changes", ArrayOf(AdminHooksRolloutChange), "The most recent pin changes, newest first.")
})

var AdminOrganizationHooksRollout = Type("AdminOrganizationHooksRollout", func() {
	Description("Hooks version rollout state for one organization.")
	Required("organization_id", "current_version", "source")

	Attribute("organization_id", String)
	Attribute("current_version", Int, "The hooks generator version this server build publishes to an organization once its pin reaches it.")
	Attribute("override", AdminHooksRolloutPin, "The organization's own pin. Absent when it follows the default pin.")
	Attribute("default_pin", AdminHooksRolloutPin, "The platform-wide default pin. Absent until one is set.")
	Attribute("source", AdminHooksRolloutSource)
	Attribute("effective_version", Int, "Version of the pin that applies. Absent for canary and legacy_flag.")
	Attribute("eligible", Boolean, "Whether the organization's next publish moves its hooks plugin to current_version. Absent for legacy_flag, which the admin server cannot read.")
})

// MCP parity: get_hooks_rollout and get_organization_hooks_rollout expose the
// reads to Staff Admin MCP. The writes are deliberately dashboard-only: moving
// the default pin changes the hooks plugin of every customer organization at
// once, which does not fit the Admin MCP proposal flow's single-organization
// targeting, and a release step this consequential stays a human action.
func hooksRolloutMethods() {
	Method("getHooksRollout", func() {
		Description("Returns the platform-wide hooks version rollout state: the version this build publishes, the default pin, canary organizations, organization overrides and recent changes.")
		Payload(func() {
			security.AdminAuthPayload()
		})
		Result(AdminHooksRollout)
		HTTP(func() {
			GET("/admin/hooksRollout.get")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminGetHooksRollout")
	})

	Method("setHooksRolloutDefault", func() {
		Description("Moves the default hooks rollout pin for every organization without an override. Customer organizations receive a hooks version on the next rollout sweep once the pin reaches it. Lowering the pin holds back later versions; it never downgrades a published hooks plugin.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "SetHooksRolloutDefaultRequestBody")
			Required("version")
			Attribute("version", Int, "Highest hooks generator version to clear. Must not exceed the version this build publishes.", func() {
				Minimum(1)
				Maximum(maxHooksRolloutVersion)
			})
		})
		Result(AdminHooksRollout)
		HTTP(func() {
			POST("/admin/hooksRollout.setDefault")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminSetHooksRolloutDefault")
	})

	Method("getOrganizationHooksRollout", func() {
		Description("Returns the hooks version rollout state for one organization.")
		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")
			Attribute("organization_id", String)
		})
		Result(AdminOrganizationHooksRollout)
		HTTP(func() {
			GET("/admin/organization.hooksRollout")
			Param("organization_id")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminGetOrganizationHooksRollout")
	})

	Method("setOrganizationHooksRollout", func() {
		Description("Pins one organization to its own hooks version, regardless of the default pin.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "SetOrganizationHooksRolloutRequestBody")
			Required("organization_id", "version")
			Attribute("organization_id", String)
			Attribute("version", Int, "Highest hooks generator version to clear for the organization. Must not exceed the version this build publishes.", func() {
				Minimum(1)
				Maximum(maxHooksRolloutVersion)
			})
		})
		Result(AdminOrganizationHooksRollout)
		HTTP(func() {
			POST("/admin/organization.setHooksRollout")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminSetOrganizationHooksRollout")
	})

	Method("clearOrganizationHooksRollout", func() {
		Description("Removes an organization's hooks version override so it follows the default pin again.")
		Payload(func() {
			security.AdminAuthPayload()
			Meta("openapi:typename", "ClearOrganizationHooksRolloutRequestBody")
			Required("organization_id")
			Attribute("organization_id", String)
		})
		Result(AdminOrganizationHooksRollout)
		HTTP(func() {
			POST("/admin/organization.clearHooksRollout")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminClearOrganizationHooksRollout")
	})
}
