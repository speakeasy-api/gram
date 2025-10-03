package mcpmetadata

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var McpMetadata = Type("McpMetadata", func() {
	Meta("struct:pkg:path", "types")

	Description("Metadata used to configure the MCP install page.")

	Attribute("id", String, "The ID of the metadata record")
	Attribute("toolset_id", String, "The toolset associated with this install page metadata", func() {
		Format(FormatUUID)
	})
	Attribute("logo_asset_id", String, "The asset ID for the MCP install page logo", func() {
		Format(FormatUUID)
	})
	Attribute("external_documentation_url", String, "A link to external documentation for the MCP install page", func() {
		Format(FormatURI)
	})
	Attribute("created_at", String, "When the metadata entry was created", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "When the metadata entry was last updated", func() {
		Format(FormatDateTime)
	})

	Required("id", "toolset_id", "created_at", "updated_at")
})

var _ = Service("mcpMetadata", func() {
	Description("Manages metadata for the MCP install page shown to users.")

	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("getMcpMetadata", func() {
		Description("Fetch the metadata that powers the MCP install page.")

		Payload(func() {
			Attribute("toolset_slug", shared.Slug, "The slug of the toolset associated with this install page metadata")

			Required("toolset_slug")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("metadata", McpMetadata, "Metadata for the MCP install page")
		})

		HTTP(func() {
			GET("/rpc/mcpMetadata.get")
			security.SessionHeader()
			security.ProjectHeader()
			Param("toolset_slug")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getMcpMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetMcpMetadata"}`)
	})

	Method("setMcpMetadata", func() {
		Description("Create or update the metadata that powers the MCP install page.")

		Payload(func() {
			Attribute("toolset_slug", shared.Slug, "The slug of the toolset associated with this install page metadata")
			Attribute("logo_asset_id", String, "The asset ID for the MCP install page logo")
			Attribute("external_documentation_url", String, "A link to external documentation for the MCP install page")

			Required("toolset_slug")

			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(McpMetadata)

		HTTP(func() {
			POST("/rpc/mcpMetadata.set")
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "setMcpMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "set")
	})
})
