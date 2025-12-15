package externalmcp

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("mcpRegistries", func() {
	Description("External MCP registry operations")

	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("listCatalog", func() {
		Description("List available MCP servers from configured registries")

		Payload(func() {
			Attribute("registry_id", String, "Filter to a specific registry", func() {
				Format(FormatUUID)
			})
			Attribute("search", String, "Search query to filter servers by name")
			Attribute("cursor", String, "Pagination cursor")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("servers", ArrayOf(ExternalMCPServer), "List of available MCP servers")
			Attribute("next_cursor", String, "Pagination cursor for the next page")
			Required("servers")
		})

		HTTP(func() {
			GET("/rpc/mcpRegistries.listCatalog")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("registry_id")
			Param("search")
			Param("cursor")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMCPCatalog")
		Meta("openapi:extension:x-speakeasy-name-override", "listCatalog")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListMCPCatalog"}`)
	})
})

var ExternalMCPServer = Type("ExternalMCPServer", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP server from an external registry")

	Attribute("name", String, "Server name in reverse-DNS format (e.g., 'io.github.user/server')", func() {
		Example("io.modelcontextprotocol.anonymous/exa")
	})
	Attribute("version", String, "Semantic version of the server", func() {
		Example("1.0.0")
	})
	Attribute("description", String, "Description of what the server does")
	Attribute("registry_id", String, "ID of the registry this server came from", func() {
		Format(FormatUUID)
	})
	Attribute("title", String, "Display name for the server")
	Attribute("icon_url", String, "URL to the server's icon", func() {
		Format(FormatURI)
	})
	Attribute("meta", Any, "Opaque metadata from the registry")

	Required("name", "version", "description", "registry_id")
})
