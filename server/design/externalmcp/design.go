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
			Attribute("servers", ArrayOf(ExternalMCPServerEntry), "List of available MCP servers")
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

	Method("getSetupDocs", func() {
		Description("Get the published setup documentation for an upstream MCP server, located by endpoint URL and/or registry identifier")

		Payload(func() {
			Attribute("server_url", String, "URL of the upstream MCP server endpoint", func() {
				Example("https://mcp.box.com")
			})
			Attribute("registry_specifier", String, "Registry specifier for the server, as returned by listCatalog (e.g., 'com.pulsemcp.mirror/box')", func() {
				Example("com.pulsemcp.mirror/box")
			})

			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(func() {
			Attribute("guides", ArrayOf(MCPSetupGuide), "Matching setup guides, most specific match first. Empty when no guide has been published for the server.")
			Required("guides")
		})

		HTTP(func() {
			GET("/rpc/mcpRegistries.getSetupDocs")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("server_url")
			Param("registry_specifier")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getMCPSetupDocs")
		Meta("openapi:extension:x-speakeasy-name-override", "getSetupDocs")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetMCPSetupDocs"}`)
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
	Attribute("toolset_id", String, "ID of the attached toolset when this server is listed from a Collection (toolset-backed attachment)", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_id", String, "ID of the attached MCP server when this server is listed from a Collection (mcp_server-backed attachment)", func() {
		Format(FormatUUID)
	})
	Attribute("registry_id", String, "ID of the external MCP registry this server came from", func() {
		Format(FormatUUID)
	})
	Attribute("organization_mcp_collection_registry_id", String, "ID of the internal collection registry this server came from", func() {
		Format(FormatUUID)
	})
	Attribute("title", String, "Display name for the server")
	Attribute("icon_url", String, "URL to the server's icon", func() {
		Format(FormatURI)
	})
	Attribute("meta", Any, "Opaque metadata from the registry")
	Attribute("tools", ArrayOf(ExternalMCPTool), "Tools available on the server")
	Attribute("remotes", ArrayOf(ExternalMCPRemote), "Available remote endpoints for the server")

	Required("registry_specifier", "version", "description")
})

// ExternalMCPServerEntry is the lightweight shape returned by the catalog list
// endpoint. It carries everything the index view needs (header, install state,
// usage/auth metadata, remotes) but omits the per-tool definitions, which are
// large and unused by the list. tool_count and is_read_only are precomputed so
// cards and the tool-behavior filter don't need the tools. The detail page
// fetches full tools via getServerDetails (ExternalMCPServer).
var ExternalMCPServerEntry = Type("ExternalMCPServerEntry", func() {
	Meta("struct:pkg:path", "types")

	Description("A summary of an MCP server from an external registry, returned by catalog listings")

	Attribute("registry_specifier", String, "Server specifier used to look up in the registry (e.g., 'io.github.user/server')", func() {
		Example("io.modelcontextprotocol.anonymous/exa")
	})
	Attribute("version", String, "Semantic version of the server", func() {
		Example("1.0.0")
	})
	Attribute("description", String, "Description of what the server does")
	Attribute("toolset_id", String, "ID of the attached toolset when this server is listed from a Collection (toolset-backed attachment)", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_id", String, "ID of the attached MCP server when this server is listed from a Collection (mcp_server-backed attachment)", func() {
		Format(FormatUUID)
	})
	Attribute("registry_id", String, "ID of the external MCP registry this server came from", func() {
		Format(FormatUUID)
	})
	Attribute("organization_mcp_collection_registry_id", String, "ID of the internal collection registry this server came from", func() {
		Format(FormatUUID)
	})
	Attribute("title", String, "Display name for the server")
	Attribute("icon_url", String, "URL to the server's icon", func() {
		Format(FormatURI)
	})
	Attribute("meta", Any, "Opaque metadata from the registry")
	Attribute("tool_count", Int, "Number of tools the server exposes")
	Attribute("is_read_only", Boolean, "Whether every tool on the server is read-only")
	Attribute("supports_dcr", Boolean, "Whether the server's OAuth authorization server advertises a dynamic client registration endpoint (RFC 7591). When false, connecting requires manual setup (static OAuth client credentials or API keys).")
	Attribute("remotes", ArrayOf(ExternalMCPRemote), "Available remote endpoints for the server")
	Attribute("repository", ExternalMCPRepository, "The source repository the registry links for this server, when it declares one")
	Attribute("packages", ArrayOf(ExternalMCPPackage), "Published packages that run this server, when the registry declares any")

	// tool_count, is_read_only, and supports_dcr are always computed for every catalog entry.
	Required("registry_specifier", "version", "description", "tool_count", "is_read_only", "supports_dcr")
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

var MCPSetupGuide = Type("MCPSetupGuide", func() {
	Meta("struct:pkg:path", "types")

	Description("Published setup documentation for an upstream MCP server")

	Attribute("slug", String, "Stable identifier of the guide", func() {
		Example("box")
	})
	Attribute("title", String, "Display title of the guide", func() {
		Example("Box")
	})
	Attribute("summary", String, "One-line summary of what the guide covers")
	Attribute("add_server_flow", String, "How the server is meant to be added in Gram, when the guide states one (e.g., 'catalog', 'custom-remote')")
	Attribute("aliases", ArrayOf(String), "Registry identifiers the guide is also published under")
	Attribute("remotes", ArrayOf(MCPSetupGuideRemote), "Endpoints documented by the guide")
	Attribute("matched_remote_id", String, "ID of the documented endpoint the lookup matched. Absent when the lookup identified the guide and not a specific endpoint, which is always the case for an 'alias' match.", func() {
		Example("hosted")
	})
	Attribute("match_kind", String, "How the lookup matched this guide. The most specific kind, when both lookup keys matched it.", func() {
		Enum("endpoint", "alias")
	})
	Attribute("external_markdown", String, "Markdown instructions for the setup work that happens in the upstream provider")
	Attribute("speakeasy_markdown", String, "Markdown instructions for the setup work that happens in Gram")

	Required("slug", "title", "summary", "aliases", "remotes", "match_kind", "external_markdown", "speakeasy_markdown")
})

var MCPSetupGuideRemote = Type("MCPSetupGuideRemote", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP server endpoint documented by a setup guide")

	Attribute("id", String, "Stable identifier of the endpoint within the guide", func() {
		Example("hosted")
	})
	Attribute("url", String, "URL of the endpoint", func() {
		Format(FormatURI)
	})
	// Left an open string rather than an enum: the value is passthrough data from
	// the guides SDK that Gram does not control, so a closed enum would turn an
	// upstream publish into a decode failure in every generated client.
	Attribute("transport_type", String, "Transport type as published by the guide (e.g., 'streamable-http', 'sse')", func() {
		Example("streamable-http")
	})
	Attribute("tenanted", Boolean, "Whether the endpoint URL is customer-specific and has to be filled in per tenant")

	Required("id", "url", "transport_type", "tenanted")
})

var ExternalMCPTool = Type("ExternalMCPTool", func() {
	Meta("struct:pkg:path", "types")

	Attribute("name", String, "Name of the tool")
	Attribute("description", String, "Description of the tool")
	Attribute("input_schema", Any, "Input schema for the tool")
	Attribute("annotations", Any, "Annotations for the tool")
})

// ExternalMCPRepository and ExternalMCPPackage carry the registry's linked
// source repository and published packages. Both are registry declarations:
// nothing ties the linked repository or a package to what a remote endpoint
// actually runs, and consumers presenting them as evidence must say so.
var ExternalMCPRepository = Type("ExternalMCPRepository", func() {
	Meta("struct:pkg:path", "types")

	Description("The source repository a registry entry links for its server. A registry declaration: nothing verifies the endpoint runs this code.")

	Attribute("url", String, "Repository URL", func() {
		Format(FormatURI)
	})
	Attribute("source", String, "Hosting service the repository lives on, such as github")
	Attribute("subfolder", String, "Path within the repository holding the server, for monorepos")

	Required("url")
})

var ExternalMCPPackage = Type("ExternalMCPPackage", func() {
	Meta("struct:pkg:path", "types")

	Description("A published package that runs this server, as declared by the registry")

	Attribute("registry_type", String, "Package registry the artifact is published to, such as npm or pypi")
	Attribute("registry_base_url", String, "Registry base URL when the package lives outside the default public registry", func() {
		Format(FormatURI)
	})
	Attribute("identifier", String, "Package identifier, scope included")
	Attribute("version", String, "Published version")
	Attribute("runtime_hint", String, "Launcher the publisher suggests, such as npx or uvx")
	Attribute("transport_type", String, "Execution transport the package declares, such as stdio")
	Attribute("environment_variables", ArrayOf(ExternalMCPPackageEnvironmentVariable), "Environment variables the package asks an install to supply. What a server demands — a required secret named here is an approval signal in its own right.")
	Attribute("file_sha256", String, "SHA-256 of the packaged artifact, when the registry publishes one")

	Required("registry_type", "identifier", "version")
})

var ExternalMCPPackageEnvironmentVariable = Type("ExternalMCPPackageEnvironmentVariable", func() {
	Meta("struct:pkg:path", "types")

	Description("An environment variable a package declares its install requires")

	Attribute("name", String, "Variable name the install must populate")
	Attribute("description", String, "The publisher's explanation of the variable. Untrusted text.")
	Attribute("is_secret", Boolean, "Whether the publisher marked the value sensitive")
	Attribute("is_required", Boolean, "Whether an install cannot proceed without it")

	Required("name", "is_secret", "is_required")
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
	Attribute("headers", ArrayOf(ExternalMCPRemoteHeader), "HTTP headers the MCP client should collect and send when connecting to this remote")
	Attribute("variables", MapOf(String, ExternalMCPRemoteVariable), "URL template variables for this remote endpoint")

	Required("url", "transport_type")
})

var ExternalMCPRemoteHeader = Type("ExternalMCPRemoteHeader", func() {
	Meta("struct:pkg:path", "types")

	Description("A header requirement for a remote MCP server")

	Attribute("name", String, "Header name")
	Attribute("description", String, "Description of the header")
	Attribute("is_secret", Boolean, "Whether this header value should be treated as secret")
	Attribute("is_required", Boolean, "Whether this header is required")
	Attribute("placeholder", String, "Placeholder value to show when collecting this header")

	Required("name")
})

var ExternalMCPRemoteVariable = Type("ExternalMCPRemoteVariable", func() {
	Meta("struct:pkg:path", "types")

	Description("A URL template variable for a remote MCP server")

	Attribute("description", String, "Description of the variable")
	Attribute("is_required", Boolean, "Whether this variable is required")
	Attribute("is_secret", Boolean, "Whether this variable value should be treated as secret")
	Attribute("default", String, "Default value for the variable")
	Attribute("choices", ArrayOf(String), "Allowed values for the variable")
})
