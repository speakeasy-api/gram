package mcpservers

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("mcpServers", func() {
	Description("Managing MCP servers, which configure authentication, environment, and backend selection for an MCP server.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createMcpServer", func() {
		Description("Create a new MCP server")

		Payload(func() {
			Extend(CreateMcpServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpServer)

		HTTP(func() {
			POST("/rpc/mcpServers.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateMcpServer"}`)
	})

	Method("getMcpServer", func() {
		Description("Get an MCP server by ID or slug. Exactly one of id or slug must be provided.")

		Payload(func() {
			Attribute("id", String, "The ID of the MCP server. Mutually exclusive with slug.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "The slug of the MCP server. Mutually exclusive with id.")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpServer)

		HTTP(func() {
			GET("/rpc/mcpServers.get")
			Param("id")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetMcpServer"}`)
	})

	Method("listMcpServers", func() {
		Description("List MCP servers for a project. Accepts optional remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id filters to scope the result to a single backend; at most one filter may be supplied since the backends are mutually exclusive.")

		Payload(func() {
			Attribute("remote_mcp_server_id", String, "Filter to MCP servers backed by this remote MCP server", func() {
				Format(FormatUUID)
			})
			Attribute("tunneled_mcp_server_id", String, "Filter to MCP servers backed by this tunneled MCP server", func() {
				Format(FormatUUID)
			})
			Attribute("toolset_id", String, "Filter to MCP servers backed by this toolset", func() {
				Format(FormatUUID)
			})
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListMcpServersResult)

		HTTP(func() {
			GET("/rpc/mcpServers.list")
			Param("remote_mcp_server_id")
			Param("tunneled_mcp_server_id")
			Param("toolset_id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "McpServers"}`)
	})

	Method("listMcpServersForOrg", func() {
		Description("List all MCP servers across the organization")

		// Session-only, unlike the rest of the service: this dashboard flow has
		// no project selector, and API-key auth is not RBAC-enforced, so a
		// project-scoped producer key could otherwise enumerate MCP servers
		// across every project in the organization.
		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListMcpServersResult)

		HTTP(func() {
			GET("/rpc/mcpServers.listForOrg")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMcpServersForOrg")
		Meta("openapi:extension:x-speakeasy-name-override", "listForOrg")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListMcpServersForOrg"}`)
	})

	Method("updateMcpServer", func() {
		Description("Update an MCP server. This is a full-record replace for the optional UUID references: fields omitted from the request become null on the stored record. name is an exception — omitting it leaves the existing display name unchanged, while providing it requires a non-empty value and recomputes the server-side slug. The id and visibility fields are required; exactly one of remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id must be provided.")

		Payload(func() {
			Extend(UpdateMcpServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(McpServer)

		HTTP(func() {
			POST("/rpc/mcpServers.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateMcpServer"}`)
	})

	Method("listToolFilters", func() {
		Description("List the tool filter scopes (tags) available on an MCP server and the tools under each, including tools excluded from all filters. Exactly one of id or slug must be provided. Read-only; reflects the explicit tool variations group resolved from the chain (mcp_servers then toolsets), deriving effective tags with the same logic as the runtime ?tags= filter. Returns filtering disabled when no explicit group is set.")

		Payload(func() {
			Attribute("id", String, "The ID of the MCP server. Mutually exclusive with slug.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "The slug of the MCP server. Mutually exclusive with id.")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(shared.ListToolFiltersResult)

		HTTP(func() {
			GET("/rpc/mcpServers.listToolFilters")
			Param("id")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMcpServerToolFilters")
		Meta("openapi:extension:x-speakeasy-name-override", "listToolFilters")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListMcpServerToolFilters"}`)
	})

	Method("setToolMetadataBatch", func() {
		Description("Authoritative batch upsert of tool metadata for an MCP server. Every tool in the payload is upserted and any stored tool absent from the payload is soft-deleted, all in one transaction.")

		Payload(func() {
			Attribute("mcp_server_id", String, "The ID of the MCP server the tool metadata belongs to", func() {
				Format(FormatUUID)
			})
			Attribute("tools", ArrayOf(ToolMetadataForm), "The authoritative set of tools for the MCP server. Stored tools absent from this list are soft-deleted.")
			Required("mcp_server_id", "tools")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(SetToolMetadataBatchResult)

		HTTP(func() {
			PUT("/rpc/mcpServers.setToolMetadataBatch")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setMcpServerToolMetadataBatch")
		Meta("openapi:extension:x-speakeasy-name-override", "setToolMetadataBatch")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetMcpServerToolMetadataBatch"}`)
	})

	Method("addToolMetadataBatch", func() {
		Description("Strictly additive batch insert of tool metadata for an MCP server. Every tool in the payload is inserted; if any of them already has a live stored entry the whole batch fails with a conflict and nothing is inserted. Stored tools absent from the payload are left untouched and nothing is deleted. Callers are expected to send only tools they know are new.")

		Payload(func() {
			Attribute("mcp_server_id", String, "The ID of the MCP server the tool metadata belongs to", func() {
				Format(FormatUUID)
			})
			Attribute("tools", ArrayOf(ToolMetadataForm), "The net-new tools to record. Every entry must be absent from the server's stored tool metadata.")
			Required("mcp_server_id", "tools")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(AddToolMetadataBatchResult)

		HTTP(func() {
			POST("/rpc/mcpServers.addToolMetadataBatch")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "addMcpServerToolMetadataBatch")
		Meta("openapi:extension:x-speakeasy-name-override", "addToolMetadataBatch")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AddMcpServerToolMetadataBatch"}`)
	})

	Method("listToolMetadata", func() {
		Description("List stored tool metadata for an MCP server.")

		Payload(func() {
			Attribute("mcp_server_id", String, "The ID of the MCP server the tool metadata belongs to", func() {
				Format(FormatUUID)
			})
			Attribute("include_deleted", Boolean, "Include soft-deleted tool metadata entries in the result. Deleted entries carry a deleted_at timestamp.")
			Required("mcp_server_id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListToolMetadataResult)

		HTTP(func() {
			GET("/rpc/mcpServers.listToolMetadata")
			Param("mcp_server_id")
			Param("include_deleted")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMcpServerToolMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "listToolMetadata")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListMcpServerToolMetadata"}`)
	})

	Method("setToolMetadata", func() {
		Description("Set the annotation hints of a single tool metadata entry (manual override). This is a full-record replace: omitted hints become unset on the stored record.")

		Payload(func() {
			Attribute("mcp_server_id", String, "The ID of the MCP server the tool metadata belongs to", func() {
				Format(FormatUUID)
			})
			Attribute("tool_name", String, "The name of the tool to update")
			toolMetadataAnnotationHints()
			Required("mcp_server_id", "tool_name")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ToolMetadata)

		HTTP(func() {
			PUT("/rpc/mcpServers.setToolMetadata")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setMcpServerToolMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "setToolMetadata")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "SetMcpServerToolMetadata"}`)
	})

	Method("deleteToolMetadata", func() {
		Description("Soft-delete a single tool metadata entry.")

		Payload(func() {
			Attribute("mcp_server_id", String, "The ID of the MCP server the tool metadata belongs to", func() {
				Format(FormatUUID)
			})
			Attribute("tool_name", String, "The name of the tool to delete")
			Required("mcp_server_id", "tool_name")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/mcpServers.deleteToolMetadata")
			Param("mcp_server_id")
			Param("tool_name")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteMcpServerToolMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteToolMetadata")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteMcpServerToolMetadata"}`)
	})

	Method("deleteMcpServer", func() {
		Description("Delete an MCP server")

		Payload(func() {
			Attribute("id", String, "The ID of the MCP server to delete", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/mcpServers.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteMcpServer"}`)
	})
})

var McpServerVisibility = Type("McpServerVisibility", String, func() {
	Description("The visibility of an MCP server")
	Enum("disabled", "private", "public")
	Meta("struct:pkg:path", "types")
})

var CreateMcpServerForm = Type("CreateMcpServerForm", func() {
	Description("Form for creating a new MCP server. Exactly one of remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id must be provided.")

	Attribute("name", String, "A human-readable display name for the server")
	Attribute("environment_id", String, "The ID of the environment to associate with the server", func() {
		Format(FormatUUID)
	})
	Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("tunneled_mcp_server_id", String, "The ID of the tunneled MCP server to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, "The ID of the toolset to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("tool_variations_group_id", String, "The ID of the tool variations group enabling MCP tool filtering for this server. Omit to leave filtering disabled.", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", McpServerVisibility, "The visibility of the server")

	Required("name", "visibility")
})

var UpdateMcpServerForm = Type("UpdateMcpServerForm", func() {
	Description("Form for updating an MCP server. This is a full-record replace: fields omitted from the request become null on the stored record. The user session issuer cannot be changed after create. Exactly one of remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id must be provided. Omit name to leave the existing display name unchanged; the slug is recomputed server-side from the resulting name.")

	Attribute("id", String, "The ID of the MCP server to update", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "A human-readable display name for the server. Omit to leave the existing name unchanged; if provided, must be non-empty.")
	Attribute("environment_id", String, "The ID of the environment to associate with the server", func() {
		Format(FormatUUID)
	})
	Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("tunneled_mcp_server_id", String, "The ID of the tunneled MCP server to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, "The ID of the toolset to use as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("tool_variations_group_id", String, "The ID of the tool variations group enabling MCP tool filtering for this server. Omit to disable filtering (cleared to null, consistent with the full-record replace semantics of the other UUID references).", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", McpServerVisibility, "The visibility of the server")

	Required("id", "visibility")
})

var McpServer = Type("McpServer", func() {
	Meta("struct:pkg:path", "types")

	Description("An MCP server configuration: authentication, environment, and backend selection for an MCP server.")

	Attribute("id", String, "The ID of the MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID this MCP server belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "A human-readable display name for the server")
	Attribute("slug", String, "A URL-safe, project-unique slug derived server-side from the name and ID")
	Attribute("environment_id", String, "The ID of the environment associated with the server", func() {
		Format(FormatUUID)
	})
	Attribute("user_session_issuer_id", String, "The ID of the user session issuer that gates OAuth-based MCP client authentication for this server, if any.", func() {
		Format(FormatUUID)
	})
	Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server used as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("tunneled_mcp_server_id", String, "The ID of the tunneled MCP server used as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("toolset_id", String, "The ID of the toolset used as the backend", func() {
		Format(FormatUUID)
	})
	Attribute("tool_variations_group_id", String, "The ID of the tool variations group enabling MCP tool filtering for this server, if any.", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", McpServerVisibility, "The visibility of the server")
	Attribute("created_at", String, func() {
		Description("When the MCP server was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the MCP server was last updated")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "visibility", "created_at", "updated_at")
})

var ListMcpServersResult = Type("ListMcpServersResult", func() {
	Description("Result type for listing MCP servers")

	Attribute("mcp_servers", ArrayOf(McpServer))
	Required("mcp_servers")
})

// toolMetadataAnnotationHints declares the shared MCP tool annotation hint
// attributes carried by tool metadata payloads and results. Each hint is
// tri-state: true, false, or unset (unknown).
func toolMetadataAnnotationHints() {
	Attribute("title", String, "A human-readable title for the tool")
	Attribute("read_only_hint", Boolean, "Hint that the tool does not modify its environment")
	Attribute("destructive_hint", Boolean, "Hint that the tool may perform destructive updates to its environment")
	Attribute("idempotent_hint", Boolean, "Hint that calling the tool repeatedly with the same arguments has no additional effect")
	Attribute("open_world_hint", Boolean, "Hint that the tool may interact with an open world of external entities")
}

var ToolMetadataForm = Type("ToolMetadataForm", func() {
	Description("A single tool entry in a tool metadata batch.")

	Attribute("tool_name", String, "The name of the tool")
	toolMetadataAnnotationHints()
	Required("tool_name")
})

var ToolMetadata = Type("ToolMetadata", func() {
	Meta("struct:pkg:path", "types")

	Description("Stored metadata about a tool exposed by an MCP server. The annotation hints mirror MCP tool annotations and drive annotation-aware authorization.")

	Attribute("mcp_server_id", String, "The ID of the MCP server the tool metadata belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("tool_name", String, "The name of the tool")
	toolMetadataAnnotationHints()
	Attribute("created_at", String, func() {
		Description("When the tool metadata entry was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the tool metadata entry was last updated")
		Format(FormatDateTime)
	})
	Attribute("deleted_at", String, func() {
		Description("When the tool metadata entry was deleted. Only present on deleted entries returned by listToolMetadata with include_deleted.")
		Format(FormatDateTime)
	})

	Required("mcp_server_id", "tool_name", "created_at", "updated_at")
})

var SetToolMetadataBatchResult = Type("SetToolMetadataBatchResult", func() {
	Description("Result of an authoritative tool metadata batch upsert.")

	Attribute("tools", ArrayOf(ToolMetadata), "The stored tool metadata after the upsert")
	Attribute("deleted", Int, "The number of stored tools soft-deleted because they were absent from the payload")
	Required("tools", "deleted")
})

var AddToolMetadataBatchResult = Type("AddToolMetadataBatchResult", func() {
	Description("Result of a strictly additive tool metadata batch insert.")

	Attribute("tools", ArrayOf(ToolMetadata), "The tool metadata entries created by this call")
	Required("tools")
})

var ListToolMetadataResult = Type("ListToolMetadataResult", func() {
	Description("Result type for listing tool metadata")

	Attribute("tools", ArrayOf(ToolMetadata), "The stored tool metadata for the MCP server")
	Required("tools")
})
