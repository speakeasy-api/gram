package analytics

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var SignalComparison = Type("AnalyticsSignalComparison", func() {
	Attribute("from", String, func() { Format(FormatDateTime) })
	Attribute("to", String, func() { Format(FormatDateTime) })
	Attribute("summary", MapOf(String, Any))
	Attribute("comparable", Boolean)
	Attribute("reasons", ArrayOf(String), "Failed comparison gates. Coverage remains unknown even when the observed measurements are comparable.")
	Attribute("delta_share", Float64, "Descriptive difference in observed answer shares, never a significance test")
	Required("from", "to", "summary", "comparable", "reasons")
})

var SignalFindings = Type("AnalyticsSignalFindings", func() {
	Attribute("measurement", SignalResult)
	Attribute("concentration", MapOf(String, Any))
	Attribute("peer", SignalSelection)
	Attribute("peer_summary", MapOf(String, Any))
	Attribute("cooccurrence", ArrayOf(MapOf(String, Any)), "Separate same_message and same_session joint-answer counts and shares")
	Attribute("comparison", SignalComparison)
	Attribute("evidence", ArrayOf(MapOf(String, Any)), "Permitted recent conversation references without transcript content")
	Attribute("evidence_scope", String)
	Required("measurement", "concentration", "cooccurrence", "evidence", "evidence_scope")
})

func signalFindingsMethod() {
	Method("signalFindings", func() {
		Description("Find cross-session signal patterns, joint-answer co-occurrence and guarded previous-window changes. Aggregate reads require project:read and Signals Intelligence; each evidence reference additionally passes the underlying conversation access checks.")
		Payload(func() {
			Attribute("from", String, func() { Format(FormatDateTime) })
			Attribute("to", String, func() { Format(FormatDateTime) })
			Attribute("signal_query", SignalQuery, "Select a complete cohort, without group_by")
			Attribute("peer", SignalSelection, "Optional second predicate for co-occurrence in the same cohort and window")
			Attribute("compare_previous", Boolean, func() { Default(false) })
			Attribute("evidence", Boolean, func() { Default(false) })
			Required("from", "to", "signal_query")
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(SignalFindings)
		HTTP(func() {
			POST("/rpc/analytics.signalFindings")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "analyticsSignalFindings")
		Meta("openapi:extension:x-speakeasy-name-override", "signalFindings")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AnalyticsSignalFindings"}`)
	})
}
