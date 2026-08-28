package platformmcp

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var OnboardingState = Type("PlatformMCPOnboardingState", func() {
	Description("Safe, session-authenticated Platform MCP onboarding projection. It contains no provider URLs, credentials, OAuth values, setup handoffs, or internal resource identifiers.")
	Attribute("enabled", Boolean, "Whether the active organization currently has the Platform MCP product feature enabled.")
	Attribute("stage", String, "Current server-derived onboarding stage.", func() {
		Enum("not_started", "install_instructions", "authorized", "connection_ready")
	})
	Attribute("mcp_url", String, "Canonical Platform MCP endpoint URL supplied by the server.")
	Attribute("workflow_active", Boolean, "Whether the authenticated user has an active onboarding workflow in this organization.")
	Attribute("organization_setup_complete", Boolean, "Whether the organization already has a durable Platform MCP value outcome or an attached distribution.")
	Attribute("client_family", String, "Selected manual-install client family when a workflow is active.")
	Attribute("agent_configuration_copied", Boolean, "Whether the user copied Platform MCP configuration or completed an equivalent supported agent-setup action in this workflow.")
	Attribute("connection_authorized", Boolean, "Whether the authenticated user has a current authorized Platform MCP connection.")
	Attribute("connection_auth_state", String, "Bounded authorization state for the authenticated user's latest Platform MCP connection.", func() {
		Enum("not_connected", "active", "reauthorization_required")
	})
	Attribute("reauthorization_reason", String, "Bounded reason why interactive Platform MCP authorization is required again.", func() {
		Enum("", "idle_expired", "authorization_expired", "refresh_invalidated", "authorization_changed", "revoked", "security_reset")
	})
	Attribute("connection_ready", Boolean, "Whether the authenticated user's current connection has completed authenticated discovery.")
	Attribute("catalog_explored", Boolean, "Whether the active workflow observed a successful reviewed MCP Catalog search through Platform MCP.")
	Attribute("selected_project_name", String, "Display name of the workflow-bound project, when one has been selected.")
	Attribute("selected_project_slug", String, "Slug of the workflow-bound project, when one has been selected.")
	Attribute("registration_complete", Boolean, "Whether the server-owned local candidate is registered for the selected project.")
	Attribute("readiness_state", String, "Bounded current readiness state for the selected registration.", func() {
		Enum("", "ready", "needs_provider_setup", "needs_gram_authorization", "needs_configuration", "auth_failed", "unreachable", "unsupported", "unauthorized", "guide_unavailable", "degraded")
	})
	Attribute("readiness_freshness", String, "Whether selected-registration readiness is currently fresh.", func() {
		Enum("", "fresh", "stale", "unavailable")
	})
	Attribute("distribution_state", String, "Bounded lifecycle state for the selected MCP's existing-Default-plugin distribution.", func() {
		Enum("", "attached", "removed")
	})
	Attribute("distribution_attached", Boolean, "Whether the selected MCP is currently attached to the selected project's existing Default plugin.")
	Attribute("distribution_tool_succeeded", Boolean, "Whether Platform MCP successfully added the workflow-bound MCP to the selected project's existing Default plugin.")
	Attribute("readiness_verified", Boolean, "Whether Platform MCP observed a successful ready status check for the workflow-bound MCP.")
	Attribute("distribution_publication_state", String, "Bounded package-publication state for the selected project's Default plugin after a distribution change.", func() {
		Enum("", "pending", "current", "repair_required")
	})
	Attribute("selected_use_verified", Boolean, "Whether the server observed a successful normal Remote MCP tool call for the currently attached selected distribution version.")
	Attribute("distribution_expected_version", String, "Opaque server-issued version token required by distribute, remove, or publication-repair mutations. It binds the selected project and authenticated user without exposing internal IDs or counters.")
	Attribute("repair_action", String, "A bounded next action when the workflow cannot progress.", func() {
		Enum("", "enable_platform_mcp", "authorize_platform_mcp", "select_project", "start_setup", "continue_dashboard_setup", "retry_readiness", "retry_registration", "contact_support", "distribute_to_default_plugin", "repair_publication")
	})
	Required("enabled", "stage", "mcp_url", "workflow_active", "organization_setup_complete", "client_family", "agent_configuration_copied", "connection_authorized", "connection_auth_state", "reauthorization_reason", "connection_ready", "catalog_explored", "selected_project_name", "selected_project_slug", "registration_complete", "readiness_state", "readiness_freshness", "distribution_state", "distribution_attached", "distribution_tool_succeeded", "readiness_verified", "distribution_publication_state", "selected_use_verified", "distribution_expected_version", "repair_action")
})

var OnboardingSetupHandoff = Type("PlatformMCPOnboardingSetupHandoff", func() {
	Description("Secure setup continuation returned only to the immediate session-authenticated dashboard caller. Browser Catalogue entries return a server-owned Inspect URL; the local synthetic fixture returns a one-time handoff. Neither is part of the onboarding projection.")
	Attribute("handoff", String, "One-time opaque local-fixture setup handoff.")
	Attribute("dashboard_setup_url", String, "Server-owned same-origin dashboard Inspect URL for the persisted browser Catalogue registration.")
})

var _ = Service("platformMcp", func() {
	Description("Session-authenticated onboarding and lifecycle projection for the organization-level Gram Platform MCP.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("getOnboarding", func() {
		Description("Get the current user's safe Platform MCP onboarding projection for the active organization.")
		Payload(func() { security.SessionPayload() })
		Result(OnboardingState)
		HTTP(func() {
			GET("/rpc/platformMcp.getOnboarding")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getPlatformMCPOnboarding")
		Meta("openapi:extension:x-speakeasy-name-override", "getOnboarding")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "PlatformMCPOnboarding"}`)
	})

	Method("startOnboarding", func() {
		Description("Create or resume the current user's durable Platform MCP onboarding workflow.")
		Payload(func() {
			Attribute("source_surface", String, "Bounded dashboard surface that opened setup.", func() {
				Enum("platform_mcp_settings", "organization_setup", "platform_plugins", "sidebar_footer", "sources_empty", "project_overview_zero_data", "organization_home")
			})
			security.SessionPayload()
		})
		Result(OnboardingState)
		HTTP(func() {
			POST("/rpc/platformMcp.startOnboarding")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "startPlatformMCPOnboarding")
		Meta("openapi:extension:x-speakeasy-name-override", "startOnboarding")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "StartPlatformMCPOnboarding"}`)
	})

	Method("recordDashboardCtaEvent", func() {
		Description("Record a bounded Platform MCP dashboard CTA impression, selection, or dismissal.")
		Payload(func() {
			Attribute("action", String, "CTA action.", func() { Enum("impression", "selected", "dismissed") })
			Attribute("surface", String, "CTA surface.", func() {
				Enum("sidebar_footer", "sources_empty", "project_overview_zero_data", "organization_home")
			})
			Required("action", "surface")
			security.SessionPayload()
		})
		HTTP(func() {
			POST("/rpc/platformMcp.recordDashboardCtaEvent")
			security.SessionHeader()
			Response(StatusNoContent)
		})
		Meta("openapi:operationId", "recordPlatformMCPDashboardCtaEvent")
		Meta("openapi:extension:x-speakeasy-name-override", "recordDashboardCtaEvent")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RecordPlatformMCPDashboardCtaEvent"}`)
	})

	Method("recordInstallIntent", func() {
		Description("Record a selected manual-install client family for the current user's Platform MCP workflow.")
		Payload(func() {
			Attribute("client_family", String, "Manual-install client family.", func() {
				Enum("claude_code", "claude_cowork", "codex", "cursor", "opencode", "other")
			})
			Required("client_family")
			security.SessionPayload()
		})
		Result(OnboardingState)
		HTTP(func() {
			POST("/rpc/platformMcp.recordInstallIntent")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "recordPlatformMCPInstallIntent")
		Meta("openapi:extension:x-speakeasy-name-override", "recordInstallIntent")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RecordPlatformMCPInstallIntent"}`)
	})

	Method("recordAgentConfigurationCopied", func() {
		Description("Record that the user copied the displayed Platform MCP configuration or completed an equivalent supported agent-setup action.")
		Payload(func() { security.SessionPayload() })
		Result(OnboardingState)
		HTTP(func() {
			POST("/rpc/platformMcp.recordAgentConfigurationCopied")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "recordPlatformMCPAgentConfigurationCopied")
		Meta("openapi:extension:x-speakeasy-name-override", "recordAgentConfigurationCopied")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RecordPlatformMCPAgentConfigurationCopied"}`)
	})

	Method("startOnboardingSetup", func() {
		Description("Return the secure setup continuation for the workflow-bound registration. Browser Catalogue registrations return their existing same-origin dashboard Inspect page; the local fixture returns a one-time handoff for its synthetic provider setup endpoint.")
		Payload(func() { security.SessionPayload() })
		Result(OnboardingSetupHandoff)
		HTTP(func() {
			POST("/rpc/platformMcp.startOnboardingSetup")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "startPlatformMCPOnboardingSetup")
		Meta("openapi:extension:x-speakeasy-name-override", "startOnboardingSetup")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "StartPlatformMCPOnboardingSetup"}`)
	})

	Method("recheckOnboardingReadiness", func() {
		Description("Force a rate-limited authenticated readiness recheck for the workflow-bound local registration.")
		Payload(func() { security.SessionPayload() })
		Result(OnboardingState)
		HTTP(func() {
			POST("/rpc/platformMcp.recheckOnboardingReadiness")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "recheckPlatformMCPOnboardingReadiness")
		Meta("openapi:extension:x-speakeasy-name-override", "recheckOnboardingReadiness")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RecheckPlatformMCPOnboardingReadiness"}`)
	})

	Method("distributeOnboardingCandidate", func() {
		Description("Attach the workflow-bound ready local MCP to the selected project's existing Default plugin. The caller supplies only the selected project slug and its opaque server-issued version token.")
		Payload(func() {
			Attribute("project_slug", String, "Exact workflow-selected project slug.")
			Attribute("expected_version", String, "Opaque current distribution version token from getOnboarding.")
			Required("project_slug", "expected_version")
			security.SessionPayload()
		})
		Result(OnboardingState)
		HTTP(func() {
			POST("/rpc/platformMcp.distributeOnboardingCandidate")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "distributePlatformMCPOnboardingCandidate")
		Meta("openapi:extension:x-speakeasy-name-override", "distributeOnboardingCandidate")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DistributePlatformMCPOnboardingCandidate"}`)
	})

	Method("removeOnboardingDistribution", func() {
		Description("Remove only the workflow-bound MCP from the selected project's existing Default plugin. Registration, readiness, connection, and prior evidence remain intact.")
		Payload(func() {
			Attribute("project_slug", String, "Exact workflow-selected project slug.")
			Attribute("expected_version", String, "Opaque current distribution version token from getOnboarding.")
			Required("project_slug", "expected_version")
			security.SessionPayload()
		})
		Result(OnboardingState)
		HTTP(func() {
			POST("/rpc/platformMcp.removeOnboardingDistribution")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "removePlatformMCPOnboardingDistribution")
		Meta("openapi:extension:x-speakeasy-name-override", "removeOnboardingDistribution")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemovePlatformMCPOnboardingDistribution"}`)
	})

	Method("repairOnboardingPublication", func() {
		Description("Retry in-memory local package publication for the workflow-bound distribution without changing its attachment or version.")
		Payload(func() {
			Attribute("project_slug", String, "Exact workflow-selected project slug.")
			Attribute("expected_version", String, "Opaque current distribution version token from getOnboarding.")
			Required("project_slug", "expected_version")
			security.SessionPayload()
		})
		Result(OnboardingState)
		HTTP(func() {
			POST("/rpc/platformMcp.repairOnboardingPublication")
			security.SessionHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "repairPlatformMCPOnboardingPublication")
		Meta("openapi:extension:x-speakeasy-name-override", "repairOnboardingPublication")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RepairPlatformMCPOnboardingPublication"}`)
	})

	Method("dismissOnboarding", func() {
		Description("Dismiss the optional current user's Platform MCP onboarding workflow without changing organization setup or project resources.")
		Payload(func() { security.SessionPayload() })
		HTTP(func() {
			POST("/rpc/platformMcp.dismissOnboarding")
			security.SessionHeader()
			Response(StatusNoContent)
		})
		Meta("openapi:operationId", "dismissPlatformMCPOnboarding")
		Meta("openapi:extension:x-speakeasy-name-override", "dismissOnboarding")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DismissPlatformMCPOnboarding"}`)
	})
})
