package skillefficacy

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var Settings = Type("SkillEfficacySettings", func() {
	Description("Per-organization settings for skill efficacy scoring. Scoring is session-grained: one evaluation covers every skill a session activated, so the cap counts sessions per day.")
	Required("organization_id", "enabled", "daily_cap", "is_default")
	Attribute("organization_id", String, "Organization these settings apply to.")
	Attribute("enabled", Boolean, "Whether skill efficacy scoring is enabled.")
	Attribute("daily_cap", Int, "Maximum session evaluations reserved across the organization each UTC day.", func() { Minimum(0); Maximum(10000) })
	Attribute("is_default", Boolean, "Whether these values are platform defaults rather than stored organization settings.")
})

var EfficacyMetrics = Type("SkillEfficacyMetrics", func() {
	Required("scored_sessions", "average_score", "estimated_turns_saved_total", "estimated_turns_saved_samples", "estimated_minutes_saved_total", "estimated_minutes_saved_samples", "roi_confidence_counts", "flag_counts")
	Attribute("scored_sessions", UInt64)
	Attribute("average_score", Float64)
	Attribute("estimated_turns_saved_total", Float64)
	Attribute("estimated_turns_saved_average", Float64)
	Attribute("estimated_turns_saved_samples", UInt64)
	Attribute("estimated_minutes_saved_total", Float64)
	Attribute("estimated_minutes_saved_average", Float64)
	Attribute("estimated_minutes_saved_samples", UInt64)
	Attribute("roi_confidence_counts", MapOf(String, UInt64))
	Attribute("flag_counts", MapOf(String, UInt64))
})

var InsightMetrics = Type("SkillInsightMetrics", func() {
	Required("activations", "activated_sessions", "session_cost_usd")
	Attribute("activations", UInt64)
	Attribute("activated_sessions", UInt64)
	Attribute("session_cost_usd", Float64)
	Attribute("average_session_cost_usd", Float64)
	Attribute("efficacy", EfficacyMetrics, "Absent when no sampled score exists.")
})

var InsightPoint = Type("SkillInsightPoint", func() {
	Required("bucket_start", "activations", "activated_sessions", "session_cost_usd", "scored_sessions", "estimated_minutes_saved")
	Attribute("bucket_start", String, func() { Format(FormatDateTime) })
	Attribute("activations", UInt64)
	Attribute("activated_sessions", UInt64)
	Attribute("session_cost_usd", Float64)
	Attribute("scored_sessions", UInt64)
	Attribute("average_score", Float64)
	Attribute("estimated_minutes_saved", Float64)
})

var VersionInsight = Type("SkillVersionInsight", func() {
	Required("skill_version_id", "metrics", "trend")
	Attribute("skill_version_id", String, func() { Format(FormatUUID) })
	Attribute("metrics", InsightMetrics)
	Attribute("trend", ArrayOf(InsightPoint))
})

var SkillInsight = Type("SkillEfficacyInsight", func() {
	Required("skill_id", "metrics", "versions")
	Attribute("skill_id", String, func() { Format(FormatUUID) })
	Attribute("metrics", InsightMetrics)
	Attribute("versions", ArrayOf(VersionInsight))
})

var ScoredSession = Type("SkillEfficacyScoredSession", func() {
	Required("id", "skill_id", "skill_version_id", "surface", "activated_at", "scored_at", "score", "rationale", "flags")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("skill_id", String, func() { Format(FormatUUID) })
	Attribute("skill_version_id", String, func() { Format(FormatUUID) })
	Attribute("surface", String, func() { Enum("dev", "assistant") })
	Attribute("activated_at", String, func() { Format(FormatDateTime) })
	Attribute("scored_at", String, func() { Format(FormatDateTime) })
	Attribute("score", Float64)
	Attribute("rationale", String)
	Attribute("estimated_turns_saved", Float64)
	Attribute("estimated_minutes_saved", Float64)
	Attribute("roi_confidence", String, func() { Enum("low", "med", "high") })
	Attribute("flags", ArrayOf(String))
	Attribute("gram_chat_id", String, func() { Format(FormatUUID) })
})

var InsightsResult = Type("SkillEfficacyInsightsResult", func() {
	Required("from", "to", "interval_seconds", "scores_available", "insights", "scored_sessions")
	Attribute("from", String, func() { Format(FormatDateTime) })
	Attribute("to", String, func() { Format(FormatDateTime) })
	Attribute("interval_seconds", Int64)
	Attribute("scores_available", Boolean)
	Attribute("insights", ArrayOf(SkillInsight))
	Attribute("scored_sessions", ArrayOf(ScoredSession))
})

var _ = Service("skillEfficacy", func() {
	Description("Manage organization-wide skill efficacy sampling settings.")

	shared.DeclareErrorResponses()

	Method("getSettings", func() {
		Description("Get effective organization-wide skill efficacy sampling settings.")

		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(Settings)

		HTTP(func() {
			GET("/rpc/skillEfficacy.getSettings")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getSkillEfficacySettings")
		Meta("openapi:extension:x-speakeasy-name-override", "getSettings")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SkillEfficacySettings"}`)
	})

	Method("upsertSettings", func() {
		Description("Create or replace organization-wide skill efficacy sampling settings.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("enabled", Boolean, "Whether skill efficacy scoring is enabled.")
			Attribute("daily_cap", Int, "Maximum session evaluations reserved across the organization each UTC day.", func() { Minimum(0); Maximum(10000) })
			Required("enabled", "daily_cap")
		})

		Result(Settings)

		HTTP(func() {
			POST("/rpc/skillEfficacy.upsertSettings")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertSkillEfficacySettings")
		Meta("openapi:extension:x-speakeasy-name-override", "upsertSettings")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertSkillEfficacySettings"}`)
	})

	Method("queryInsights", func() {
		Description("Query activation-time skill efficacy, estimated savings, attributed session cost, and optional scored-session detail for the current project. Scores are sampled and costs fan out to every activated version.")
		Security(security.Session, security.ProjectSlug)
		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("skill_ids", ArrayOf(String), "Active skill IDs to query (up to 200). Omit to query all active project skills.")
			Attribute("from", String, "RFC3339 window start; defaults to 30 days before to.", func() { Format(FormatDateTime) })
			Attribute("to", String, "RFC3339 window end; defaults to now.", func() { Format(FormatDateTime) })
			Attribute("include_versions", Boolean, "Include per-version daily trends.")
			Attribute("include_scored_sessions", Boolean, "Include up to 100 recent scored sessions. Intended for one skill detail view.")
		})
		Result(InsightsResult)
		HTTP(func() {
			GET("/rpc/skillEfficacy.queryInsights")
			security.SessionHeader()
			security.ProjectHeader()
			Param("skill_ids")
			Param("from")
			Param("to")
			Param("include_versions")
			Param("include_scored_sessions")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "querySkillEfficacyInsights")
		Meta("openapi:extension:x-speakeasy-name-override", "queryInsights")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SkillEfficacyInsights", "type": "query"}`)
	})
})
