package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminBackfillMCPAuthGrantsResult = Type("AdminBackfillMCPAuthGrantsResult", func() {
	Required("granted")

	Attribute("granted", Int64, "How many principals gained the mcp:auth grant.")
})

// MCP parity: not exposed through Staff Admin MCP. This is a one-off
// fleet-wide data backfill run by a person from the admin dashboard.
func mcpAuthBackfillMethods() {
	Method("backfillMcpAuthGrants", func() {
		Description("Grants unrestricted mcp:auth to every principal that holds any mcp:connect grant, in every organization. The connect grant's selectors are ignored. Safe to run more than once.")

		Payload(func() {
			security.AdminAuthPayload()
		})

		Result(AdminBackfillMCPAuthGrantsResult)

		HTTP(func() {
			POST("/admin/mcpAuthGrants.backfill")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "adminBackfillMcpAuthGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "backfillMcpAuthGrants")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminBackfillMcpAuthGrants"}`)
	})
}
