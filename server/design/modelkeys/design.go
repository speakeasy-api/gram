package modelkeys

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var ModelProviderKey = Type("ModelProviderKey", func() {
	Meta("struct:pkg:path", "types")
	Description("A customer-supplied model provider API key bound to a project and responsibility slot. Key material is write-only and never returned.")
	Required("id", "project_id", "slot", "provider", "enabled", "created_at", "updated_at")
	Attribute("id", String, "The ID of the key", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project the key belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("slot", String, "The responsibility slot the key applies to. The 'default' slot covers every slot without a dedicated override.")
	Attribute("provider", String, "The model provider the key authenticates with. Supported values include openrouter.")
	Attribute("enabled", Boolean, "Whether the key participates in key resolution.")
	Attribute("created_at", String, "ISO 8601 timestamp when the key was created.", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "ISO 8601 timestamp of the most recent change.", func() {
		Format(FormatDateTime)
	})
})

var _ = Service("modelKeys", func() {
	Description("Manage customer-supplied model provider API keys, scoped per project and responsibility slot.")

	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})

	shared.DeclareErrorResponses()

	Method("listKeys", func() {
		Description("List the model provider keys configured for a project. Key material is never returned.")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("keys", ArrayOf(ModelProviderKey), "The model provider keys configured for the project.")
			Required("keys")
		})

		HTTP(func() {
			GET("/rpc/modelKeys.listKeys")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listModelProviderKeys")
		Meta("openapi:extension:x-speakeasy-name-override", "listKeys")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ModelProviderKeys"}`)
	})

	Method("upsertKey", func() {
		Description("Create or replace the model provider key for a slot. The key is validated with the provider, then stored encrypted.")

		Payload(func() {
			Attribute("slot", String, "The responsibility slot the key applies to. Use 'default' to cover every slot without a dedicated override.")
			Attribute("provider", String, "The model provider the key authenticates with. Supported values include openrouter.")
			Attribute("api_key", String, "The provider API key. Stored encrypted at rest; never returned on reads.")
			Attribute("enabled", Boolean, "Whether the key participates in key resolution.", func() {
				Default(true)
			})
			Required("slot", "provider", "api_key")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ModelProviderKey)

		HTTP(func() {
			POST("/rpc/modelKeys.upsertKey")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertModelProviderKey")
		Meta("openapi:extension:x-speakeasy-name-override", "upsertKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertModelProviderKey"}`)
	})

	Method("setKeyEnabled", func() {
		Description("Enable or disable a model provider key without replacing its key material.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to update.", func() {
				Format(FormatUUID)
			})
			Attribute("enabled", Boolean, "Whether the key participates in key resolution.")
			Required("id", "enabled")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ModelProviderKey)

		HTTP(func() {
			POST("/rpc/modelKeys.setKeyEnabled")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setModelProviderKeyEnabled")
		Meta("openapi:extension:x-speakeasy-name-override", "setKeyEnabled")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetModelProviderKeyEnabled"}`)
	})

	Method("deleteKey", func() {
		Description("Delete a model provider key. Completions on the affected slot fall back to the project default key or the platform key.")

		Payload(func() {
			Attribute("id", String, "The ID of the key to delete.", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(Empty)

		HTTP(func() {
			DELETE("/rpc/modelKeys.deleteKey")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteModelProviderKey")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteModelProviderKey"}`)
	})
})
