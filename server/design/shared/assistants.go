package shared

import . "goa.design/goa/v3/dsl"

var AssistantToolsetRef = Type("AssistantToolsetRef", func() {
	Meta("struct:pkg:path", "types")

	Attribute("toolset_slug", String, "The toolset slug exposed to the assistant.")
	Attribute("environment_slug", String, "Optional environment slug used when invoking the toolset.")

	Required("toolset_slug")
})

var AssistantMCPServerRef = Type("AssistantMCPServerRef", func() {
	Meta("struct:pkg:path", "types")

	Attribute("mcp_server_slug", String, "The MCP server slug exposed to the assistant. Covers remote- and tunnelled-backed MCP servers, which have no toolset to attach.")
	Attribute("environment_slug", String, "Optional environment slug used when connecting to the MCP server.")
	Attribute("endpoint_slug", String, "The slug of the server's Gram-hosted MCP endpoint (/mcp/{endpoint_slug}). Populated on reads; ignored on writes. Absent when the server has no Gram-hosted endpoint.")

	Required("mcp_server_slug")
})

var AssistantSkillRef = Type("AssistantSkillRef", func() {
	Meta("struct:pkg:path", "types")

	Attribute("skill_id", String, "The attached skill ID.", func() { Format(FormatUUID) })
	Attribute("pinned_version_id", String, "The pinned version, absent when tracking latest valid.", func() { Format(FormatUUID) })
	Attribute("resolved_version_id", String, "The currently resolved valid version.", func() { Format(FormatUUID) })

	Required("skill_id", "resolved_version_id")
})

var Assistant = Type("Assistant", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The assistant ID.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID owning the assistant.", func() {
		Format(FormatUUID)
	})
	Attribute("created_by_user_id", String, "The ID of the user who created the assistant, if known.")
	Attribute("identity_state", String, "Whether this assistant has never configured, active, or tombstoned workload identity bindings. This is configuration state, not permission to execute.", func() {
		Enum("NEVER_CONFIGURED", "ACTIVE", "TOMBSTONED")
	})
	Attribute("agent_id", String, "The dedicated agent ID for an active identity binding.", func() { Format(FormatUUID) })
	Attribute("identity_diagnostics", AssistantIdentityDiagnostics, "Detail-only identity health and rollout information. Configuration is not permission or consent.")
	Attribute("identity_upgrade_outcome", String, "Present only on an explicit identity upgrade response: upgraded creates the first binding, repaired provisions missing live roots, unchanged preserves existing bindings. Not a permission grant or OAuth consent.", func() {
		Enum("upgraded", "repaired", "unchanged")
	})
	Attribute("identity_generation", Int64, "The current or last retained assistant identity binding generation.")
	Attribute("name", String, "The assistant name.")
	Attribute("model", String, "The model identifier used by the assistant.")
	Attribute("instructions", String, "The system instructions for the assistant.")
	Attribute("toolsets", ArrayOf(AssistantToolsetRef), "Toolsets available to the assistant.")
	Attribute("mcp_servers", ArrayOf(AssistantMCPServerRef), "MCP servers attached directly to the assistant (remote- or tunnelled-backed).")
	Attribute("skills", ArrayOf(AssistantSkillRef), "Skills attached to the assistant.")
	Attribute("warm_ttl_seconds", Int, "Warm runtime TTL in seconds.")
	Attribute("max_concurrency", Int, "Maximum active warm runtimes for the assistant.")
	Attribute("status", String, "The assistant status.", func() {
		Enum("active", "paused")
	})
	Attribute("created_at", String, "Creation timestamp.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "Last update timestamp.", func() {
		Format(FormatDateTime)
	})

	Required(
		"id",
		"project_id",
		"name",
		"model",
		"instructions",
		"toolsets",
		"mcp_servers",
		"skills",
		"warm_ttl_seconds",
		"max_concurrency",
		"status",
		"created_at",
		"updated_at",
	)
})

var AssistantMemory = Type("AssistantMemory", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The assistant memory ID.", func() {
		Format(FormatUUID)
	})
	Attribute("assistant_id", String, "The assistant ID owning the memory.", func() {
		Format(FormatUUID)
	})
	Attribute("content", String, "The memory content.")
	Attribute("tags", ArrayOf(String), "Tags associated with the memory.")
	Attribute("created_at", String, "Creation timestamp.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "Last update timestamp.", func() {
		Format(FormatDateTime)
	})
	Attribute("last_access", String, "Timestamp of the most recent access.", func() {
		Format(FormatDateTime)
	})
	Attribute("valid_at", String, "Timestamp at which the memory becomes valid.", func() {
		Format(FormatDateTime)
	})
	Attribute("superseded_at", String, "Timestamp at which the memory was superseded by another memory.", func() {
		Format(FormatDateTime)
	})
	Attribute("deleted_at", String, "Timestamp at which the memory was soft-deleted.", func() {
		Format(FormatDateTime)
	})
	Attribute("supersedes_id", String, "The ID of the memory this one supersedes, if any.", func() {
		Format(FormatUUID)
	})

	Required(
		"id",
		"assistant_id",
		"content",
		"tags",
		"created_at",
		"updated_at",
		"last_access",
		"valid_at",
	)
})

var AssistantIdentityDiagnostics = Type("AssistantIdentityDiagnostics", func() {
	Meta("struct:pkg:path", "types")
	Description("Safe workload configuration diagnostics, not a permission or OAuth consent decision. Shared by the dashboard and Platform MCP.")
	Attribute("health", String, "legacy, ready, suspended, or unavailable; ready only describes identity configuration.", func() { Meta("struct:tag:json", "health"); Enum("legacy", "ready", "suspended", "unavailable") })
	Attribute("provisioning_enabled", Boolean, "Whether new identities and explicit upgrades are enabled on this serving tier.", func() { Meta("struct:tag:json", "provisioning_enabled") })
	Attribute("execution_enabled", Boolean, "Whether workload token issuance and use are enabled on this serving tier.", func() { Meta("struct:tag:json", "execution_enabled") })
	Attribute("slack_delegation_enabled", Boolean, "Whether mapped Slack delegation is enabled on this serving tier.", func() { Meta("struct:tag:json", "slack_delegation_enabled") })
	Attribute("bindings", ArrayOf(AssistantIdentityBinding), "At most 100 current trigger roots, including roots without a binding.", func() { Meta("struct:tag:json", "bindings") })
	Attribute("bindings_truncated", Boolean, "More roots exist than are returned.", func() { Meta("struct:tag:json", "bindings_truncated") })
	Attribute("last_event_id", String, "Most recent persisted event ID; not a token.", func() { Meta("struct:tag:json", "last_event_id,omitempty") })
	Attribute("last_execution_mode", String, "Captured mode of the most recent event, or legacy. Not a prediction for the next message.", func() { Meta("struct:tag:json", "last_execution_mode,omitempty") })
	Attribute("last_fallback_reason", String, "Bounded reason for autonomous execution when no delegating user was resolved.", func() { Meta("struct:tag:json", "last_fallback_reason,omitempty") })
	Attribute("last_event_status", String, "Persisted processing status, not a tool permission or consent verdict.", func() { Meta("struct:tag:json", "last_event_status,omitempty") })
	Attribute("last_initiating_user_id", String, "Selected human delegator, absent for autonomous work; never the acting agent.", func() { Meta("struct:tag:json", "last_initiating_user_id,omitempty") })
	Required("health", "provisioning_enabled", "execution_enabled", "slack_delegation_enabled", "bindings", "bindings_truncated")
})

var AssistantIdentityBinding = Type("AssistantIdentityBinding", func() {
	Meta("struct:pkg:path", "types")
	Attribute("trigger_id", String, "Exact trigger root ID.", func() { Meta("struct:tag:json", "trigger_id"); Format(FormatUUID) })
	Attribute("trigger_kind", String, "Trigger definition slug.", func() { Meta("struct:tag:json", "trigger_kind") })
	Attribute("trigger_status", String, "Current trigger status.", func() { Meta("struct:tag:json", "trigger_status") })
	Attribute("state", String, "ready, missing, or unavailable. Never interprets missing authority as legacy.", func() { Meta("struct:tag:json", "state"); Enum("ready", "missing", "unavailable") })
	Attribute("generation", Int64, "Retained workload binding generation, zero if never configured.", func() { Meta("struct:tag:json", "generation") })
	Required("trigger_id", "trigger_kind", "trigger_status", "state", "generation")
})
