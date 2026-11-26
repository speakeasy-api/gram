package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminBackfillMCPAuthGrantsResult = Type("AdminBackfillMCPAuthGrantsResult", func() {
	Required("granted")

	Attribute("granted", Int64, "Legacy response field; the retired endpoint does not grant access.")
})

// The retired endpoint is kept for existing HTTP clients, not exposed in Staff Admin MCP.
func mcpAuthBackfillMethods() {
	Method("backfillMcpAuthGrants", func() {
		Description("Retired. Rejects authenticated calls and does not modify grants.")

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
