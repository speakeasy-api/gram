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

	Method("serve", func() {
		Description("List published MCP servers from a collection")

		Payload(func() {
			Attribute("collection_slug", String, "Slug of the collection to serve")
			Required("collection_slug")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("servers", ArrayOf(ExternalMCPServer), "List of available MCP servers")
			Required("servers")
		})

		HTTP(func() {
			GET("/rpc/mcpRegistries.serve")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("collection_slug")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "serveMCPCollection")
		Meta("openapi:extension:x-speakeasy-name-override", "serve")
	})

	Method("createCollection", func() {
		Description("Create an MCP collection within the organization")

		Payload(func() {
			Attribute("name", String, "Display name for the collection", func() {
				MinLength(1)
				MaxLength(100)
			})
			Attribute("slug", String, "URL-friendly identifier for the collection", func() {
				MinLength(1)
				MaxLength(60)
			})
			Attribute("description", String, "Description of the collection", func() {
				MaxLength(500)
			})
			Attribute("mcp_registry_namespace", String, "Registry namespace (e.g., 'com.speakeasy.acme.my-tools')", func() {
				MinLength(1)
				MaxLength(200)
			})
			Attribute("visibility", String, "Visibility of the collection", func() {
				Enum("public", "private")
				Default("private")
			})
			Attribute("toolset_ids", ArrayOf(String), "Toolset IDs to attach to the collection")
			Required("name", "slug", "mcp_registry_namespace")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MCPCollection)

		HTTP(func() {
			POST("/rpc/mcpRegistries.createCollection")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "createMCPCollection")
		Meta("openapi:extension:x-speakeasy-name-override", "createCollection")
	})

	Method("listCollections", func() {
		Description("List MCP collections in the organization")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("collections", ArrayOf(MCPCollection), "List of collections")
			Required("collections")
		})

		HTTP(func() {
			GET("/rpc/mcpRegistries.listCollections")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMCPCollections")
		Meta("openapi:extension:x-speakeasy-name-override", "listCollections")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListMCPCollections"}`)
	})

	Method("updateCollection", func() {
		Description("Update an MCP collection")

		Payload(func() {
			Attribute("collection_id", String, "ID of the collection to update", func() {
				Format(FormatUUID)
			})
			Attribute("name", String, "Display name for the collection", func() {
				MinLength(1)
				MaxLength(100)
			})
			Attribute("description", String, "Description of the collection", func() {
				MaxLength(500)
			})
			Attribute("visibility", String, "Visibility of the collection", func() {
				Enum("public", "private")
			})
			Required("collection_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MCPCollection)

		HTTP(func() {
			PATCH("/rpc/mcpRegistries.updateCollection")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateMCPCollection")
		Meta("openapi:extension:x-speakeasy-name-override", "updateCollection")
	})

	Method("deleteCollection", func() {
		Description("Delete an MCP collection")

		Payload(func() {
			Attribute("collection_id", String, "ID of the collection to delete", func() {
				Format(FormatUUID)
			})
			Required("collection_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/mcpRegistries.deleteCollection")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("collection_id")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteMCPCollection")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteCollection")
	})

	Method("attachServer", func() {
		Description("Attach a server (toolset) to a collection")

		Payload(func() {
			Attribute("collection_id", String, "ID of the collection", func() {
				Format(FormatUUID)
			})
			Attribute("toolset_id", String, "ID of the toolset to attach", func() {
				Format(FormatUUID)
			})
			Required("collection_id", "toolset_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MCPCollection)

		HTTP(func() {
			POST("/rpc/mcpRegistries.attachServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "attachServerToCollection")
		Meta("openapi:extension:x-speakeasy-name-override", "attachServer")
	})

	Method("detachServer", func() {
		Description("Detach a server (toolset) from a collection")

		Payload(func() {
			Attribute("collection_id", String, "ID of the collection", func() {
				Format(FormatUUID)
			})
			Attribute("toolset_id", String, "ID of the toolset to detach", func() {
				Format(FormatUUID)
			})
			Required("collection_id", "toolset_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/mcpRegistries.detachServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "detachServerFromCollection")
		Meta("openapi:extension:x-speakeasy-name-override", "detachServer")
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

var MCPCollection = Type("MCPCollection", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP collection within an organization")

	Attribute("id", String, "Collection ID", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Display name for the collection")
	Attribute("description", String, "Description of the collection")
	Attribute("slug", String, "URL-friendly identifier")
	Attribute("mcp_registry_namespace", String, "Registry namespace")
	Attribute("visibility", String, "Visibility of the collection", func() {
		Enum("public", "private")
	})

	Required("id", "name", "slug", "visibility")
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
