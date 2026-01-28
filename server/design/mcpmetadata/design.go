package mcpmetadata

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var McpExportTool = Type("McpExportTool", func() {
	Meta("struct:pkg:path", "types")
	Description("A tool definition in the MCP export")

	Attribute("name", String, "The tool name")
	Attribute("description", String, "Description of what the tool does")
	Attribute("input_schema", Any, "JSON Schema for the tool's input parameters")

	Required("name", "description", "input_schema")
})

var McpExportAuthHeader = Type("McpExportAuthHeader", func() {
	Meta("struct:pkg:path", "types")
	Description("An authentication header required by the MCP server")

	Attribute("name", String, "The HTTP header name (e.g., Authorization)")
	Attribute("display_name", String, "User-friendly display name (e.g., API Key)")

	Required("name", "display_name")
})

var McpExportAuthentication = Type("McpExportAuthentication", func() {
	Meta("struct:pkg:path", "types")
	Description("Authentication requirements for the MCP server")

	Attribute("required", Boolean, "Whether authentication is required")
	Attribute("headers", ArrayOf(McpExportAuthHeader), "Required authentication headers")

	Required("required", "headers")
})

var McpExport = Type("McpExport", func() {
	Meta("struct:pkg:path", "types")
	Description("Complete MCP server export for documentation and integration")

	Attribute("name", String, "The MCP server name")
	Attribute("slug", String, "The MCP server slug")
	Attribute("description", String, "Description of the MCP server")
	Attribute("server_url", String, "The MCP server URL")
	Attribute("documentation_url", String, "Link to external documentation")
	Attribute("logo_url", String, "URL to the server logo")
	Attribute("instructions", String, "Server instructions for users")
	Attribute("tools", ArrayOf(McpExportTool), "Available tools on this MCP server")
	Attribute("authentication", McpExportAuthentication, "Authentication requirements")

	Required("name", "slug", "server_url", "tools", "authentication")
})

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
	Attribute("instructions", String, "Server instructions returned in the MCP initialize response")
	Attribute("header_display_names", MapOf(String, String), "Maps security scheme keys to user-friendly display names")
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

	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("getMcpMetadata", func() {
		Description("Fetch the metadata that powers the MCP install page.")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Attribute("toolset_slug", shared.Slug, "The slug of the toolset associated with this install page metadata")

			Required("toolset_slug")

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("metadata", McpMetadata, "Metadata for the MCP install page")
		})

		HTTP(func() {
			GET("/rpc/mcpMetadata.get")
			security.ByKeyHeader()
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
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Attribute("toolset_slug", shared.Slug, "The slug of the toolset associated with this install page metadata")
			Attribute("logo_asset_id", String, "The asset ID for the MCP install page logo")
			Attribute("external_documentation_url", String, "A link to external documentation for the MCP install page")
			Attribute("instructions", String, "Server instructions returned in the MCP initialize response")

			Required("toolset_slug")

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(McpMetadata)

		HTTP(func() {
			POST("/rpc/mcpMetadata.set")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "setMcpMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "set")
	})

	Method("exportMcpMetadata", func() {
		Description("Export MCP server details as JSON for documentation and integration purposes.")
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)

		Payload(func() {
			Attribute("toolset_slug", shared.Slug, "The slug of the toolset to export")

			Required("toolset_slug")

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(McpExport)

		HTTP(func() {
			POST("/rpc/mcpMetadata.export")
			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "exportMcpMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "export")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ExportMcpMetadata"}`)
	})
})
