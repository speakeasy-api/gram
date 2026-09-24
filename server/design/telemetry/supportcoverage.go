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

func supportCoverageMethods() {
	Method("getSupportCoverage", func() {
		Description("Observed support coverage for the caller's organization: per-surface evidence for session activity, policy enforcement, identity attribution, token usage and shadow MCP exposure. Every cell distinguishes evidence found from evidence absent from evidence not yet answerable, so an empty cell is never rendered as unsupported.")

		// Org-scoped like telemetry.query: coverage spans every project in
		// the caller's organization, and the page that renders it is a
		// platform-admin view of one organization at a time.
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
