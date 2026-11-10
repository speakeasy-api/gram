package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminHookFleetInstall = Type("AdminHookFleetInstall", func() {
	Description("One hooks install: a project, user, machine and harness. Version, generation and route are the latest the install sent.")
	Required("project_id", "user_email", "hostname", "harness", "version", "version_status", "endpoint_generation", "install_route", "first_seen", "last_seen", "events", "errors", "quiet")

	Attribute("project_id", String, "The project the install reports to. Empty for traffic that named no project.")
	Attribute("user_email", String, "The user the install reports as. Empty when the install sent no identity.")
	Attribute("hostname", String, "The machine's hostname. Empty when the install sent none.")
	Attribute("harness", String, "The agent the hooks run in, such as claude, codex or cursor.")
	Attribute("version", String, "The hooks binary version. Empty for the legacy curl hooks and for installs that sent no version.")
	Attribute("version_status", String, "How the version compares to the release Speakeasy ships.", func() {
		Enum("current", "behind", "unsupported", "unknown")
	})
	Attribute("endpoint_generation", String, "The hooks endpoint the install last sent to: legacy_unkeyed, legacy_keyed, ingest_script or ingest_binary. Empty for delivery failure reports only.")
	Attribute("install_route", String, "How the hooks were installed, such as marketplace or download. Empty when unknown.")
	Attribute("first_seen", String, "First hook in the window.", func() { Format(FormatDateTime) })
	Attribute("last_seen", String, "Most recent hook.", func() { Format(FormatDateTime) })
	Attribute("events", Int64, "Hook events recorded in the window.")
	Attribute("errors", Int64, "Delivery failures the install reported in the window.")
	Attribute("quiet", Boolean, "True when the install has sent nothing for quiet_after_days or longer.")
})

var AdminHookFleetVersion = Type("AdminHookFleetVersion", func() {
	Description("Installs on one hooks version.")
	Required("version", "status", "installs")

	Attribute("version", String, "The hooks binary version, or empty for installs with no version.")
	Attribute("status", String, "How the version compares to the release Speakeasy ships.", func() {
		Enum("current", "behind", "unsupported", "unknown")
	})
	Attribute("installs", Int64, "Installs whose latest version is this one.")
})

var AdminHookFleetBucket = Type("AdminHookFleetBucket", func() {
	Description("Installs and events for one value of a breakdown.")
	Required("value", "installs", "events")

	Attribute("value", String, "The route or endpoint generation. Empty when unknown.")
	Attribute("installs", Int64, "Installs whose latest value is this one.")
	Attribute("events", Int64, "Hook events sent with this value across every install. Zero for routes.")
})

var AdminHookFleetResult = Type("AdminHookFleetResult", func() {
	Description("The hooks installs of one organization over a window, with version, route and endpoint breakdowns.")
	Required("organization_id", "window_days", "since", "latest_version", "quiet_after_days", "truncated", "total_installs", "quiet_installs", "legacy_events", "versions", "routes", "generations", "installs")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("window_days", Int, "Length of the window in days.")
	Attribute("since", String, "Start of the window. Counts cover the whole UTC day it falls on.", func() { Format(FormatDateTime) })
	Attribute("latest_version", String, "The hooks release Speakeasy currently ships. Versions below it are behind.")
	Attribute("minimum_version", String, "The oldest supported hooks version. Versions below it are unsupported. Absent when no minimum is set.")
	Attribute("quiet_after_days", Int, "Days without traffic after which an install counts as quiet.")
	Attribute("truncated", Boolean, "True when the organization has more installs than the response holds. Breakdowns then cover only the installs returned.")
	Attribute("total_installs", Int64, "Installs returned. Not the organization's full install count when truncated is true.")
	Attribute("quiet_installs", Int64, "Returned installs that have gone quiet. Only a window longer than quiet_after_days can return quiet installs.")
	Attribute("legacy_events", Int64, "Hook events sent to the legacy endpoints in the window.")
	Attribute("versions", ArrayOf(AdminHookFleetVersion), "Installs per version, newest version first.")
	Attribute("routes", ArrayOf(AdminHookFleetBucket), "Installs per install route, largest first.")
	Attribute("generations", ArrayOf(AdminHookFleetBucket), "Installs and events per endpoint generation, largest first.")
	Attribute("installs", ArrayOf(AdminHookFleetInstall), "Installs, most recently seen first.")
})

// MCP parity: exposed through Staff Admin MCP as get_organization_hook_fleet.
// Deliberately not a Platform MCP tool: there is no customer-facing hooks
// fleet view.
func hookFleetMethods() {
	Method("getOrganizationHookFleet", func() {
		Meta("openapi:operationId", "adminGetOrganizationHookFleet")
		Meta("openapi:extension:x-speakeasy-name-override", "getOrganizationHookFleet")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminGetOrganizationHookFleet"}`)
		Description("The hooks installs of one organization: which versions, install routes and endpoints its users' machines use, which machines have gone quiet, and how much traffic still reaches the legacy endpoints.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String, "Organization to report on.")
			Attribute("window_days", Int, "Window in days.", func() {
				Minimum(1)
				Maximum(89)
				Default(30)
			})
			Required("organization_id")
		})
		Result(AdminHookFleetResult)
		HTTP(func() {
			GET("/admin/organizationHookFleet.get")
			Param("organization_id")
			Param("window_days")
			Response(StatusOK)
		})
	})
}
