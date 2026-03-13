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

	Method("createPeer", func() {
		Description("Create a peered organization relationship (super org grants sub org access)")

		Payload(func() {
			Attribute("sub_organization_id", String, "ID of the sub organization to peer with")
			Required("sub_organization_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(PeeredOrganization)

		HTTP(func() {
			POST("/rpc/mcpRegistries.createPeer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "createMCPPeer")
		Meta("openapi:extension:x-speakeasy-name-override", "createPeer")
	})

	Method("listPeers", func() {
		Description("List peered organizations for the current organization")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("peers", ArrayOf(PeeredOrganization), "List of peered organizations")
			Required("peers")
		})

		HTTP(func() {
			GET("/rpc/mcpRegistries.listPeers")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMCPPeers")
		Meta("openapi:extension:x-speakeasy-name-override", "listPeers")
	})

	Method("deletePeer", func() {
		Description("Remove a peered organization relationship")

		Payload(func() {
			Attribute("sub_organization_id", String, "ID of the sub organization to remove")
			Required("sub_organization_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/mcpRegistries.deletePeer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("sub_organization_id")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteMCPPeer")
		Meta("openapi:extension:x-speakeasy-name-override", "deletePeer")
	})

	Method("publish", func() {
		Description("Publish toolsets as an internal MCP registry catalog")

		Payload(func() {
			Attribute("name", String, "Display name for the catalog", func() {
				MinLength(1)
				MaxLength(100)
			})
			Attribute("slug", String, "URL-friendly identifier for the catalog", func() {
				MinLength(1)
				MaxLength(100)
			})
			Attribute("toolset_ids", ArrayOf(String), "IDs of the toolsets to include", func() {
				MinLength(1)
			})
			Attribute("visibility", String, "Visibility of the catalog", func() {
				Enum("public", "private")
				Default("private")
			})
			Required("name", "slug", "toolset_ids")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MCPRegistry)

		HTTP(func() {
			POST("/rpc/mcpRegistries.publish")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "publishMCPRegistry")
		Meta("openapi:extension:x-speakeasy-name-override", "publish")
	})

	Method("grant", func() {
		Description("Grant an organization access to a private registry")

		Payload(func() {
			Attribute("registry_id", String, "ID of the registry to grant access to", func() {
				Format(FormatUUID)
			})
			Attribute("organization_id", String, "ID of the organization to grant access to")
			Required("registry_id", "organization_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			POST("/rpc/mcpRegistries.grant")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "grantMCPRegistryAccess")
		Meta("openapi:extension:x-speakeasy-name-override", "grant")
	})

	Method("revokeGrant", func() {
		Description("Revoke an organization's access to a private registry")

		Payload(func() {
			Attribute("registry_id", String, "ID of the registry to revoke access to", func() {
				Format(FormatUUID)
			})
			Attribute("organization_id", String, "ID of the organization to revoke access from")
			Required("registry_id", "organization_id")

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/mcpRegistries.revokeGrant")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("registry_id")
			Param("organization_id")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "revokeMCPRegistryAccess")
		Meta("openapi:extension:x-speakeasy-name-override", "revokeGrant")
	})

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
		Description("List MCP registries accessible to the current organization")

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

	Method("serve", func() {
		Description("Serve MCP servers from a specific registry by slug")

		Payload(func() {
			Attribute("registry_slug", String, "Slug of the registry to serve")
			Attribute("search", String, "Search query to filter servers by name")
			Attribute("cursor", String, "Pagination cursor")
			Required("registry_slug")

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
			GET("/rpc/mcpRegistries.serve")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("registry_slug")
			Param("search")
			Param("cursor")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "serveMCPRegistry")
		Meta("openapi:extension:x-speakeasy-name-override", "serve")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ServeMCPRegistry"}`)
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
	Attribute("slug", String, "URL-friendly identifier for the registry")
	Attribute("source", String, "Source type of the registry", func() {
		Enum("internal", "external")
	})
	Attribute("visibility", String, "Visibility of the registry", func() {
		Enum("public", "private")
	})
	Attribute("organization_id", String, "Owning organization ID")

	Required("id", "name")
})

var ExternalMCPTool = Type("ExternalMCPTool", func() {
	Meta("struct:pkg:path", "types")

	Attribute("name", String, "Name of the tool")
	Attribute("description", String, "Description of the tool")
	Attribute("input_schema", Any, "Input schema for the tool")
	Attribute("annotations", Any, "Annotations for the tool")
})

var PeeredOrganization = Type("PeeredOrganization", func() {
	Meta("struct:pkg:path", "types")

	Description("A peered organization relationship")

	Attribute("id", String, "Peer relationship ID", func() {
		Format(FormatUUID)
	})
	Attribute("super_organization_id", String, "ID of the super (granting) organization")
	Attribute("sub_organization_id", String, "ID of the sub (granted) organization")
	Attribute("sub_organization_name", String, "Name of the sub organization")
	Attribute("sub_organization_slug", String, "Slug of the sub organization")
	Attribute("created_at", String, "When the peer relationship was created")

	Required("id", "super_organization_id", "sub_organization_id", "created_at")
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
