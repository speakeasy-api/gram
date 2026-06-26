package tunnelledmcp

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("tunnelledMcp", func() {
	Description("Managing customer-hosted tunnelled MCP servers.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createServer", func() {
		Description("Create a new tunnelled MCP server source. Returns the tunnel key once.")

		Payload(func() {
			Extend(TunnelledMcpCreateServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(CreateServerResult)

		HTTP(func() {
			POST("/rpc/tunnelledMcp.createServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(TunnelledMcpCreateServerForm)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createTunnelledMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "createServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateTunnelledMcpServer"}`)
	})

	Method("listServers", func() {
		Description("List all tunnelled MCP server sources for a project")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListServersResult)

		HTTP(func() {
			GET("/rpc/tunnelledMcp.listServers")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listTunnelledMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "listServers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "TunnelledMcpServers"}`)
	})

	Method("getServer", func() {
		Description("Get a tunnelled MCP server by ID")

		Payload(func() {
			Attribute("id", String, "The ID of the tunnelled MCP server", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(TunnelledMcpServer)

		HTTP(func() {
			GET("/rpc/tunnelledMcp.getServer")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getTunnelledMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "getServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetTunnelledMcpServer"}`)
	})

	Method("updateServer", func() {
		Description("Update a tunnelled MCP server source")

		Payload(func() {
			Extend(TunnelledMcpUpdateServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(TunnelledMcpServer)

		HTTP(func() {
			POST("/rpc/tunnelledMcp.updateServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(TunnelledMcpUpdateServerForm)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateTunnelledMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "updateServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateTunnelledMcpServer"}`)
	})

	Method("deleteServer", func() {
		Description("Delete a tunnelled MCP server source")

		Payload(func() {
			Attribute("id", String, "The ID of the tunnelled MCP server to delete", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/tunnelledMcp.deleteServer")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteTunnelledMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteTunnelledMcpServer"}`)
	})
})

var TunnelledMcpCreateServerForm = Type("CreateTunnelledMcpServerForm", func() {
	Meta("openapi:typename", "CreateTunnelledMcpServerForm")

	Description("Form for creating a new tunnelled MCP server source")

	Attribute("name", String, "Human-readable display name for the tunnelled MCP server")
	Required("name")
})

var TunnelledMcpUpdateServerForm = Type("UpdateTunnelledMcpServerForm", func() {
	Meta("openapi:typename", "UpdateTunnelledMcpServerForm")

	Description("Form for updating a tunnelled MCP server source")

	Attribute("id", String, "The ID of the tunnelled MCP server to update", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Human-readable display name for the tunnelled MCP server")

	Required("id", "name")
})

var TunnelledMcpLifecycleStatus = Type("TunnelledMcpLifecycleStatus", String, func() {
	Description("Stored lifecycle status for a tunnelled MCP server source")
	Enum("created", "active", "revoked")
	Meta("struct:pkg:path", "types")
})

var TunnelledMcpConnectionStatus = Type("TunnelledMcpConnectionStatus", String, func() {
	Description("Derived live connection status for a tunnelled MCP server source")
	Enum("connected", "inactive", "never_connected")
	Meta("struct:pkg:path", "types")
})

var TunnelledMcpConnection = Type("TunnelledMcpConnection", func() {
	Meta("struct:pkg:path", "types")

	Attribute("session_id", String, "Gateway session ID for a live tunnel connection")
	Attribute("service_id", String, "Customer-declared stable ID for the MCP service behind this tunnel connection")
	Attribute("service_slug", String, "Customer-declared slug for the MCP service behind this tunnel connection")
	Attribute("service_version", String, "Customer-declared version of the MCP service behind this tunnel connection")
	Attribute("agent_version", String, "Tunnel agent version reported by the connection")
	Attribute("connected_at", String, func() {
		Description("When this tunnel session connected")
		Format(FormatDateTime)
	})
	Attribute("last_heartbeat_at", String, func() {
		Description("Most recent heartbeat observed for this tunnel session")
		Format(FormatDateTime)
	})
	Attribute("remote_addr", String, "Remote address reported by the gateway")
	Attribute("active_substreams", Int, "Number of active request substreams on this tunnel session")
	Attribute("active_consumer_sessions", Int, "Number of MCP consumer sessions currently pinned to this tunnel connection")
	Attribute("metadata", MapOf(String, String), "User-provided tunnel metadata reported by the agent")

	Required("session_id", "service_id", "service_slug", "service_version", "connected_at", "last_heartbeat_at", "active_substreams", "active_consumer_sessions", "metadata")
})

var TunnelledMcpServer = Type("TunnelledMcpServer", func() {
	Meta("struct:pkg:path", "types")

	Description("A customer-hosted MCP server connected through a tunnel")

	Attribute("id", String, "The ID of the tunnelled MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID this tunnelled MCP server belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Human-readable name for the tunnelled MCP server")
	Attribute("key_prefix", String, "Non-secret prefix of the tunnel key")
	Attribute("status", TunnelledMcpLifecycleStatus, "Stored lifecycle status")
	Attribute("connection_status", TunnelledMcpConnectionStatus, "Derived connection status")
	Attribute("agent_version", String, "Most recent agent version reported by the tunnel")
	Attribute("last_seen_at", String, func() {
		Description("Most recent persisted heartbeat timestamp")
		Format(FormatDateTime)
	})
	Attribute("connections", ArrayOf(TunnelledMcpConnection), "Live tunnel connections currently visible in Redis")
	Attribute("active_connection_count", Int, "Number of active tunnel connections currently visible in Redis")
	Attribute("active_consumer_session_count", Int, "Total MCP consumer sessions currently pinned across active tunnel connections")
	Attribute("created_at", String, func() {
		Description("When the tunnelled MCP server source was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the tunnelled MCP server source was last updated")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "name", "key_prefix", "status", "connection_status", "connections", "active_connection_count", "active_consumer_session_count", "created_at", "updated_at")
})

var CreateServerResult = Type("CreateTunnelledMcpServerResult", func() {
	Description("Created tunnelled MCP server plus the one-time tunnel key")

	Attribute("server", TunnelledMcpServer)
	Attribute("tunnel_key", String, "Plaintext tunnel key. Only returned at creation time.")

	Required("server", "tunnel_key")
})

var ListServersResult = Type("ListTunnelledMcpServersResult", func() {
	Description("Result type for listing tunnelled MCP servers")

	Attribute("tunnelled_mcp_servers", ArrayOf(TunnelledMcpServer))
	Required("tunnelled_mcp_servers")
})
