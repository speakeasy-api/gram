package admin

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var AdminRegistryPromotionCandidate = Type("AdminRegistryPromotionCandidate", func() {
	Attribute("id", String)
	Attribute("organization_id", String)
	Attribute("project_id", String)
	Attribute("mcp_server_id", String)
	Attribute("remote_mcp_server_id", String)
	Attribute("source_name", String)
	Attribute("source_revision", String)
	Attribute("eligible", Boolean)
	Attribute("reason", String)
	Attribute("status", String)
	Attribute("observed_successes", Int64)
	Attribute("observed_failures", Int64)
	Attribute("observed_days", Int)
	Attribute("checked_at", String)
	Attribute("registry_entry_id", String)
	Attribute("docs_status", String)
	Attribute("docs_url", String)
	Attribute("docs_revision", String)
	Attribute("docs_retry_allowed", Boolean)
	Attribute("docs_attempts", Int)
	Attribute("docs_last_attempt_at", String)
	Attribute("docs_next_attempt_at", String)
	Attribute("docs_queued_at", String)
	Attribute("promoted_at", String)
	Attribute("public_title", String)
	Attribute("delivery_error", String)
	Required("id", "organization_id", "project_id", "mcp_server_id", "remote_mcp_server_id", "source_revision", "eligible", "reason", "status", "observed_successes", "observed_failures", "observed_days", "docs_revision", "docs_retry_allowed", "docs_attempts")
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
		Description("List observed remote MCP candidates and durable promotion delivery status. No source URLs or credentials are returned.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("cursor", String)
			Attribute("limit", Int, func() { Default(25); Minimum(1); Maximum(50) })
		})
		Result(AdminRegistryPromotionCandidates)
		HTTP(func() {
			GET("/admin/registry.promotionCandidates")
			Param("cursor")
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
			GET("/admin/registry.promotionCandidate")
			Param("organization_id")
			Param("project_id")
			Param("mcp_server_id")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminGetRegistryPromotionCandidate")
		Meta("openapi:extension:x-speakeasy-name-override", "getRegistryPromotionCandidate")
	})
	Method("promoteRegistryCandidate", func() {
		Description("Publish an explicitly reviewed public catalogue entry and queue setup documentation. Customer credentials are never copied.")
		Payload(func() {
			security.AdminAuthPayload()
			promotionTarget()
			Attribute("source_revision", String)
			Attribute("public_name", String)
			Attribute("public_title", String)
			Attribute("public_description", String)
			Attribute("public_url", String)
			Attribute("documentation_url", String)
			Attribute("docs_slug", String)
			Attribute("auth_mode", String)
			Attribute("retry_key", String)
			Attribute("public_metadata_confirmed", Boolean)
			Required("source_revision", "public_name", "public_title", "public_description", "public_url", "documentation_url", "docs_slug", "auth_mode", "retry_key", "public_metadata_confirmed")
		})
		Result(AdminRegistryPromotionCandidate)
		HTTP(func() { POST("/admin/registry.promoteCandidate"); Response(StatusOK) })
		Meta("openapi:operationId", "adminPromoteRegistryCandidate")
		Meta("openapi:extension:x-speakeasy-name-override", "promoteRegistryCandidate")
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
