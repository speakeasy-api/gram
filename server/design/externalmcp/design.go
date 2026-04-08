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

	Method("clearCache", func() {
		Description("Clear the registry cache for a specific registry (admin only)")

		Payload(func() {
			Attribute("registry_id", String, "The registry to clear cache for", func() {
				Format(FormatUUID)
			})
			Required("registry_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/mcpRegistries.clearCache")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("registry_id")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "clearMCPRegistryCache")
		Meta("openapi:extension:x-speakeasy-name-override", "clearCache")
	})

	Method("listRegistries", func() {
		Description("List all MCP registries (admin only)")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("registries", ArrayOf(MCPRegistry), "List of MCP registries")
			Required("registries")
		})

		HTTP(func() {
			GET("/rpc/mcpRegistries.listRegistries")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMCPRegistries")
		Meta("openapi:extension:x-speakeasy-name-override", "listRegistries")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListMCPRegistries"}`)
	})

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

	Method("getServerDetails", func() {
		Description("Get detailed information about an MCP server including remotes")

		Payload(func() {
			Attribute("registry_id", String, "ID of the registry", func() {
				Format(FormatUUID)
			})
			Attribute("server_specifier", String, "Server specifier (e.g., 'io.github.user/server')")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()

			Required("registry_id", "server_specifier")
		})

		Result(ExternalMCPServer)

		HTTP(func() {
			GET("/rpc/mcpRegistries.getServerDetails")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("registry_id")
			Param("server_specifier")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getMCPServerDetails")
		Meta("openapi:extension:x-speakeasy-name-override", "getServerDetails")
	})
})

var ExternalMCPServer = Type("ExternalMCPServer", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP server from an external registry")

	Attribute("registry_specifier", String, "Server specifier used to look up in the registry (e.g., 'io.github.user/server')", func() {
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
	Attribute("tools", ArrayOf(ExternalMCPTool), "Tools available on the server")
	Attribute("remotes", ArrayOf(ExternalMCPRemote), "Available remote endpoints for the server")

	Required("registry_specifier", "version", "description", "registry_id")
})

var MCPRegistry = Type("MCPRegistry", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP registry")

	Attribute("id", String, "Registry ID", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Display name for the registry")
	Attribute("url", String, "URL of the registry")

	Required("id", "name", "url")
})

var ExternalMCPTool = Type("ExternalMCPTool", func() {
	Meta("struct:pkg:path", "types")

	Attribute("name", String, "Name of the tool")
	Attribute("description", String, "Description of the tool")
	Attribute("input_schema", Any, "Input schema for the tool")
	Attribute("annotations", Any, "Annotations for the tool")
})

var ExternalMCPRemote = Type("ExternalMCPRemote", func() {
	Meta("struct:pkg:path", "types")

	Description("A remote endpoint for an MCP server")

	Attribute("url", String, "URL of the remote endpoint", func() {
		Format(FormatURI)
	})
	Attribute("transport_type", String, "Transport type (sse or streamable-http)", func() {
		Enum("sse", "streamable-http")
	})

	Required("url", "transport_type")
})
