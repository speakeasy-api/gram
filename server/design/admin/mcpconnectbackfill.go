package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminBackfillMCPConnectGrantsResult = Type("AdminBackfillMCPConnectGrantsResult", func() {
	Required("inserted", "extended", "taken_over")

	Attribute("inserted", Int64, "How many mcp:connect grants were written.")
	Attribute("extended", Int64, "How many existing temporary mcp:connect grants were extended to last as long as the access they mirror.")
	Attribute("taken_over", Int64, "How many mcp:connect grants provisioned by the default auth permission backfill became explicit grants.")
})

// MCP parity: not exposed through Staff Admin MCP. This is a fleet-wide data
// backfill run by a person from the admin API around one rollout; Admin MCP
// writes need a per-tenant proposal and approval, which a fleet-wide backfill
// has no target for.
func mcpConnectBackfillMethods() {
	Method("backfillMcpConnectGrants", func() {
		Description("Adds an explicit mcp:connect grant, with the same principal, selectors and expiry, for every unexpired mcp:read or mcp:write grant in every organization, ahead of read and write no longer implying connect. Never removes access. Safe to run more than once.")

		Payload(func() {
			security.AdminAuthPayload()
			Attribute("created_before", String, "Only copy grants created before this time. Set it to when read and write stopped implying connect, so grants written since are left alone.", func() {
				Format(FormatDateTime)
			})
		})

		Result(AdminBackfillMCPConnectGrantsResult)

		HTTP(func() {
			POST("/admin/mcpConnectGrants.backfill")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminBackfillMcpConnectGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "backfillMcpConnectGrants")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminBackfillMcpConnectGrants"}`)
	})
}
