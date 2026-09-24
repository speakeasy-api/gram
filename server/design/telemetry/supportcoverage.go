package telemetry

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

// SupportCoverageCell is one (capability, surface) cell of the coverage
// matrix.
//
// status is what the cell means, and it is deliberately not a boolean. The
// page's whole failure mode was rendering "we did not ask" identically to "we
// asked and there is nothing", so the three cases are distinct values:
// observed (evidence exists), none (the source was queried and reported
// nothing) and pending (the evidence source exists but cannot answer for this
// surface yet).
var SupportCoverageCell = Type("SupportCoverageCell", func() {
	Description("Observed evidence for one capability on one surface.")
	Attribute("capability", String, "Capability the cell reports on.", func() {
		Enum("session", "blocking", "identity", "cost", "shadow")
	})
	Attribute("surface", String, "Consuming surface the cell reports on.", func() {
		Enum("claude_code", "claude_chat", "cowork", "codex", "cursor", "other")
	})
	Attribute("status", String, "Whether evidence was found, absent, or cannot be answered for this surface yet.", func() {
		Enum("observed", "none", "pending")
	})
	Attribute("value", Int64, "Primary measure behind the cell: sessions, tokens, blocks, attributed sessions or distinct shadow servers depending on the capability. Zero when status is not observed.")
	Attribute("detail", String, "Short human-readable qualifier rendered under the value, e.g. the identity split or the granularity a value was attributed at. Empty when there is nothing to qualify.")
	Attribute("last_seen", String, "RFC3339 timestamp of the most recent supporting evidence. Empty when status is not observed.")
	Required("capability", "surface", "status", "value", "detail", "last_seen")
})

// SupportCoverageUnmapped reports activity that could not be folded onto a
// surface. Without it an unrecognized hook_source would vanish from the matrix
// and still be counted as full coverage by the summary tiles.
var SupportCoverageUnmapped = Type("SupportCoverageUnmapped", func() {
	Description("A hook_source the surface fold did not recognize, reported rather than discarded.")
	Attribute("hook_source", String, "The raw, unrecognized hook_source.")
	Attribute("sessions", Int64, "Sessions observed under it inside the window.")
	Required("hook_source", "sessions")
})

var SupportCoverageResult = Type("SupportCoverageResult", func() {
	Description("Observed support coverage for the caller's organization over a fixed observation window.")
	Attribute("cells", ArrayOf(SupportCoverageCell), "One cell per (capability, surface) pair. Always fully populated, so clients never infer a missing pair as unsupported.")
	Attribute("unmapped", ArrayOf(SupportCoverageUnmapped), "Activity whose hook_source folded to no surface. Non-empty means the matrix is not showing everything the org did.")
	Attribute("window_days", Int, "Length of the observation window in days.")
	Attribute("from", String, "RFC3339 start of the observation window.", func() {
		Format(FormatDateTime)
	})
	Attribute("to", String, "RFC3339 end of the observation window.", func() {
		Format(FormatDateTime)
	})
	Required("cells", "unmapped", "window_days", "from", "to")
})

// Platform MCP assessment (see .agents/skills/maintaining-platform-mcp):
// decision is to intentionally omit a tool for this endpoint, for now.
//
//   - Outcome: "which agent surfaces is this organization's telemetry actually
//     covering, and which integration would close the biggest gap".
//   - Actor: a Speakeasy platform admin, enforced in the handler via
//     auth.RequirePlatformAdmin rather than left to the dashboard's
//     PlatformAdminGate, which is presentation only. The view exists for
//     support and onboarding conversations, not for the organization's own
//     administrators.
//   - Existing tools: the underlying evidence is already reachable through
//     query_mcp_metrics, list_shadow_mcp_inventory, query_skill_usage and
//     get_project_overview. What this endpoint adds is the framing — a fixed
//     capability-by-surface matrix and a gap ranking — rather than new facts.
//   - Rationale for omitting: Platform MCP serves an organization's own
//     administrators and members, and this endpoint deliberately refuses them.
//     Admitting it would either contradict that gate or require widening the
//     audience, which is a product decision that has not been made. Half the payload is also static product
//     capability rather than organization state, so an agent would be liable
//     to present integration advice as if it were grounded in this org's data.
//   - Revisit when: the coverage view is promoted out of platform-admin to a
//     customer-facing surface. The contract is settled at that point and this
//     becomes a genuine administrator outcome worth a tool.
func supportCoverageMethods() {
	Method("getSupportCoverage", func() {
		Description("Observed support coverage for the caller's organization: per-surface evidence for session activity, policy enforcement, identity attribution, token usage and shadow MCP exposure. Every cell distinguishes evidence found from evidence absent from evidence not yet answerable, so an empty cell is never rendered as unsupported.")

		// Session security carries the scheme; the handler additionally
		// enforces the platform-admin flag through auth.RequirePlatformAdmin,
		// because this is a Speakeasy support view rather than something an
		// organization's own members should read. Coverage is org-scoped like
		// telemetry.query: it spans every project in the caller's active
		// organization.
		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
			Attribute("window_days", Int, "Observation window in days.", func() {
				Minimum(1)
				Maximum(90)
				Default(30)
			})
		})

		Result(SupportCoverageResult)

		HTTP(func() {
			GET("/rpc/telemetry.getSupportCoverage")
			Param("window_days")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getSupportCoverage")
		Meta("openapi:extension:x-speakeasy-name-override", "getSupportCoverage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "TelemetrySupportCoverage", "type": "query"}`)
	})
}
