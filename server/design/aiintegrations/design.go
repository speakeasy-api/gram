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
	Attribute("provider", String, "AI provider identifier. Initially only cursor is supported.")
	Attribute("project_id", String, "Project used as the telemetry write target. Omitted when no config is set.")
	Attribute("enabled", Boolean, "Whether the provider integration is active.")
	Attribute("has_api_key", Boolean, "Whether an API key is currently stored. The key itself is never returned.")
	Attribute("last_polled_at", String, "ISO 8601 timestamp for the usage sync high-water mark. Omitted when no config is set.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, "ISO 8601 timestamp when the config was created. Omitted when no config is set.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "ISO 8601 timestamp of the most recent change. Omitted when no config is set.", func() {
		Format(FormatDateTime)
	})
})

var _ = Service("aiIntegrations", func() {
	Description("Manage organization-level AI provider integrations.")

	shared.DeclareErrorResponses()

	Method("getAIIntegrationConfig", func() {
		Description("Get the org-wide AI integration config for a provider. Returns an empty config (enabled=false, has_api_key=false) when none is set.")

		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "AI provider identifier. Initially only cursor is supported.")
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

	Method("upsertAIIntegrationConfig", func() {
		Description("Create or update the org-wide AI integration config for a provider.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "AI provider identifier. Initially only cursor is supported.")
			Attribute("api_key", String, "Provider API key. Stored encrypted at rest; never returned on reads.")
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

	Method("deleteAIIntegrationConfig", func() {
		Description("Delete the org-wide AI integration config for a provider.")

		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			Attribute("provider", String, "AI provider identifier. Initially only cursor is supported.")
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
})
