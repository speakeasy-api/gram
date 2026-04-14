package shared

import . "goa.design/goa/v3/dsl"

var TriggerEnvRequirement = Type("TriggerEnvRequirement", func() {
	Meta("struct:pkg:path", "types")

	Attribute("name", String, "The environment variable name.")
	Attribute("description", String, "Description of the variable.")
	Attribute("required", Boolean, "Whether the variable is required.")

	Required("name", "required")
})

var TriggerDefinition = Type("TriggerDefinition", func() {
	Meta("struct:pkg:path", "types")

	Attribute("slug", String, "The trigger definition slug.")
	Attribute("title", String, "The trigger definition title.")
	Attribute("description", String, "Description of the trigger definition.")
	Attribute("kind", String, "The ingress kind for the trigger definition.", func() {
		Enum("webhook", "schedule")
	})
	Attribute("config_schema", String, "JSON schema describing the trigger config.", func() {
		Format(FormatJSON)
	})
	Attribute("env_requirements", ArrayOf(TriggerEnvRequirement), "Environment variables required by this trigger definition.")

	Required("slug", "title", "description", "kind", "config_schema", "env_requirements")
})

var TriggerInstance = Type("TriggerInstance", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The trigger instance ID.", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID owning the trigger instance.", func() {
		Format(FormatUUID)
	})
	Attribute("definition_slug", String, "The trigger definition slug.")
	Attribute("name", String, "The trigger instance name.")
	Attribute("environment_id", String, "The linked environment ID.", func() {
		Format(FormatUUID)
	})
	Attribute("target_kind", String, "The target kind for the trigger instance.")
	Attribute("target_ref", String, "The opaque target reference.")
	Attribute("target_display", String, "The user-facing target display value.")
	Attribute("config", MapOf(String, Any), "The trigger config payload.")
	Attribute("status", String, "The trigger instance status.", func() {
		Enum("active", "paused")
	})
	Attribute("webhook_url", String, "Webhook URL for webhook-backed triggers.")
	Attribute("created_at", String, "Creation timestamp.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "Last update timestamp.", func() {
		Format(FormatDateTime)
	})

	Required(
		"id",
		"project_id",
		"definition_slug",
		"name",
		"target_kind",
		"target_ref",
		"target_display",
		"config",
		"status",
		"created_at",
		"updated_at",
	)
})
