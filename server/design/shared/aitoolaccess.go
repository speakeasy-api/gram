package shared

import (
	. "goa.design/goa/v3/dsl"
)

// AIToolAccessSummaryModel is the enforcement verdict for one AI tool. It is
// shared because two services render it: the access service on detections
// and decisions, the agent service on every scan target in the library.
// Generated into gen/types so both services use one Go struct and one
// builder (mv.BuildAIToolAccessSummaryView).
var AIToolAccessSummaryModel = Type("AIToolAccessSummary", func() {
	Meta("struct:pkg:path", "types")

	Description("The enforcement verdict for one AI tool, computed server-side so a client renders wording without re-deriving it. state shares its vocabulary with the shadow MCP inventory's access summary, so one column describes both halves of the Shadow AI section.")

	Required("state", "decision", "enforceable")

	Attribute("state", String, func() {
		Description("The effective decision as a table renders it: allowed and blocked mean an organization decision is recorded and in force, unreviewed means nothing is enforced — either because nobody has decided or because the tool is not enforceable, in which case a stored decision is projected as unreviewed. Read enforceable to tell the two apart.")
		Enum("allowed", "blocked", "unreviewed")
	})
	Attribute("decision", String, func() {
		Description("The effective organization decision behind state, which is not always the stored one. Absent rows read as unreviewed, so every tool has a value, and a tool that is not enforceable reads as unreviewed however it was decided — the stored decision is left untouched and takes effect again if the tool gains a verifiable gateway matcher.")
		Enum("unreviewed", "approved", "blocked")
	})
	Attribute("enforceable", Boolean, "Whether a block on this tool would actually reach the gateway. False when the tool is linked only by a self-reported client name, or by nothing at all: the decision is recorded honestly and enforces nothing. Surfaced so an admin is never told a tool is blocked when it is not.")
	Attribute("rationale", String, "Why the decision was made, when an admin gave a reason. Who recorded it and when are in the audit log rather than here, so there is one record of that and not two.")
})
