package admin

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
)

// MCP parity: omitted. Organization encryption transitions are rare,
// staff-only platform steps, and finalize irreversibly destroys the
// platform-key copies of an organization's values, so they stay deliberate
// dashboard actions behind a typed confirmation rather than agent tools.

var transitionStates = []any{"platform_key", "dual_write", "org_kms"}

// transitionListValues selects which organizations the transition list
// reports: those with values for the use case, or every organization.
var transitionListValues = []any{"present", "all"}

// transitionListSorts orders the transition list: most recently written
// values first, or oldest organization first.
var transitionListSorts = []any{"last_written", "oldest"}

var AdminOrganizationEncryptionConfig = Type("AdminOrganizationEncryptionConfig", func() {
	Description("An organization's encryption configuration: who controls its key, and whether the key is usable.")
	Required("mode", "state")

	Attribute("mode", String, "Who controls the organization's key.", func() { Enum("speakeasy_managed", "customer_managed") })
	Attribute("state", String, "Whether the key is usable: active and switching are, suspended is not.", func() { Enum("active", "switching", "suspended") })
	Attribute("switch_started_at", String, "When the switch to another key started. Absent unless one is in flight.", func() { Format(FormatDateTime) })
	Attribute("suspended_at", String, "When the configuration was suspended because its key became unusable.", func() { Format(FormatDateTime) })
	Attribute("suspended_reason", String, "A short provider error summary for the suspension.")
	Attribute("last_check_at", String, "When the key's health was last checked.", func() { Format(FormatDateTime) })
	Attribute("last_check_error", String, "A short provider error summary from the last failed health check.")
})

var AdminOrganizationEncryptionUseCase = Type("AdminOrganizationEncryptionUseCase", func() {
	Description("Where one organization stands in moving one use case's values from the platform key to its own key. Counts are read live.")
	Required("use_case", "state", "generation", "waiting_values", "unreadable_values", "unattributed_values", "platform_copies", "finalize_blockers")

	Attribute("use_case", String, "The registered use case, such as oauth-client-secrets.")
	Attribute("state", String, "The transition state.", func() { Enum(transitionStates...) })
	Attribute("generation", Int64, "Increases on every state change; 0 while the organization has never been enrolled.")
	Attribute("state_changed_at", String, "When the state was entered. Absent while the organization has never been enrolled.", func() { Format(FormatDateTime) })
	Attribute("platform_copies_cleared_at", String, "When the platform-key copies were destroyed after finalize. Absent until clearing completes.", func() { Format(FormatDateTime) })
	Attribute("waiting_values", Int64, "Values that still wait for an organization-key copy.")
	Attribute("unreadable_values", Int64, "Values whose platform-key copy does not decrypt. Finalize destroys them with the rest.")
	Attribute("unattributed_values", Int64, "Values stored without an organization they can be attributed to. They block finalize.")
	Attribute("platform_copies", Int64, "Platform-key copies still stored.")
	Attribute("last_backfill_started_at", String, "When the last backfill run started.", func() { Format(FormatDateTime) })
	Attribute("last_backfill_completed_at", String, "When the last backfill run ended. Absent while it runs.", func() { Format(FormatDateTime) })
	Attribute("last_backfill_converted", Int64, "Values the last backfill run gave an organization-key copy.")
	Attribute("last_backfill_remaining", Int64, "Values still waiting when the last backfill run ended.")
	Attribute("last_backfill_unreadable", Int64, "Values recorded as unreadable when the last backfill run ended.")
	Attribute("last_backfill_error", String, "Why the last backfill run stopped early.")
	Attribute("last_verified_at", String, "When the last clean verification pass started.", func() { Format(FormatDateTime) })
	Attribute("finalize_blockers", ArrayOf(String), "Why finalize would be refused now, empty when it would proceed. Advisory: finalize checks again when it runs.")
})

var AdminOrganizationEncryption = Type("AdminOrganizationEncryption", func() {
	Description("An organization's encryption configuration and, for each use case that moves to organization keys, its transition state.")
	Required("organization_id", "use_cases", "minimum_dual_write_seconds", "finalize_confirmation", "runs_available")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("config", AdminOrganizationEncryptionConfig, "The encryption configuration. Absent until the organization's key is provisioned, which enroll does.")
	Attribute("use_cases", ArrayOf(AdminOrganizationEncryptionUseCase), "One entry per use case that moves from the platform key to organization keys.")
	Attribute("minimum_dual_write_seconds", Int64, "How long a use case must be in dual_write before finalize is allowed.")
	Attribute("finalize_confirmation", String, "The phrase finalize requires as its confirmation.")
	Attribute("runs_available", Boolean, "Whether this server can start backfill and clearing runs. When false, finalize leaves clearing to the hourly background run.")
})

var AdminOrganizationEncryptionTransitionRow = Type("AdminOrganizationEncryptionTransitionRow", func() {
	Description("One organization's transition state for a use case, from stored state only.")
	Required("organization_id", "name", "slug", "disabled", "state", "value_count")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("name", String, "The organization name.")
	Attribute("slug", String, "The organization slug.")
	Attribute("disabled", Boolean, "Whether the organization is disabled.")
	Attribute("state", String, "The transition state.", func() { Enum(transitionStates...) })
	Attribute("state_changed_at", String, "When the state was entered.", func() { Format(FormatDateTime) })
	Attribute("platform_copies_cleared_at", String, "When the platform-key copies were destroyed after finalize.", func() { Format(FormatDateTime) })
	Attribute("last_backfill_started_at", String, "When the last backfill run started.", func() { Format(FormatDateTime) })
	Attribute("last_backfill_completed_at", String, "When the last backfill run ended.", func() { Format(FormatDateTime) })
	Attribute("last_backfill_remaining", Int64, "Values still waiting when the last backfill run ended.")
	Attribute("last_backfill_unreadable", Int64, "Values recorded as unreadable when the last backfill run ended.")
	Attribute("last_backfill_error", String, "Why the last backfill run stopped early.")
	Attribute("last_verified_at", String, "When the last clean verification pass started.", func() { Format(FormatDateTime) })
	Attribute("value_count", Int64, "How many values the organization stores for the use case under either key, soft-deleted rows included. A row storing values in two columns counts once in each.")
	Attribute("last_written_at", String, "The latest updated_at of a row storing one of the organization's values. A change that updates the row's updated_at moves it, such as a session's token refresh or an OpenRouter key's limit change; a write that leaves updated_at alone, such as a backfill seal, does not. Absent when the organization has none.", func() { Format(FormatDateTime) })
})

var AdminOrganizationEncryptionStateCount = Type("AdminOrganizationEncryptionStateCount", func() {
	Required("state", "organizations")

	Attribute("state", String, "The transition state.", func() { Enum(transitionStates...) })
	Attribute("organizations", Int64, "Organizations in the state.")
})

var AdminOrganizationEncryptionTransitionList = Type("AdminOrganizationEncryptionTransitionList", func() {
	Required("use_case", "use_cases", "organizations", "state_counts", "has_more")

	Attribute("use_case", String, "The use case listed.")
	Attribute("use_cases", ArrayOf(String), "Every use case that moves from the platform key to organization keys.")
	Attribute("organizations", ArrayOf(AdminOrganizationEncryptionTransitionRow), "The page of organizations, in the requested order.")
	Attribute("state_counts", ArrayOf(AdminOrganizationEncryptionStateCount), "How many organizations the values filter keeps are in each state, across every page.")
	Attribute("has_more", Boolean, "Whether another page follows.")
})

var AdminOrganizationEncryptionFinalizeResult = Type("AdminOrganizationEncryptionFinalizeResult", func() {
	Required("use_case", "clearing_started")

	Attribute("use_case", AdminOrganizationEncryptionUseCase, "The use case after finalize, now in org_kms.")
	Attribute("clearing_started", Boolean, "Whether a run destroying the platform-key copies was started. When false, the hourly background run finishes clearing, or an administrator starts a run.")
})

// organizationEncryptionUseCasePayload is the target of every per-organization
// encryption action.
func organizationEncryptionUseCasePayload(typename string) {
	security.AdminAuthPayload()
	Required("organization_id", "use_case")

	Attribute("organization_id", String, "The organization ID.")
	Attribute("use_case", String, "The use case, such as oauth-client-secrets.")

	// Goa's OpenAPI emitter deduplicates request bodies by shape, so without
	// an explicit typename these bodies take one another's names in the
	// generated SDK.
	Meta("openapi:typename", typename)
}

func organizationEncryptionMethods() {
	Method("getOrganizationEncryption", func() {
		Description("Reports an organization's encryption configuration and, for each use case that moves to organization keys, its transition state with live counts.")

		Payload(func() {
			security.AdminAuthPayload()
			Required("organization_id")

			Attribute("organization_id", String, "The organization ID or slug.")
		})

		Result(AdminOrganizationEncryption)
		declareUnavailable()

		HTTP(func() {
			GET("/admin/organizationEncryption.get")
			Param("organization_id")
			Response(StatusOK)
			declareUnavailableResponse()
		})

		Meta("openapi:operationId", "adminGetOrganizationEncryption")
		Meta("openapi:extension:x-speakeasy-name-override", "getOrganizationEncryption")
	})

	Method("listOrganizationEncryptionTransitions", func() {
		Description("Lists organizations by transition state for one use case. Organizations never enrolled are in platform_key. By default it lists only organizations with values for the use case, most recently written first.")

		Payload(func() {
			security.AdminAuthPayload()

			Attribute("use_case", String, "The use case, such as oauth-client-secrets. Absent lists the first use case that moves to organization keys.")
			Attribute("state", String, "Only organizations in this state.", func() { Enum(transitionStates...) })
			Attribute("values", String, "present lists only organizations with at least one value for the use case under either key, soft-deleted rows included. all lists every organization.", func() {
				Enum(transitionListValues...)
				Default("present")
			})
			Attribute("sort", String, "last_written lists the most recently written organizations first, then organizations without values oldest first. oldest lists the oldest organizations first. The list pages by offset, so an organization whose last write changes between page requests can repeat or be skipped.", func() {
				Enum(transitionListSorts...)
				Default("last_written")
			})
			Attribute("limit", Int, "Organizations per page.", func() {
				Minimum(1)
				Maximum(200)
				Default(50)
			})
			Attribute("offset", Int, "Organizations to skip.", func() {
				Minimum(0)
				Default(0)
			})
		})

		Result(AdminOrganizationEncryptionTransitionList)
		declareUnavailable()

		HTTP(func() {
			GET("/admin/organizationEncryption.listTransitions")
			Param("use_case")
			Param("state")
			Param("values")
			Param("sort")
			Param("limit")
			Param("offset")
			Response(StatusOK)
			declareUnavailableResponse()
		})

		Meta("openapi:operationId", "adminListOrganizationEncryptionTransitions")
		Meta("openapi:extension:x-speakeasy-name-override", "listOrganizationEncryptionTransitions")
	})

	Method("enrollOrganizationEncryption", func() {
		Description("Moves one organization's use case from platform_key to dual_write. Provisions the organization's key and data key first and changes nothing if that fails. Each enroll can mint a KMS key, so enroll several organizations one at a time.")

		Payload(func() { organizationEncryptionUseCasePayload("EnrollOrganizationEncryptionRequestBody") })

		Result(AdminOrganizationEncryptionUseCase)
		declareUnavailable()

		HTTP(func() {
			POST("/admin/organizationEncryption.enroll")
			Response(StatusOK)
			declareUnavailableResponse()
		})

		Meta("openapi:operationId", "adminEnrollOrganizationEncryption")
		Meta("openapi:extension:x-speakeasy-name-override", "enrollOrganizationEncryption")
	})

	Method("rollbackOrganizationEncryption", func() {
		Description("Moves one organization's use case from dual_write back to platform_key. Both copies still exist in dual_write, so nothing is lost. Not available once the use case is in org_kms.")

		Payload(func() { organizationEncryptionUseCasePayload("RollbackOrganizationEncryptionRequestBody") })

		Result(AdminOrganizationEncryptionUseCase)
		declareUnavailable()

		HTTP(func() {
			POST("/admin/organizationEncryption.rollback")
			Response(StatusOK)
			declareUnavailableResponse()
		})

		Meta("openapi:operationId", "adminRollbackOrganizationEncryption")
		Meta("openapi:extension:x-speakeasy-name-override", "rollbackOrganizationEncryption")
	})

	Method("runOrganizationEncryption", func() {
		Description("Starts a background run for one organization's use case: in dual_write it converts waiting values and verifies both copies, and in org_kms it finishes destroying the platform-key copies after finalize. Joins a run already in progress. Returns at once with the current state.")

		Payload(func() { organizationEncryptionUseCasePayload("RunOrganizationEncryptionRequestBody") })

		Result(AdminOrganizationEncryptionUseCase)
		declareUnavailable()

		HTTP(func() {
			POST("/admin/organizationEncryption.run")
			Response(StatusOK)
			declareUnavailableResponse()
		})

		Meta("openapi:operationId", "adminRunOrganizationEncryption")
		Meta("openapi:extension:x-speakeasy-name-override", "runOrganizationEncryption")
	})

	Method("finalizeOrganizationEncryption", func() {
		Description("Moves one organization's use case from dual_write to org_kms, then starts a run that destroys its platform-key copies, including every unreadable value. Irreversible: rollback is no longer possible afterwards. Refused unless no value waits for an organization-key copy, every value is attributed to the organization, a clean verification ran since the last backfill, the use case spent the minimum time in dual_write, the confirmation matches, and the acknowledged unreadable count matches the count when finalize runs.")

		Payload(func() {
			organizationEncryptionUseCasePayload("FinalizeOrganizationEncryptionRequestBody")
			Required("confirmation", "acknowledged_unreadable_values")

			Attribute("confirmation", String, "The finalize_confirmation phrase, typed by the administrator.")
			Attribute("acknowledged_unreadable_values", Int64, "The number of unreadable values the administrator acknowledged will be destroyed.", func() { Minimum(0) })
		})

		Result(AdminOrganizationEncryptionFinalizeResult)
		declareUnavailable()

		HTTP(func() {
			POST("/admin/organizationEncryption.finalize")
			Response(StatusOK)
			declareUnavailableResponse()
		})

		Meta("openapi:operationId", "adminFinalizeOrganizationEncryption")
		Meta("openapi:extension:x-speakeasy-name-override", "finalizeOrganizationEncryption")
	})
}
