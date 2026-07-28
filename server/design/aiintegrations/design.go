package aiintegrations

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var Config = Type("AIIntegrationConfig", func() {
	Description("Per-organization AI provider integration config. The provider API key is write-only; reads only expose whether a key is configured.")
	Required("organization_id", "provider", "enabled", "has_api_key")
	Attribute("id", String, "Config ID. Omitted when no config is set for the provider.")
	Attribute("organization_id", String, "Organization the config belongs to.")
	Attribute("provider", String, "AI provider identifier. Supported values include cursor, anthropic_compliance, and codex_compliance.")
	Attribute("project_id", String, "Project used as the telemetry write target. Omitted when no config is set.")
	Attribute("external_organization_id", String, "Provider organization identifier. Required for anthropic_compliance and codex_compliance; omitted for providers that do not need one.")
	Attribute("billing_mode", String, "How the provider org is billed: 'metered' (pay-per-token; dashboard cost is real spend), 'flat_rate' (subscription seats; cost is an estimate), or 'unknown'. Empty/omitted when not declared.")
	Attribute("enabled", Boolean, "Whether the provider integration is active.")
	Attribute("has_api_key", Boolean, "Whether an API key is currently stored. The key itself is never returned.")
	Attribute("last_polled_at", String, "ISO 8601 timestamp for the last successful usage poll. Omitted until a poll succeeds.", func() {
		Format(FormatDateTime)
	})
	Attribute("next_poll_after", String, "ISO 8601 timestamp for the next scheduled usage poll. Omitted when no config is set.", func() {
		Format(FormatDateTime)
	})
	Attribute("last_poll_status", String, "Derived status for the latest usage poll state. Omitted when no config is set for the provider.", func() {
		Enum("pending", "success", "failed")
	})
	Attribute("last_poll_error", String, "Stored error from the latest failed usage poll. Omitted unless the latest poll state failed.")
	Attribute("last_poll_failed_at", String, "ISO 8601 timestamp for the latest failed usage poll. Omitted unless a poll has failed.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, "ISO 8601 timestamp when the config was created. Omitted when no config is set.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "ISO 8601 timestamp of the most recent change. Omitted when no config is set.", func() {
		Format(FormatDateTime)
	})
})

var ScheduleState = Type("AIIntegrationScheduleState", func() {
	Description("Scheduler state for one sync schedule (stream) of a provider integration.")
	Required("schedule", "enabled", "status", "consecutive_failures")
	Attribute("schedule", String, "Schedule identifier (e.g. cursor, anthropic_compliance, anthropic_analytics_usage).")
	Attribute("stream", String, "Product-level identifier for the stream this schedule writes (e.g. cursor.usage, claude.chat.message). Omitted for legacy schedules with no registered stream.")
	Attribute("stream_kind", String, "Whether the stream carries discrete events or aggregated metrics. Omitted for legacy schedules with no registered stream.", func() {
		Enum("events", "metrics")
	})
	Attribute("enabled", Boolean, "Whether the user has this schedule enabled. Disabled schedules are skipped by the poller until re-enabled.")
	Attribute("status", String, "Derived status for the schedule's latest poll state.", func() {
		Enum("pending", "success", "failed", "auto_paused", "disabled")
	})
	Attribute("last_poll_success_at", String, "ISO 8601 timestamp for the schedule's last successful poll. Omitted until a poll succeeds.", func() {
		Format(FormatDateTime)
	})
	Attribute("last_poll_failed_at", String, "ISO 8601 timestamp for the schedule's latest failed poll. Omitted unless a poll has failed.", func() {
		Format(FormatDateTime)
	})
	Attribute("last_poll_error", String, "Stored error from the schedule's latest failed poll. Omitted unless the latest poll failed.")
	Attribute("next_poll_after", String, "ISO 8601 timestamp for the schedule's next scheduled poll.", func() {
		Format(FormatDateTime)
	})
	Attribute("consecutive_failures", Int, "Number of consecutive failed polls. Resets to zero on success or retry.")
	Attribute("auto_paused_at", String, "ISO 8601 timestamp when the scheduler auto-paused the schedule after repeated provider rejections. Omitted unless auto-paused.", func() {
		Format(FormatDateTime)
	})
})

var ListSchedulesResult = Type("ListAIIntegrationSchedulesResult", func() {
	Description("Sync schedules for one provider integration. Empty when no config is set for the provider.")
	Required("organization_id", "provider", "schedules")
	Attribute("organization_id", String, "Organization the schedules belong to.")
	Attribute("provider", String, "AI provider identifier.")
	Attribute("schedules", ArrayOf(ScheduleState), "Scheduler state for each of the provider's sync schedules.")
})

var _ = Service("aiIntegrations", func() {
	Description("Manage organization-level AI provider integrations.")

	shared.DeclareErrorResponses()

	Method("getConfig", func() {
		Description("Get the org-wide AI integration config for a provider. Returns an empty config (enabled=false, has_api_key=false) when none is set.")

		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "AI provider identifier. Supported values include cursor, anthropic_compliance, and codex_compliance.")
			Required("provider")
		})

		Result(Config)

		HTTP(func() {
			GET("/rpc/aiIntegrations.getConfig")
			Param("provider")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAIIntegrationConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "getConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AIIntegrationConfig"}`)
	})

	Method("upsertConfig", func() {
		Description("Create or update the org-wide AI integration config for a provider.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "AI provider identifier. Supported values include cursor, anthropic_compliance, and codex_compliance.")
			Attribute("api_key", String, "Provider API key. Stored encrypted at rest; never returned on reads.")
			Attribute("external_organization_id", String, "Provider organization identifier. Required for anthropic_compliance and codex_compliance.")
			Attribute("billing_mode", String, "How the provider org is billed: 'metered', 'flat_rate', or 'unknown'. Free-form; omit to leave the existing value unchanged.")
			Attribute("enabled", Boolean, "Whether the integration should be active.")
			Required("provider", "api_key", "enabled")
		})

		Result(Config)

		HTTP(func() {
			POST("/rpc/aiIntegrations.upsertConfig")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertAIIntegrationConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "upsertConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertAIIntegrationConfig"}`)
	})

	Method("deleteConfig", func() {
		Description("Delete the org-wide AI integration config for a provider.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "AI provider identifier. Supported values include cursor, anthropic_compliance, and codex_compliance.")
			Required("provider")
		})

		Result(Empty)

		HTTP(func() {
			POST("/rpc/aiIntegrations.deleteConfig")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteAIIntegrationConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteAIIntegrationConfig"}`)
	})

	Method("listSchedules", func() {
		Description("List the sync schedules and their scheduler state for a provider's org-wide AI integration config. Returns an empty list when no config is set.")

		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "AI provider identifier. Supported values include cursor, anthropic_compliance, and codex_compliance.")
			Required("provider")
		})

		Result(ListSchedulesResult)

		HTTP(func() {
			GET("/rpc/aiIntegrations.listSchedules")
			Param("provider")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listAIIntegrationSchedules")
		Meta("openapi:extension:x-speakeasy-name-override", "listSchedules")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AIIntegrationSchedules"}`)
	})

	Method("setScheduleEnabled", func() {
		Description("Enable or disable one sync schedule of a provider's org-wide AI integration config. Disabled schedules are skipped by the poller until re-enabled.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "AI provider identifier. Supported values include cursor, anthropic_compliance, and codex_compliance.")
			Attribute("schedule", String, "Schedule identifier (e.g. cursor, anthropic_compliance, anthropic_analytics_usage).")
			Attribute("enabled", Boolean, "Whether the schedule should be polled.")
			Required("provider", "schedule", "enabled")
		})

		Result(ScheduleState)

		HTTP(func() {
			POST("/rpc/aiIntegrations.setScheduleEnabled")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setAIIntegrationScheduleEnabled")
		Meta("openapi:extension:x-speakeasy-name-override", "setScheduleEnabled")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetAIIntegrationScheduleEnabled"}`)
	})

	Method("retrySchedule", func() {
		Description("Make one sync schedule due immediately, lifting any automatic pause and resetting its failure streak. The scheduler picks it up on its next tick.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "AI provider identifier. Supported values include cursor, anthropic_compliance, and codex_compliance.")
			Attribute("schedule", String, "Schedule identifier (e.g. cursor, anthropic_compliance, anthropic_analytics_usage).")
			Required("provider", "schedule")
		})

		Result(ScheduleState)

		HTTP(func() {
			POST("/rpc/aiIntegrations.retrySchedule")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "retryAIIntegrationSchedule")
		Meta("openapi:extension:x-speakeasy-name-override", "retrySchedule")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RetryAIIntegrationSchedule"}`)
	})
})
