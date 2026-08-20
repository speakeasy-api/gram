package productfeatures

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("features", func() {
	Description("Manage product level feature controls.")

	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("getProductFeatures", func() {
		Description("Get the current state of all product feature flags.")

		Payload(func() {
			Attribute("organization_id", String, "Organization whose product features to read.")
			Required("organization_id")
			security.SessionPayload()
		})

		Result(func() {
			Attribute("logs_enabled", Boolean, "Whether logging is enabled")
			Attribute("tool_io_logs_enabled", Boolean, "Whether tool I/O logging is enabled")
			Attribute("session_capture_enabled", Boolean, "Whether Claude Code session capture is enabled")
			Attribute("authz_challenge_logging_enabled", Boolean, "Whether authz challenge logging to ClickHouse is enabled")
			Attribute("sso_enabled", Boolean, "Whether SSO setup is enabled for the organization")
			Attribute("scim_enabled", Boolean, "Whether SCIM/directory sync setup is enabled for the organization")
			Attribute("hooks_browser_login_enabled", Boolean, "Whether generated hook plugins may mint per-user keys via the interactive browser login")
			Attribute("hooks_fail_open_enabled", Boolean, "Whether hooks fail open when the Speakeasy control plane is unreachable or erroring — blocking policies are not enforced for the duration of the outage")
			Attribute("custom_model_keys_enabled", Boolean, "Whether the organization can supply its own model provider API keys (BYOK)")
			Attribute("skills_enabled", Boolean, "Whether the Skills page is enabled for the organization")
			Attribute("skill_capture_metadata_only", Boolean, "Whether skill capture stores activation metadata without requesting manifest content")
			Attribute("ai_platform_push_integrations_enabled", Boolean, "Whether the organization can provision push integrations for AI platforms")
			Attribute("platform_mcp_enabled", Boolean, "Whether the organization can use the Gram Platform MCP capability")
			Attribute("customer_managed_encryption_keys_enabled", Boolean, "Whether the organization can manage the external credentials and cloud KMS keys backing customer-managed encryption")
			Attribute("remote_session_auto_refresh_enabled", Boolean, "Whether consent screens expose automatic remote-session refresh for the organization")
			Attribute("remote_session_auto_refresh_enforced_enabled", Boolean, "Whether automatic remote-session refresh is enforced as the organization default: forced on for every user, shown locked on consent screens, and applied by the keepalive regardless of per-session preference")
			Attribute("consent_tool_filtering_enabled", Boolean, "Whether MCP consent screens offer the tool filtering picker for the organization")
			Attribute("session_portability_enabled", Boolean, "Whether agent session portability is enabled for the organization: session sharing links, move reporting with lineage, and picker title enrichment via the device agent")
			Attribute("device_agent", Boolean, "Whether the organization uses the device agent (any device has polled agent.getPlugins). Derived from device-agent syncs, not an admin-settable feature.")
			Required("logs_enabled", "tool_io_logs_enabled", "session_capture_enabled", "authz_challenge_logging_enabled", "sso_enabled", "scim_enabled", "hooks_browser_login_enabled", "hooks_fail_open_enabled", "custom_model_keys_enabled", "skills_enabled", "skill_capture_metadata_only", "ai_platform_push_integrations_enabled", "platform_mcp_enabled", "customer_managed_encryption_keys_enabled", "remote_session_auto_refresh_enabled", "remote_session_auto_refresh_enforced_enabled", "consent_tool_filtering_enabled", "session_portability_enabled", "device_agent")
		})

		HTTP(func() {
			GET("/rpc/productFeatures.get")
			Param("organization_id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getProductFeatures")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ProductFeatures"}`)
	})

	Method("setProductFeature", func() {
		Description("Enable or disable an organization feature flag.")

		Payload(func() {
			Attribute("organization_id", String, "Organization whose product feature to update.")
			Attribute("feature_name", String, "Name of the feature to update", func() {
				MaxLength(60)
				Enum("logs", "tool_io_logs", "session_capture", "authz_challenge_logging", "sso", "scim", "hooks_browser_login", "hooks_fail_open", "custom_model_keys", "skills", "skill_capture_metadata_only", "ai_platform_push_integrations", "platform_mcp", "customer_managed_encryption_keys", "remote_session_auto_refresh", "remote_session_auto_refresh_enforced", "consent_tool_filtering", "session_portability")
			})
			Attribute("enabled", Boolean, "Whether the feature should be enabled")
			Required("organization_id", "feature_name", "enabled")

			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/productFeatures.set")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setProductFeature")
		Meta("openapi:extension:x-speakeasy-name-override", "set")
	})

	Method("setRemoteSessionAutoRefreshPolicy", func() {
		Description("Set the organization policy for automatic remote-session refresh.")

		Payload(func() {
			Attribute("organization_id", String, "Organization whose automatic remote-session refresh policy to update.")
			Attribute("policy", String, "Organization policy for automatic remote-session refresh", func() {
				Enum("disabled", "user_controlled", "enforced")
			})
			Required("organization_id", "policy")

			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/productFeatures.setRemoteSessionAutoRefreshPolicy")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setRemoteSessionAutoRefreshPolicy")
		Meta("openapi:extension:x-speakeasy-name-override", "setRemoteSessionAutoRefreshPolicy")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetRemoteSessionAutoRefreshPolicy"}`)
	})
})
