package admin

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

// SupportCoverageCell is one (capability, surface) cell of the matrix. status
// is not a boolean so that "no evidence", "cannot answer yet" and "this pair
// can never report" stay distinguishable.
var SupportCoverageCell = Type("SupportCoverageCell", func() {
	Description("Observed evidence for one capability on one surface.")
	Attribute("capability", String, "Capability the cell reports on.", func() {
		Enum("session", "blocking", "identity", "cost", "shadow")
	})
	Attribute("surface", String, "Surface the cell reports on: Gram's own MCP gateway, or a consuming agent surface.", func() {
		Enum("mcp_gateway", "claude_code", "claude_chat", "cowork", "codex", "cursor", "other")
	})
	Attribute("status", String, "Whether evidence was found, absent, not answerable yet, or impossible for this pair.", func() {
		Enum("observed", "none", "pending", "na")
	})
	Attribute("value", Int64, "Primary measure: sessions, tokens, blocks, attributed sessions or distinct shadow servers depending on the capability. Zero unless observed.")
	Attribute("unit", String, "Singular noun the value counts, when the capability's own unit does not apply. The gateway is measured in tool calls where an agent surface is measured in sessions. Empty when the capability's default unit stands.")
	Attribute("detail", String, "Short qualifier rendered under the value. Empty when there is nothing to qualify.")
	// Absent rather than empty when there is no evidence: the declared
	// date-time format leaves no room for a sentinel, and one empty string
	// would fail validation for the whole response.
	Attribute("last_seen", String, "RFC3339 timestamp of the most recent supporting evidence. Absent unless observed.", func() {
		Format(FormatDateTime)
	})
	Required("capability", "surface", "status", "value", "unit", "detail")
})

// SupportCoverageUnmapped reports activity that folded onto no surface, so it
// is visible rather than silently missing from the matrix.
var SupportCoverageUnmapped = Type("SupportCoverageUnmapped", func() {
	Description("A hook_source the surface fold did not recognize.")
	Attribute("hook_source", String, "The raw, unrecognized hook_source.")
	Attribute("sessions", Int64, "Sessions observed under it inside the window.")
	Required("hook_source", "sessions")
})

var SupportCoverageResult = Type("SupportCoverageResult", func() {
	Description("Observed support coverage for one organization over a fixed window.")
	Attribute("cells", ArrayOf(SupportCoverageCell), "One cell per (capability, surface) pair. Always fully populated.")
	Attribute("unmapped", ArrayOf(SupportCoverageUnmapped), "Activity whose hook_source folded to no surface.")
	Attribute("window_days", Int, "Length of the observation window in days.")
	Attribute("from", String, "RFC3339 start of the observation window.", func() {
		Format(FormatDateTime)
	})
	Attribute("to", String, "RFC3339 end of the observation window.", func() {
		Format(FormatDateTime)
	})
	Required("cells", "unmapped", "window_days", "from", "to")
})

// MCP parity: exposed through Staff Admin MCP as
// get_organization_support_coverage, which is the audience this endpoint
// serves. Deliberately not a Platform MCP tool — that surface serves an
// organization's own administrators and members, and this one refuses them.
func supportCoverageMethods() {
	Method("getSupportCoverage", func() {
		Meta("openapi:operationId", "adminGetSupportCoverage")
		Meta("openapi:extension:x-speakeasy-name-override", "getSupportCoverage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"AdminGetSupportCoverage"}`)
		Description("Observed support coverage for one organization: per-surface evidence for session activity, policy enforcement, identity attribution, token usage and shadow MCP exposure, across Gram's MCP gateway and each consuming agent surface.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String, "Organization to report coverage for.")
			Attribute("window_days", Int, "Observation window in days.", func() {
				Minimum(1)
				Maximum(90)
				Default(30)
			})
			Required("organization_id")
		})
		Result(SupportCoverageResult)
		HTTP(func() {
			GET("/admin/supportCoverage.get")
			Param("organization_id")
			Param("window_days")
			Response(StatusOK)
		})
	})
}
