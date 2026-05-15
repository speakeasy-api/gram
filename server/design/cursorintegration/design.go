package cursorintegration

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var Config = Type("CursorIntegrationConfig", func() {
	Description("Per-project config that controls polling Cursor Admin API usage events. The API key is write-only; reads only expose whether a key is configured.")
	Required("organization_id", "project_id", "enabled", "has_api_key")
	Attribute("id", String, "Config ID. Omitted when no config is set for the project.")
	Attribute("organization_id", String, "Organization the config belongs to.")
	Attribute("project_id", String, "Project the config belongs to.")
	Attribute("enabled", Boolean, "Whether Cursor usage polling is active.")
	Attribute("has_api_key", Boolean, "Whether a Cursor API key is currently stored. The key itself is never returned.")
	Attribute("last_polled_at", String, "ISO 8601 timestamp for the polling high-water mark. Omitted when no config is set.", func() {
		Format(FormatDateTime)
	})
	Attribute("created_at", String, "ISO 8601 timestamp when the config was created. Omitted when no config is set.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "ISO 8601 timestamp of the most recent change. Omitted when no config is set.", func() {
		Format(FormatDateTime)
	})
})

var _ = Service("cursorIntegration", func() {
	Description("Manage per-project Cursor Admin API usage polling.")

	shared.DeclareErrorResponses()

	Method("getConfig", func() {
		Description("Get the current project's Cursor integration config. Returns an empty config (enabled=false, has_api_key=false) when none is set.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("consumer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(Config)

		HTTP(func() {
			GET("/rpc/cursorIntegration.getConfig")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getCursorIntegrationConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "getConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CursorIntegrationConfig"}`)
	})

	Method("upsertConfig", func() {
		Description("Create or update the current project's Cursor integration config.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("api_key", String, "Cursor team Admin API key. Stored encrypted at rest; never returned on reads.")
			Attribute("enabled", Boolean, "Whether Cursor usage polling should be active.")
			Required("api_key", "enabled")
		})

		Result(Config)

		HTTP(func() {
			POST("/rpc/cursorIntegration.upsertConfig")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertCursorIntegrationConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "upsertConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertCursorIntegrationConfig"}`)
	})

	Method("deleteConfig", func() {
		Description("Delete the current project's Cursor integration config.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(Empty)

		HTTP(func() {
			POST("/rpc/cursorIntegration.deleteConfig")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteCursorIntegrationConfig")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteConfig")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteCursorIntegrationConfig"}`)
	})
})
