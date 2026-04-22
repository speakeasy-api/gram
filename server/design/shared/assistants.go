package shared

import . "goa.design/goa/v3/dsl"

var AssistantToolsetRef = Type("AssistantToolsetRef", func() {
	Meta("struct:pkg:path", "types")

	Attribute("toolset_slug", String, "The toolset slug exposed to the assistant.")
	Attribute("environment_slug", String, "Optional environment slug used when invoking the toolset.")

	Required("toolset_slug")
})

var Assistant = Type("Assistant", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The assistant ID.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID owning the assistant.", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The assistant name.")
	Attribute("model", String, "The model identifier used by the assistant.")
	Attribute("instructions", String, "The system instructions for the assistant.")
	Attribute("toolsets", ArrayOf(AssistantToolsetRef), "Toolsets available to the assistant.")
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
		"warm_ttl_seconds",
		"max_concurrency",
		"status",
		"created_at",
		"updated_at",
	)
})
