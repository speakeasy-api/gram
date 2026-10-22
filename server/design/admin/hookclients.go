package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminHookClientOrganization = Type("AdminHookClientOrganization", func() {
	Description("An organization that appears in any of the fleet health grids.")
	Required("id")

	Attribute("id", String, "The organization ID.")
	Attribute("name", String, "The organization name. Absent when the organization no longer exists.")
	Attribute("slug", String, "The organization slug. Absent when the organization no longer exists.")
})

var AdminHookClientSessionCell = Type("AdminHookClientSessionCell", func() {
	Description("One (organization, day, client) cell of the hook client grid. Sessions count on the UTC day of their first hook.")
	Required("organization_id", "day", "client", "machines", "sessions", "machines_all")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("day", String, "The UTC day, as YYYY-MM-DD.", func() { Format(FormatDate) })
	Attribute("client", String, "curl for the legacy curl hooks, a hooks binary version, binary for binary rows that record no version, or empty for the organization-day total.")
	Attribute("machines", Int64, "Machines that started a session on this day.")
	Attribute("sessions", Int64, "Sessions started on this day.")
	Attribute("machines_all", Int64, "Machines that sent any hook on this day, including sessions started earlier.")
})

var AdminInferenceHookCell = Type("AdminInferenceHookCell", func() {
	Description("One (organization, day, surface) cell of the inference hook grid.")
	Required("organization_id", "day", "surface", "users", "events", "sessions")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("day", String, "The UTC day, as YYYY-MM-DD.", func() { Format(FormatDate) })
	Attribute("surface", String, "The Claude app that sent the transcript, or empty for the organization-day total.")
	Attribute("users", Int64, "Distinct users.")
	Attribute("events", Int64, "Stored transcript messages.")
	Attribute("sessions", Int64, "Distinct sessions.")
})

var AdminDeviceAgentCell = Type("AdminDeviceAgentCell", func() {
	Description("One (organization, day, version) cell of the device agent grid.")
	Required("organization_id", "day", "version", "machines")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("day", String, "The UTC day, as YYYY-MM-DD.", func() { Format(FormatDate) })
	Attribute("version", String, "The speakeasyd version, or empty for the organization-day total.")
	Attribute("machines", Int64, "Machines whose agent polled on this day, by the last version each reported that day.")
})

var AdminHookClientsResult = Type("AdminHookClientsResult", func() {
	Description("Fleet-wide client health grids over a fixed window: hook clients, inference hook traffic and device agent versions.")
	Required("window_days", "queried_at", "from_cache", "organizations", "sessions", "inference", "device_agents")

	Attribute("window_days", Int, "Length of the window in days.")
	Attribute("queried_at", String, "When the grids were read from ClickHouse.", func() { Format(FormatDateTime) })
	Attribute("from_cache", Boolean, "True when the grids came from the cache rather than a fresh ClickHouse read. Cached grids last 24 hours and refresh in the background once an hour old.")
	Attribute("organizations", ArrayOf(AdminHookClientOrganization), "Every organization that appears in any grid.")
	Attribute("sessions", ArrayOf(AdminHookClientSessionCell), "The hook client grid, new sessions only.")
	Attribute("inference", ArrayOf(AdminInferenceHookCell), "The inference hook traffic grid.")
	Attribute("device_agents", ArrayOf(AdminDeviceAgentCell), "The device agent version grid. Empty for days before versions were recorded.")
})

// MCP parity: not exposed through Staff Admin MCP. This is a fleet-wide
// chart read for incident follow-up; the per-organization question an agent
// would ask ("which surfaces does this org use?") is already answered by
// get_organization_support_coverage. Deliberately not a Platform MCP tool:
// it spans every organization.
func hookClientsMethods() {
	Method("getHookClients", func() {
		Meta("openapi:operationId", "adminGetHookClients")
		Meta("openapi:extension:x-speakeasy-name-override", "getHookClients")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminGetHookClients"}`)
		Description("Fleet-wide client health grids: which hook client (legacy curl or hooks binary version) each organization's new sessions use per day, inference hook traffic per Claude app, and device agent versions per day.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("window_days", Int, "Window in days.", func() {
				Minimum(1)
				Maximum(30)
				Default(15)
			})
			Attribute("refresh", Boolean, "Skip the cache and read ClickHouse now.", func() {
				Default(false)
			})
		})
		Result(AdminHookClientsResult)
		HTTP(func() {
			GET("/admin/hookClients.get")
			Param("window_days")
			Param("refresh")
			Response(StatusOK)
		})
	})
}
