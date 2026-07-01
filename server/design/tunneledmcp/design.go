package tunneledmcp

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("tunneledMcp", func() {
	Description("Managing customer-hosted tunneled MCP servers.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createServer", func() {
		Description("Create a new tunneled MCP server source. Returns the tunnel key once.")

		Payload(func() {
			Extend(TunneledMcpCreateServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(CreateServerResult)

		HTTP(func() {
			POST("/rpc/tunneledMcp.createServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(TunneledMcpCreateServerForm)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createTunneledMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "createServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateTunneledMcpServer"}`)
	})

	Method("listServers", func() {
		Description("List all tunneled MCP server sources for a project")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListServersResult)

		HTTP(func() {
			GET("/rpc/tunneledMcp.listServers")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listTunneledMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "listServers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "TunneledMcpServers"}`)
	})

	Method("getServer", func() {
		Description("Get a tunneled MCP server by ID")

		Payload(func() {
			Attribute("id", String, "The ID of the tunneled MCP server", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(TunneledMcpServer)

		HTTP(func() {
			GET("/rpc/tunneledMcp.getServer")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getTunneledMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "getServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetTunneledMcpServer"}`)
	})

	Method("getServerConnections", func() {
		Description("Get live tunnel connections for a tunneled MCP server")

		Payload(func() {
			Attribute("id", String, "The ID of the tunneled MCP server", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(TunneledMcpServerConnections)

		HTTP(func() {
			GET("/rpc/tunneledMcp.getServerConnections")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getTunneledMcpServerConnections")
		Meta("openapi:extension:x-speakeasy-name-override", "getServerConnections")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetTunneledMcpServerConnections"}`)
	})

	Method("updateServer", func() {
		Description("Update a tunneled MCP server source")

		Payload(func() {
			Extend(TunneledMcpUpdateServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(TunneledMcpServer)

		HTTP(func() {
			POST("/rpc/tunneledMcp.updateServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(TunneledMcpUpdateServerForm)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateTunneledMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "updateServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateTunneledMcpServer"}`)
	})

	Method("rotateServerKey", func() {
		Description("Rotate a tunneled MCP server source key. Returns the new tunnel key once.")

		Payload(func() {
			Extend(TunneledMcpRotateServerKeyForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RotateServerKeyResult)

		HTTP(func() {
			POST("/rpc/tunneledMcp.rotateServerKey")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Body(TunneledMcpRotateServerKeyForm)
			Response(StatusOK)
		})

		Meta("openapi:operationId", "rotateTunneledMcpServerKey")
		Meta("openapi:extension:x-speakeasy-name-override", "rotateServerKey")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RotateTunneledMcpServerKey"}`)
	})

	Method("deleteServer", func() {
		Description("Delete a tunneled MCP server source")

		Payload(func() {
			Attribute("id", String, "The ID of the tunneled MCP server to delete", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/tunneledMcp.deleteServer")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteTunneledMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteTunneledMcpServer"}`)
	})
})

var TunneledMcpCreateServerForm = Type("CreateTunneledMcpServerForm", func() {
	Meta("openapi:typename", "CreateTunneledMcpServerForm")

	Description("Form for creating a new tunneled MCP server source")

	Attribute("name", String, "Human-readable display name for the tunneled MCP server")
	Required("name")
})

var TunneledMcpUpdateServerForm = Type("UpdateTunneledMcpServerForm", func() {
	Meta("openapi:typename", "UpdateTunneledMcpServerForm")

	Description("Form for updating a tunneled MCP server source")

	Attribute("id", String, "The ID of the tunneled MCP server to update", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Human-readable display name for the tunneled MCP server")

	Required("id", "name")
})

var TunneledMcpRotateServerKeyForm = Type("RotateTunneledMcpServerKeyForm", func() {
	Meta("openapi:typename", "RotateTunneledMcpServerKeyForm")

	Description("Form for rotating a tunneled MCP server source key")

	Attribute("id", String, "The ID of the tunneled MCP server", func() {
		Format(FormatUUID)
	})

	Required("id")
})

var TunneledMcpLifecycleStatus = Type("TunneledMcpLifecycleStatus", String, func() {
	Description("Stored lifecycle status for a tunneled MCP server source")
	Enum("created", "active", "revoked")
	Meta("struct:pkg:path", "types")
})

var TunneledMcpConnectionStatus = Type("TunneledMcpConnectionStatus", String, func() {
	Description("Derived live connection status for a tunneled MCP server source")
	Enum("connected", "inactive", "never_connected")
	Meta("struct:pkg:path", "types")
})

var TunneledMcpConnection = Type("TunneledMcpConnection", func() {
	Meta("struct:pkg:path", "types")

	Attribute("gateway_session_id", String, "Gateway session ID for a live tunnel connection")
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

	Required("gateway_session_id", "service_version", "connected_at", "last_heartbeat_at", "active_substreams", "active_consumer_sessions", "metadata")
})

var TunneledMcpServer = Type("TunneledMcpServer", func() {
	Meta("struct:pkg:path", "types")

	Description("A customer-hosted MCP server connected through a tunnel")

	Attribute("id", String, "The ID of the tunneled MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID this tunneled MCP server belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Human-readable name for the tunneled MCP server")
	Attribute("key_prefix", String, "Non-secret prefix of the tunnel key")
	Attribute("status", TunneledMcpLifecycleStatus, "Stored lifecycle status")
	Attribute("connection_status", TunneledMcpConnectionStatus, "Derived connection status")
	Attribute("agent_version", String, "Most recent agent version reported by the tunnel")
	Attribute("last_seen_at", String, func() {
		Description("Most recent persisted heartbeat timestamp")
		Format(FormatDateTime)
	})
	Attribute("active_connection_count", Int, "Number of active tunnel connections currently visible in Redis")
	Attribute("active_consumer_session_count", Int, "Total MCP consumer sessions currently pinned across active tunnel connections")
	Attribute("created_at", String, func() {
		Description("When the tunneled MCP server source was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the tunneled MCP server source was last updated")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "name", "key_prefix", "status", "connection_status", "active_connection_count", "active_consumer_session_count", "created_at", "updated_at")
})

var TunneledMcpServerConnections = Type("TunneledMcpServerConnections", func() {
	Meta("struct:pkg:path", "types")

	Description("Live connection details for a tunneled MCP server")

	Attribute("connections", ArrayOf(TunneledMcpConnection), "Live tunnel connections currently visible in Redis")
	Attribute("active_connection_count", Int, "Number of active tunnel connections currently visible in Redis")
	Attribute("active_consumer_session_count", Int, "Total MCP consumer sessions currently pinned across active tunnel connections")

	Required("connections", "active_connection_count", "active_consumer_session_count")
})

var CreateServerResult = Type("CreateTunneledMcpServerResult", func() {
	Description("Created tunneled MCP server plus the one-time tunnel key")

	Attribute("server", TunneledMcpServer)
	Attribute("tunnel_key", String, "Plaintext tunnel key. Only returned at creation time.")

	Required("server", "tunnel_key")
})

var RotateServerKeyResult = Type("RotateTunneledMcpServerKeyResult", func() {
	Description("Rotated tunneled MCP server plus the one-time replacement tunnel key")

	Attribute("server", TunneledMcpServer)
	Attribute("tunnel_key", String, "Plaintext tunnel key. Only returned after rotation.")

	Required("server", "tunnel_key")
})

var ListServersResult = Type("ListTunneledMcpServersResult", func() {
	Description("Result type for listing tunneled MCP servers")

	Attribute("tunneled_mcp_servers", ArrayOf(TunneledMcpServer))
	Required("tunneled_mcp_servers")
})
