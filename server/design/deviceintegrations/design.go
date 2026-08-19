package deviceintegrations

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var ProviderField = Type("DeviceIntegrationProviderField", func() {
	Description("One credential or settings input a provider needs to connect. Secret fields are stored encrypted and are write-only; non-secret fields are readable settings.")
	Required("key", "label", "kind", "secret", "required")
	Attribute("key", String, "Field identifier in the credentials or settings document.")
	Attribute("label", String, "Human-readable name to render for the field.")
	Attribute("kind", String, "Input kind hint.", func() {
		Enum("text", "url")
	})
	Attribute("secret", Boolean, "Whether the value is secret: stored encrypted, write-only, and masked in the UI.")
	Attribute("required", Boolean, "Whether the field must be supplied to connect the provider.")
})

var ProviderSchedule = Type("DeviceIntegrationProviderSchedule", func() {
	Description("One sync pipeline a provider runs on a cadence.")
	Required("schedule", "interval_minutes")
	Attribute("schedule", String, "Schedule identifier (e.g. jamf_inventory).")
	Attribute("interval_minutes", Int, "Target minutes between successful runs. Declared in code by the provider, not configurable per org.")
})

var Provider = Type("DeviceIntegrationProvider", func() {
	Description("A vendor the device integrations framework can connect to.")
	Required("id", "display_name", "capabilities", "fields", "schedules")
	Attribute("id", String, "Provider identifier (e.g. jamf, kandji, drata, vanta).")
	Attribute("display_name", String, "Human-readable vendor name.")
	Attribute("capabilities", ArrayOf(String), "Integration directions the provider supports: inventory_source (pull the managed-device fleet) and/or evidence_sink (push agent-coverage evidence).")
	Attribute("fields", ArrayOf(ProviderField), "Credential and settings inputs the provider needs, in render order.")
	Attribute("schedules", ArrayOf(ProviderSchedule), "Sync pipelines the provider runs.")
})

var ListProvidersResult = Type("ListDeviceIntegrationProvidersResult", func() {
	Required("providers")
	Attribute("providers", ArrayOf(Provider), "Every available provider, sorted by id.")
})

var Config = Type("DeviceIntegrationConfig", func() {
	Description("Per-organization device integration config for one provider. Secret credentials are write-only; reads only expose whether credentials are configured.")
	Required("organization_id", "provider", "enabled", "has_credentials", "settings")
	Attribute("id", String, "Config ID. Omitted when no config is set for the provider.")
	Attribute("organization_id", String, "Organization the config belongs to.")
	Attribute("provider", String, "Provider identifier (e.g. jamf, kandji, drata, vanta).")
	Attribute("enabled", Boolean, "Whether the provider integration is active.")
	Attribute("has_credentials", Boolean, "Whether secret credentials are currently stored. The values themselves are never returned.")
	Attribute("settings", MapOf(String, String), "Non-secret provider settings (e.g. instance URL), keyed by field key.")
	Attribute("created_at", String, "ISO 8601 timestamp when the config was created. Omitted when no config is set.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "ISO 8601 timestamp of the most recent change. Omitted when no config is set.", func() {
		Format(FormatDateTime)
	})
})

var ScheduleState = Type("DeviceIntegrationScheduleState", func() {
	Description("State of one sync schedule of a provider integration: the user's intent (enabled) plus the scheduler's execution state.")
	Required("schedule", "enabled", "status", "consecutive_failures")
	Attribute("schedule", String, "Schedule identifier (e.g. jamf_inventory).")
	Attribute("enabled", Boolean, "Whether the user has this schedule enabled. Disabled schedules are skipped by the scheduler until re-enabled.")
	Attribute("status", String, "Derived status for the schedule's latest sync state.", func() {
		Enum("pending", "success", "failed", "auto_paused", "disabled")
	})
	Attribute("last_sync_success_at", String, "ISO 8601 timestamp for the schedule's last successful sync. Omitted until a sync succeeds.", func() {
		Format(FormatDateTime)
	})
	Attribute("last_sync_failed_at", String, "ISO 8601 timestamp for the schedule's latest failed sync. Omitted unless a sync has failed.", func() {
		Format(FormatDateTime)
	})
	Attribute("last_sync_error", String, "Stored error from the schedule's latest failed sync. Omitted unless the latest sync failed.")
	Attribute("next_sync_after", String, "ISO 8601 timestamp for the schedule's next scheduled sync.", func() {
		Format(FormatDateTime)
	})
	Attribute("consecutive_failures", Int, "Number of consecutive failed syncs. Resets to zero on success or retry.")
	Attribute("auto_paused_at", String, "ISO 8601 timestamp when the scheduler auto-paused the schedule after repeated provider rejections. Omitted unless auto-paused.", func() {
		Format(FormatDateTime)
	})
})

var ListSchedulesResult = Type("ListDeviceIntegrationSchedulesResult", func() {
	Description("Sync schedules for one provider integration. Empty when no config is set for the provider.")
	Required("organization_id", "provider", "schedules")
	Attribute("organization_id", String, "Organization the schedules belong to.")
	Attribute("provider", String, "Provider identifier.")
	Attribute("schedules", ArrayOf(ScheduleState), "State for each of the provider's sync schedules.")
})

var TestConnectionResult = Type("DeviceIntegrationTestConnectionResult", func() {
	Description("Outcome of probing the provider with the stored credentials.")
	Required("ok")
	Attribute("ok", Boolean, "Whether the provider accepted the stored credentials.")
	Attribute("message", String, "Human-readable failure detail. Omitted on success.")
})

var ManagedDevice = Type("ManagedDevice", func() {
	Description("One device from a connected MDM's inventory, annotated with its agent-coverage bucket. What that bucket attests depends on the organization's matching mode: under device-level matching a device may be classified by its own hardware serial (this machine ran the agent) or, when no serial heartbeat exists for it, by its assigned-user email (only that user ran the agent somewhere). Under user-level matching only the latter applies.")
	Required("id", "provider", "external_id", "coverage_bucket", "first_seen_at", "last_seen_at")
	Attribute("id", String, "Device row ID.")
	Attribute("provider", String, "Provider the device was synced from.")
	Attribute("external_id", String, "The MDM's identifier for the device.")
	Attribute("serial_number", String, "Hardware serial number as reported by the MDM.")
	Attribute("hostname", String, "Device hostname.")
	Attribute("os_name", String, "Operating system name.")
	Attribute("os_version", String, "Operating system version.")
	Attribute("user_email", String, "Assigned user's email exactly as the MDM reported it. Omitted when the MDM has no assignment.")
	Attribute("user_id", String, "Resolved Gram user for the assigned email. Omitted when the email is missing or does not resolve to an org member.")
	Attribute("mdm_last_check_in_at", String, "Last device check-in as reported by the MDM.", func() {
		Format(FormatDateTime)
	})
	Attribute("agent_last_seen_at", String, "The device-agent heartbeat that classified this device: the machine's own under device-level matching, otherwise its assigned user's. Omitted when no agent has ever synced.", func() {
		Format(FormatDateTime)
	})
	Attribute("coverage_bucket", String, "Coverage classification for the device. What agent_active attests depends on the org's matching mode and on whether this device matched by serial or by assigned-user email.", func() {
		Enum("agent_active", "agent_stale", "agent_other_device", "no_agent", "no_email", "unresolved_email", "missing")
	})
	Attribute("missing_since", String, "When the device went absent from the MDM inventory. Omitted while present.", func() {
		Format(FormatDateTime)
	})
	Attribute("first_seen_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("last_seen_at", String, func() {
		Format(FormatDateTime)
	})
})

var ListManagedDevicesResult = Type("ListManagedDevicesResult", func() {
	Required("devices")
	Attribute("devices", ArrayOf(ManagedDevice), "Devices, newest first.")
	Attribute("next_cursor", String, "Cursor for the next page. Omitted on the last page.")
})

var Coverage = Type("DeviceIntegrationCoverage", func() {
	Description("Agent-coverage summary across the org's connected MDM inventories. What a bucket attests depends on the org's matching mode: under device-level matching (hardware serial) agent_active means THIS machine reported in, while under user-level matching it means only that the device's assigned user has a fresh heartbeat somewhere.")
	Required("organization_id", "active_window_minutes", "attestation", "agent_active", "agent_active_device_attested", "agent_stale", "agent_other_device", "no_agent", "no_email", "unresolved_email", "missing", "total_devices", "unmanaged_agent_users")
	Attribute("organization_id", String, "Organization the coverage describes.")
	Attribute("active_window_minutes", Int, "Freshness window: an agent counts as active when its heartbeat is within this many minutes.")
	// Clients render the coverage percentage as a sentence, and the sentence
	// that is true depends on the matching mode. Without this they would have
	// to infer it (agent_other_device > 0 only implies device-level, never
	// rules it out) and would eventually claim more than the data supports.
	Attribute("attestation", String, "The strongest claim that holds for EVERY active device in this response — not merely the org's matching mode. \"device\": every active device was matched on its own hardware serial, so each one ran the agent. \"user\": at least one active device was matched only on its assigned-user email, so the set as a whole supports only the weaker claim that assigned users ran the agent somewhere.", func() {
		Enum("device", "user")
	})
	Attribute("agent_active", Int64, "Devices with an agent heartbeat within the window. What that attests depends on the matching mode and, per device, on whether the match was by serial or by email — compare against agent_active_device_attested.")
	Attribute("agent_active_device_attested", Int64, "How many of agent_active are backed by that machine's OWN heartbeat (serial match). Under user-level matching this is 0; under device-level matching a value below agent_active means some devices fell back to the assigned-user email, which is the weaker claim.")
	Attribute("agent_stale", Int64, "Devices with a known agent that went quiet — the drift/disable case.")
	Attribute("agent_other_device", Int64, "Device-level matching only: devices whose assigned user runs the agent, but not on this machine. Always 0 under user-level matching, which cannot distinguish this from agent_active.")
	Attribute("no_agent", Int64, "Devices whose assigned email resolves to an org member with no agent at all.")
	Attribute("no_email", Int64, "Devices the MDM reports with no assigned-user email.")
	Attribute("unresolved_email", Int64, "Devices whose assigned email matches neither an agent user nor an org member.")
	Attribute("missing", Int64, "Devices absent from the latest completed MDM snapshot.")
	Attribute("total_devices", Int64, "All synced devices, including missing ones.")
	Attribute("unmanaged_agent_users", Int64, "Agent users with no matching device in any connected MDM inventory — shadow devices or fleets outside MDM.")
})

var _ = Service("deviceIntegrations", func() {
	Description("Manage organization-level device integrations: MDM inventory sources and compliance evidence sinks.")

	shared.DeclareErrorResponses()

	Method("listProviders", func() {
		Description("List the providers the device integrations framework supports, including the credential fields each needs.")

		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListProvidersResult)

		HTTP(func() {
			GET("/rpc/deviceIntegrations.listProviders")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listDeviceIntegrationProviders")
		Meta("openapi:extension:x-speakeasy-name-override", "listProviders")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeviceIntegrationProviders"}`)
	})

	Method("getConfig", func() {
		Description("Get the org-wide device integration config for a provider. Returns an empty config (enabled=false, has_credentials=false) when none is set.")

		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "Provider identifier (e.g. jamf, kandji, drata, vanta).")
			Required("provider")
		})

		Result(Config)

		HTTP(func() {
			GET("/rpc/deviceIntegrations.getConfig")
			Param("provider")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getDeviceIntegrationConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "getConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeviceIntegrationConfig"}`)
	})

	Method("upsertConfig", func() {
		Description("Create or update the org-wide device integration config for a provider. Omit credentials to keep the stored secrets; supplying credentials rotates them in place and resets the schedules' sync state. Settings merge per key: an omitted key keeps its stored value, a supplied key overwrites it.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "Provider identifier (e.g. jamf, kandji, drata, vanta).")
			Attribute("credentials", MapOf(String, String), "Secret credential values keyed by field key. Stored encrypted; never returned on reads. Omit to keep the existing secrets.")
			Attribute("settings", MapOf(String, String), "Non-secret settings values keyed by field key. Merged per key with the stored settings: omitted keys keep their stored values.")
			Attribute("enabled", Boolean, "Whether the integration should be active.")
			Required("provider", "enabled")
		})

		Result(Config)

		HTTP(func() {
			POST("/rpc/deviceIntegrations.upsertConfig")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertDeviceIntegrationConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "upsertConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertDeviceIntegrationConfig"}`)
	})

	Method("deleteConfig", func() {
		Description("Disconnect the org-wide device integration for a provider. Synced inventory becomes invisible to coverage; reconnecting starts fresh.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "Provider identifier (e.g. jamf, kandji, drata, vanta).")
			Required("provider")
		})

		Result(Empty)

		HTTP(func() {
			POST("/rpc/deviceIntegrations.deleteConfig")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteDeviceIntegrationConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteDeviceIntegrationConfig"}`)
	})

	Method("testConnection", func() {
		Description("Probe the provider with the stored credentials to verify they work. Save the config first; this tests what is stored, so secrets never round-trip.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "Provider identifier (e.g. jamf, kandji, drata, vanta).")
			Required("provider")
		})

		Result(TestConnectionResult)

		HTTP(func() {
			POST("/rpc/deviceIntegrations.testConnection")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "testDeviceIntegrationConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "testConnection")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "TestDeviceIntegrationConnection"}`)
	})

	Method("listSchedules", func() {
		Description("List the sync schedules and their state for a provider's org-wide device integration config. Returns an empty list when no config is set.")

		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "Provider identifier (e.g. jamf, kandji, drata, vanta).")
			Required("provider")
		})

		Result(ListSchedulesResult)

		HTTP(func() {
			GET("/rpc/deviceIntegrations.listSchedules")
			Param("provider")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listDeviceIntegrationSchedules")
		Meta("openapi:extension:x-speakeasy-name-override", "listSchedules")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeviceIntegrationSchedules"}`)
	})

	Method("setScheduleEnabled", func() {
		Description("Enable or disable one sync schedule of a provider's device integration. Disabling records user intent on the schedule; only re-enabling clears it — config saves do not.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "Provider identifier (e.g. jamf, kandji, drata, vanta).")
			Attribute("schedule", String, "Schedule identifier (e.g. jamf_inventory).")
			Attribute("enabled", Boolean, "Whether the schedule should run.")
			Required("provider", "schedule", "enabled")
		})

		Result(ScheduleState)

		HTTP(func() {
			POST("/rpc/deviceIntegrations.setScheduleEnabled")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setDeviceIntegrationScheduleEnabled")
		Meta("openapi:extension:x-speakeasy-name-override", "setScheduleEnabled")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetDeviceIntegrationScheduleEnabled"}`)
	})

	Method("retrySchedule", func() {
		Description("Make one sync schedule due immediately, lifting any automatic pause and resetting its failure streak.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "Provider identifier (e.g. jamf, kandji, drata, vanta).")
			Attribute("schedule", String, "Schedule identifier (e.g. jamf_inventory).")
			Required("provider", "schedule")
		})

		Result(ScheduleState)

		HTTP(func() {
			POST("/rpc/deviceIntegrations.retrySchedule")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "retryDeviceIntegrationSchedule")
		Meta("openapi:extension:x-speakeasy-name-override", "retrySchedule")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RetryDeviceIntegrationSchedule"}`)
	})

	Method("listManagedDevices", func() {
		Description("Page through the org's synced MDM device inventory, newest first, with each device's coverage bucket.")

		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "Only devices synced from this provider.")
			Attribute("coverage_bucket", String, "Only devices in this coverage bucket.", func() {
				Enum("agent_active", "agent_stale", "agent_other_device", "no_agent", "no_email", "unresolved_email", "missing")
			})
			Attribute("user_ids", ArrayOf(String), "Only devices assigned to these Gram users. Combined with user_emails as an OR, because a device only carries a resolved user id when the MDM's reported email matched a member.")
			Attribute("user_emails", ArrayOf(String), "Only devices whose MDM-reported assigned email is one of these, matched case-insensitively.")
			Attribute("cursor", String, "Pagination cursor from a previous page.")
			Attribute("limit", Int, "Page size. Defaults to 50, maximum 200.", func() {
				Minimum(1)
				Maximum(200)
				Default(50)
			})
		})

		Result(ListManagedDevicesResult)

		HTTP(func() {
			GET("/rpc/deviceIntegrations.listManagedDevices")
			Param("provider")
			Param("coverage_bucket")
			Param("user_ids")
			Param("user_emails")
			Param("cursor")
			Param("limit")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		shared.CursorPagination()
		Meta("openapi:operationId", "listManagedDevices")
		Meta("openapi:extension:x-speakeasy-name-override", "listManagedDevices")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ManagedDevices"}`)
	})

	Method("getCoverage", func() {
		Description("Summarize agent coverage across the org's connected MDM inventories, optionally scoped to one provider.")

		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "Optionally scope coverage to one provider (e.g. jamf).")
		})

		Result(Coverage)

		HTTP(func() {
			GET("/rpc/deviceIntegrations.getCoverage")
			Param("provider")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getDeviceIntegrationCoverage")
		Meta("openapi:extension:x-speakeasy-name-override", "getCoverage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeviceIntegrationCoverage"}`)
	})
})
