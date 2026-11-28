package admin

import (
	"github.com/speakeasy-api/gram/server/design/security"
	. "goa.design/goa/v3/dsl"
)

var AdminBlockUserStepResult = Type("AdminBlockUserStepResult", func() {
	Description("Outcome of one post-commit cache or external system step during a user block.")
	Attribute("status", String, "Step outcome: succeeded, partial, skipped, failed, or not_configured.")
	Attribute("detail", String, "Human-readable detail explaining skipped, partial, failed, or not_configured outcomes.")
	Required("status")
})

var AdminBlockUserResult = Type("AdminBlockUserResult", func() {
	Description("Summary of a staff block-user action, including committed database cascades and per-step external results.")
	Attribute("user_id", String, "Canonical ID of the blocked user.")
	Attribute("email", String, "Email address of the blocked user.")
	Attribute("workos_user_id", String, "Linked WorkOS user ID, if present.")
	Attribute("already_blacklisted", Boolean, "True if users.blacklisted_at was already set before this request.")
	Attribute("blacklisted_at", String, func() {
		Description("Timestamp when the user was blacklisted.")
		Format(FormatDateTime)
	})
	Attribute("disabled_organization_ids", ArrayOf(String), "IDs of organizations disabled because the blocked user was their sole remaining active non-staff member.")
	Attribute("revoked_api_key_count", Int, "Number of API keys created by the user that were soft-revoked.")
	Attribute("revoked_sign_in_count", Int, "Number of recorded sign-ins revoked.")
	Attribute("revoked_user_session_count", Int, "Number of hosted user sessions soft-revoked.")
	Attribute("revoked_user_session_consent_count", Int, "Number of remembered user session consents withdrawn.")
	Attribute("revoked_user_session_client_count", Int, "Number of user-owned hosted OAuth clients soft-revoked.")
	Attribute("revoked_platform_mcp_connection_count", Int, "Number of Platform MCP connections revoked.")
	Attribute("revoked_platform_mcp_session_count", Int, "Number of Platform MCP sessions revoked.")
	Attribute("revoked_platform_mcp_oauth_client_count", Int, "Number of user-owned Platform MCP OAuth clients revoked.")
	Attribute("revoked_channels_oauth_grant_count", Int, "Number of Channels OAuth grants revoked.")
	Attribute("revoked_channels_oauth_client_count", Int, "Number of user-owned Channels OAuth clients revoked.")
	Attribute("session_cache", AdminBlockUserStepResult, "Immediate Redis dashboard epoch and JTI revocation push outcome.")
	Attribute("workos_metadata", AdminBlockUserStepResult, "Outcome of setting WorkOS user metadata blacklisted=true.")
	Attribute("workos_sessions", AdminBlockUserStepResult, "Outcome of listing and revoking WorkOS AuthKit sessions.")
	Attribute("workos_sessions_revoked_count", Int, "Number of WorkOS AuthKit sessions revoked.")
	Attribute("workos_radar", AdminBlockUserStepResult, "Outcome of adding the user's email to the WorkOS Radar email blocklist.")
	Attribute("registry_disable", AdminBlockUserStepResult, "Outcome of disabling the user in the registry service.")
	Attribute("warnings", ArrayOf(String), "Non-fatal warnings or partial failure details from post-commit external steps.")
	Required(
		"user_id",
		"email",
		"already_blacklisted",
		"blacklisted_at",
		"disabled_organization_ids",
		"revoked_api_key_count",
		"revoked_sign_in_count",
		"revoked_user_session_count",
		"revoked_user_session_consent_count",
		"revoked_user_session_client_count",
		"revoked_platform_mcp_connection_count",
		"revoked_platform_mcp_session_count",
		"revoked_platform_mcp_oauth_client_count",
		"revoked_channels_oauth_grant_count",
		"revoked_channels_oauth_client_count",
		"session_cache",
		"workos_metadata",
		"workos_sessions",
		"workos_sessions_revoked_count",
		"workos_radar",
		"registry_disable",
		"warnings",
	)
})

func blockUserMethods() {
	Method("blockUser", func() {
		Description("Staff-only action that blacklists a user, atomically cascades all database revocations and sole-member organization disables, invalidates session caches, and executes best-effort WorkOS revocation steps with structured partial-failure reporting.")
		Payload(func() {
			security.AdminAuthPayload()
			Attribute("user_id", String, "Canonical Speakeasy user ID to block.", func() {
				MinLength(1)
				MaxLength(255)
			})
			Attribute("reason", String, "Optional staff reason recorded in audit logs.", func() {
				MaxLength(500)
			})
			Required("user_id")
			Meta("openapi:typename", "BlockUserRequestBody")
		})
		Result(AdminBlockUserResult)
		HTTP(func() {
			POST("/admin/users.block")
			Response(StatusOK)
		})
		Meta("openapi:operationId", "adminBlockUser")
	})
}
