package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

var AdminPlatformMCPFeedback = Type("AdminPlatformMCPFeedback", func() {
	Description("One feedback report a user sent through the Platform MCP send_platform_mcp_feedback tool.")
	Required("id", "organization_id", "subject_urn", "category", "delivery_state", "created_at", "expires_at")

	Attribute("id", String, "The feedback ID.", func() { Format(FormatUUID) })
	Attribute("organization_id", String, "The organization ID.")
	Attribute("organization_name", String, "The organization name. Absent when the organization no longer exists.")
	Attribute("organization_slug", String, "The organization slug. Absent when the organization no longer exists.")
	Attribute("subject_urn", String, "The session subject that sent the feedback.")
	Attribute("user_email", String, "The email of the user who sent the feedback. Absent when the user no longer exists.")
	Attribute("category", String, "The feedback category.")
	Attribute("rating", Int, "The optional rating from 1 through 5.")
	Attribute("success", Boolean, "The optional success outcome.")
	Attribute("tool_name", String, "The optional Platform MCP tool the feedback is about.")
	Attribute("failure_category", String, "The optional failure or readiness category.")
	Attribute("note", String, "The optional free-form note.")
	Attribute("delivery_state", String, "The delivery state of the stored row.")
	Attribute("created_at", String, "When the feedback was sent.", func() { Format(FormatDateTime) })
	Attribute("expires_at", String, "When the stored row is deleted.", func() { Format(FormatDateTime) })
})

// MCP parity: not exposed through Staff Admin MCP. Notes are free text that
// customers write, so an agent that reads them would take customer text into
// its context as if it were trusted. Staff read feedback on this page. Not a
// Platform MCP tool: it spans every organization.
func platformMCPFeedbackMethods() {
	Method("listPlatformMcpFeedback", func() {
		Meta("openapi:operationId", "adminListPlatformMcpFeedback")
		Meta("openapi:extension:x-speakeasy-name-override", "listPlatformMcpFeedback")
		Description("List the newest Platform MCP feedback reports across every organization.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("limit", Int, "Maximum number of reports to return.", func() {
				Minimum(1)
				Maximum(500)
				Default(200)
			})
		})
		Result(func() {
			Attribute("feedback", ArrayOf(AdminPlatformMCPFeedback), "Feedback reports, newest first.")
			Required("feedback")
		})
		HTTP(func() {
			GET("/admin/platformMcpFeedback.list")
			Param("limit")
			Response(StatusOK)
		})
	})
}
