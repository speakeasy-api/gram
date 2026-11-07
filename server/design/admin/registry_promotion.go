package admin

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var AdminRegistryPromotionSuggestions = Type("AdminRegistryPromotionSuggestions", func() {
	Description("Untrusted observed metadata suggestions. Staff must edit and explicitly confirm every value before publication.")
	Attribute("public_name", String)
	Attribute("public_title", String)
	Attribute("public_description", String)
	Attribute("public_url", String)
	Attribute("documentation_url", String)
	Attribute("docs_slug", String)
	Attribute("auth_mode", String, func() { Enum("none", "oauth") })
})

var AdminRegistryPromotionAuthDiscovery = Type("AdminRegistryPromotionAuthDiscovery", func() {
	Description("Publicly advertised authentication capabilities, not a verified OAuth connection.")
	Attribute("supports_dcr", Boolean)
	Attribute("supports_cimd", Boolean)
	Attribute("auth_setup", String, func() { Enum("none", "dcr", "cimd", "manual", "unknown") })
	Attribute("status", String, func() { Enum("complete", "partial", "unavailable", "not_required") })
	Required("supports_dcr", "supports_cimd", "auth_setup", "status")
})

var AdminRegistryPromotionToolUsage = Type("AdminRegistryPromotionToolUsage", func() {
	Attribute("name", String)
	Attribute("calls", Int64)
	Attribute("successes", Int64)
	Required("name", "calls", "successes")
})

var AdminRegistryPromotionAccountUsage = Type("AdminRegistryPromotionAccountUsage", func() {
	Description("Aggregate source-account usage for staff only. Contains no caller identity and must never be copied into public fields.")
	Attribute("window_days", Int)
	Attribute("distinct_users", Int64)
	Attribute("clients", ArrayOf(String))
	Attribute("tools", ArrayOf(AdminRegistryPromotionToolUsage))
	Attribute("transport", String)
	Attribute("resource_name", String)
	Attribute("documentation_url", String)
	Required("window_days", "distinct_users", "clients", "tools", "transport")
})

var AdminRegistryPromotionResearch = Type("AdminRegistryPromotionResearch", func() {
	Description("The latest research run. Findings are cited web claims, never a verdict, and may be inaccurate or deliberately seeded.")
	Attribute("status", String)
	Attribute("report", Any)
	Attribute("error", String)
	Attribute("started_at", String, func() { Format(FormatDateTime) })
	Attribute("completed_at", String, func() { Format(FormatDateTime) })
	Required("status")
})

var AdminRegistryPromotionReview = Type("AdminRegistryPromotionReview", func() {
	Description("The server's MCP review, held in Speakeasy's own organization. Nothing is recorded in the source account. Evidence is untrusted data authored largely by the reviewed server.")
	Attribute("request_id", String)
	Attribute("evidence", Any)
	Attribute("evidence_collected_at", String, func() { Format(FormatDateTime) })
	Attribute("research", AdminRegistryPromotionResearch)
	Required("request_id")
})

var AdminRegistryPromotionReviewResult = Type("AdminRegistryPromotionReviewResult", func() {
	Attribute("review", AdminRegistryPromotionReview, "Absent until a review has been started for this endpoint.")
})

var AdminRegistryPromotionCandidate = Type("AdminRegistryPromotionCandidate", func() {
	Attribute("id", String)
	Attribute("organization_id", String)
	Attribute("project_id", String)
	Attribute("mcp_server_id", String)
	Attribute("remote_mcp_server_id", String)
	Attribute("source_name", String)
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("curation_revision", String, func() { Format(FormatUUID) })
	Attribute("suggested_metadata", AdminRegistryPromotionSuggestions)
	Attribute("auth_discovery", AdminRegistryPromotionAuthDiscovery)
	Attribute("account_usage", AdminRegistryPromotionAccountUsage)
	Attribute("review", AdminRegistryPromotionReview)
	Attribute("review_allowed", Boolean, "Whether this endpoint can be reviewed. URLs that may carry tenant data cannot.")
	Attribute("source_revision", String)
	Attribute("eligible", Boolean)
	Attribute("can_override_eligibility", Boolean, "Whether staff may explicitly accept this candidate's health evidence.")
	Attribute("reason", String)
	Attribute("status", String)
	Attribute("observed_successes", Int64)
	Attribute("observed_failures", Int64)
	Attribute("observed_days", Int)
	Attribute("checked_at", String, func() { Format(FormatDateTime) })
	Attribute("registry_entry_id", String)
	Attribute("docs_status", String)
	Attribute("docs_url", String)
	Attribute("docs_revision", String)
	Attribute("docs_retry_allowed", Boolean)
	Attribute("docs_attempts", Int)
	Attribute("docs_last_attempt_at", String, func() { Format(FormatDateTime) })
	Attribute("docs_next_attempt_at", String, func() { Format(FormatDateTime) })
	Attribute("docs_queued_at", String, func() { Format(FormatDateTime) })
	Attribute("promoted_at", String, func() { Format(FormatDateTime) })
	Attribute("public_title", String)
	Attribute("delivery_error", String)
	Required("created_at", "curation_revision", "id", "organization_id", "project_id", "mcp_server_id", "remote_mcp_server_id", "source_revision", "eligible", "can_override_eligibility", "review_allowed", "reason", "status", "observed_successes", "observed_failures", "observed_days", "docs_revision", "docs_retry_allowed", "docs_attempts")
})

var AdminRegistryPromotionCandidates = Type("AdminRegistryPromotionCandidates", func() {
	Attribute("candidates", ArrayOf(AdminRegistryPromotionCandidate))
	Attribute("next_cursor", String)
	Required("candidates")
})

func promotionTarget() {
	Attribute("organization_id", String)
	Attribute("project_id", String, func() { Format(FormatUUID) })
	Attribute("mcp_server_id", String, func() { Format(FormatUUID) })
	Required("organization_id", "project_id", "mcp_server_id")
}

func registryPromotionDesign() {
	Method("listRegistryPromotionCandidates", func() {
		Description("List observed remote MCP candidates and durable promotion delivery status. Active excludes endpoints already in the catalogue; candidate includes all unpublished active entries. No source URLs or credentials are returned.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("cursor", String)
			Attribute("status", String, func() { Default("active"); Enum("active", "all", "candidate", "rejected", "archived", "promoted") })
			Attribute("limit", Int, func() { Default(25); Minimum(1); Maximum(50) })
		})
		Result(AdminRegistryPromotionCandidates)
		HTTP(func() {
			GET("/admin/registry.promotionCandidates")
			Param("cursor")
			Param("status")
			Param("limit")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminListRegistryPromotionCandidates")
		Meta("openapi:extension:x-speakeasy-name-override", "listRegistryPromotionCandidates")
	})
	Method("getRegistryPromotionCandidate", func() {
		Description("Refresh health evidence for one exact remote MCP deployment before staff review.")
		Payload(func() { security.AdminAuthPayload(); promotionTarget() })
		Result(AdminRegistryPromotionCandidate)
		HTTP(func() {
			POST("/admin/registry.promotionCandidate")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminGetRegistryPromotionCandidate")
		Meta("openapi:extension:x-speakeasy-name-override", "getRegistryPromotionCandidate")
	})
	Method("getRegistryPromotionReview", func() {
		Description("Read the MCP review for one exact remote MCP deployment's endpoint without gathering evidence or starting research.")
		Payload(func() { security.AdminAuthPayload(); promotionTarget() })
		Result(AdminRegistryPromotionReviewResult)
		HTTP(func() {
			POST("/admin/registry.promotionReview")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminGetRegistryPromotionReview")
		Meta("openapi:extension:x-speakeasy-name-override", "getRegistryPromotionReview")
	})
	Method("startRegistryPromotionReview", func() {
		Description("Gather evidence and start a research run for one exact remote MCP deployment's endpoint. The review runs in Speakeasy's own organization, which pays for it and receives its audit entries; nothing is recorded in the source account. A run already in progress is returned instead of starting another.")
		Payload(func() { security.AdminAuthPayload(); promotionTarget() })
		Result(AdminRegistryPromotionReviewResult)
		HTTP(func() {
			POST("/admin/registry.startPromotionReview")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminStartRegistryPromotionReview")
		Meta("openapi:extension:x-speakeasy-name-override", "startRegistryPromotionReview")
	})
	Method("promoteRegistryCandidate", func() {
		Description("Publish an explicitly reviewed public catalogue entry and queue setup documentation. Customer credentials are never copied.")
		Payload(func() {
			security.AdminAuthPayload()
			promotionTarget()
			Attribute("source_revision", String)
			Attribute("curation_revision", String, func() { Format(FormatUUID) })
			Attribute("public_name", String)
			Attribute("public_title", String, "Public title, at most 100 Unicode code points.")
			Attribute("public_description", String, "Public description, at most 100 Unicode code points.")
			Attribute("public_url", String)
			Attribute("documentation_url", String)
			Attribute("docs_slug", String)
			Attribute("auth_mode", String, func() { Enum("none", "oauth") })
			Attribute("retry_key", String)
			Attribute("public_metadata_confirmed", Boolean)
			Attribute("override_eligibility", Boolean, "Explicitly accept insufficient, failed, or unavailable health evidence. Source safety checks still apply.", func() { Default(false) })
			Required("curation_revision", "source_revision", "public_name", "public_title", "public_description", "public_url", "documentation_url", "docs_slug", "auth_mode", "retry_key", "public_metadata_confirmed")
		})
		Result(AdminRegistryPromotionCandidate)
		HTTP(func() { POST("/admin/registry.promoteCandidate"); Response(StatusOK) })
		Meta("openapi:operationId", "adminPromoteRegistryCandidate")
		Meta("openapi:extension:x-speakeasy-name-override", "promoteRegistryCandidate")
	})
	Method("curateRegistryPromotionCandidate", func() {
		Description("Reject, archive, or restore one exact unpublished catalogue candidate. Hidden candidates cannot be promoted until restored and refreshed.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String)
			Attribute("promotion_id", String, func() { Format(FormatUUID) })
			Attribute("curation_revision", String, func() { Format(FormatUUID) })
			Attribute("action", String, func() { Enum("reject", "archive", "restore") })
			Attribute("retry_key", String)
			Required("organization_id", "promotion_id", "curation_revision", "action", "retry_key")
		})
		Result(AdminRegistryPromotionCandidate)
		HTTP(func() { POST("/admin/registry.curatePromotionCandidate"); Response(StatusOK) })
		Meta("openapi:operationId", "adminCurateRegistryPromotionCandidate")
		Meta("openapi:extension:x-speakeasy-name-override", "curateRegistryPromotionCandidate")
	})

	Method("retryRegistryPromotionDocs", func() {
		Description("Retry a failed setup-guide request for one exact reviewed promotion without republishing the catalogue entry.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("organization_id", String)
			Attribute("promotion_id", String, func() { Format(FormatUUID) })
			Attribute("docs_revision", String, func() { Format(FormatUUID) })
			Attribute("retry_key", String)
			Required("organization_id", "promotion_id", "docs_revision", "retry_key")
		})
		Result(AdminRegistryPromotionCandidate)
		HTTP(func() { POST("/admin/registry.retryPromotionDocs"); Response(StatusOK) })
		Meta("openapi:operationId", "adminRetryRegistryPromotionDocs")
		Meta("openapi:extension:x-speakeasy-name-override", "retryRegistryPromotionDocs")
	})

}
