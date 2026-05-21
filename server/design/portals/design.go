package portals

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("portals", func() {
	Description("Manages per-project Internal MCP Server Portals.")

	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("getPortal", func() {
		Description("Get the portal configuration and server cards for a project. Returns 404 when the portal does not exist or is disabled, unless preview=true and the caller has project:write.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("preview", Boolean, "Bypass the disabled-portal 404 for project admins (for the in-settings preview).")
		})

		Result(Portal)

		HTTP(func() {
			GET("/rpc/portals.get")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("preview")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getPortal")
		Meta("openapi:extension:x-speakeasy-name-override", "read")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Portal"}`)
	})

	Method("updatePortal", func() {
		Description("Create or update the portal configuration for a project.")

		Payload(func() {
			Extend(UpdatePortalForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(Portal)

		HTTP(func() {
			POST("/rpc/portals.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updatePortal")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdatePortal"}`)
	})
})

var UpdatePortalForm = Type("UpdatePortalForm", func() {
	Attribute("enabled", Boolean, "Whether the portal is publicly reachable to org members.")
	Attribute("display_name", String, "Override for the portal's display name. Empty string clears the override.")
	Attribute("tagline", String, "Short tagline shown under the title. Empty string clears.")
	Attribute("logo_asset_id", String, "UUID of an asset to use as the logo. Empty string clears.", func() {
		Format(FormatUUID)
	})
})

var Portal = Type("Portal", func() {
	Attribute("enabled", Boolean, "Whether the portal is enabled.", func() { Default(false) })
	Attribute("project_slug", String, "The project's slug.")
	Attribute("display_name", String, "Resolved display name (override → project.name).")
	Attribute("tagline", String, "Tagline if set.")
	Attribute("logo_url", String, "Resolved logo URL or empty when none.")
	Attribute("servers", ArrayOf(PortalServer), "Cards to render on the portal.")
	Required("enabled", "project_slug", "display_name", "servers")
})

var PortalServer = Type("PortalServer", func() {
	Attribute("slug", String, "Endpoint slug.")
	Attribute("name", String, "Server name.")
	Attribute("description", String, "Server or toolset description.")
	Attribute("tool_count", Int, "Number of tools exposed by this server.")
	Attribute("install_url", String, "URL of the per-server install page.")
	Required("slug", "name", "tool_count", "install_url")
})
