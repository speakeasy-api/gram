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

	Method("listServerConnections", func() {
		Description("List live tunnel connections for a tunneled MCP server")

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
			GET("/rpc/tunneledMcp.listServerConnections")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listTunneledMcpServerConnections")
		Meta("openapi:extension:x-speakeasy-name-override", "listServerConnections")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListTunneledMcpServerConnections"}`)
	})

	Method("getServerMetrics", func() {
		Description("Read seven-day payload-free tunnel activity aggregates")
		Payload(func() {
			Attribute("id", String, func() { Format(FormatUUID) })
			Attribute("window", String, func() { Enum("hour", "day", "week"); Default("day") })
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})
		Result(TunnelMetrics)
		HTTP(func() {
			GET("/rpc/tunneledMcp.getServerMetrics")
			Param("id")
			Param("window")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})
		Meta("openapi:operationId", "getTunneledMcpServerMetrics")
		Meta("openapi:extension:x-speakeasy-name-override", "getServerMetrics")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name":"GetTunneledMcpServerMetrics"}`)
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
	Attribute("resource_identifier", String, "RFC 9728 protected resource identifier of the tunneled server, used for credential routing and as the signed caller assertion audience; never dialed by Gram. The exact identifier is preserved, including trailing slashes. When unset, caller assertions use tunneled-mcp-server:<ID>. Omit unless the identifier is already known; it is usually recorded later, once the tunnel is up.")
	Required("name")
})

var TunneledMcpUpdateServerForm = Type("UpdateTunneledMcpServerForm", func() {
	Meta("openapi:typename", "UpdateTunneledMcpServerForm")

	Description("Form for updating a tunneled MCP server source")

	Attribute("id", String, "The ID of the tunneled MCP server to update", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Human-readable display name for the tunneled MCP server. Omit to leave unchanged.")
	Attribute("allow_public", Boolean, "Consent to serve this source through a public, anonymous MCP endpoint. Disabling revokes all live anonymous sessions. Omit to leave unchanged.")
	Attribute("resource_identifier", String, "RFC 9728 protected resource identifier of the tunneled server, used for credential routing and as the signed caller assertion audience; never dialed by Gram. The exact identifier is preserved, including trailing slashes. When unset, caller assertions use tunneled-mcp-server:<ID>. Pass an empty string to clear. Omit to leave unchanged.")
	Attribute("public_request_rate_per_second", Int, "Sustained anonymous MCP requests per second admitted when this source is served through a public MCP endpoint. Applies to every MCP interaction; one bucket is shared by every caller. Omit to leave unchanged, 0 to clear back to the deployment default.", func() {
		Minimum(0)
		Maximum(100000)
	})
	Attribute("public_request_burst", Int, "Token-bucket capacity for public_request_rate_per_second: how many requests are admitted back-to-back from an idle tunnel before admission drops to the sustained rate. Each request takes one token; tokens refill at the sustained rate up to this cap. Omit to leave unchanged, 0 to clear (twice the sustained rate applies).", func() {
		Minimum(0)
		Maximum(1000000)
	})

	Required("id")
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

	Attribute("target_display", String, "Agent target scheme, hostname, port and path, without credentials, query or fragment")
	Attribute("diagnostics", TunnelDiagnostics, "Optional agent diagnostics, absent for legacy agents")
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
	Attribute("allow_public", Boolean, "Whether the owner has consented to serving this source through a public, anonymous MCP endpoint")
	Attribute("agent_version", String, "Most recent agent version reported by the tunnel")
	Attribute("resource_identifier", String, "RFC 9728 protected resource identifier of the tunneled server, used for credential routing and as the signed caller assertion audience; never dialed by Gram. The exact identifier is preserved, including trailing slashes. When unset, caller assertions use tunneled-mcp-server:<ID>")
	Attribute("public_request_rate_per_second", Int, "Sustained anonymous MCP requests per second admitted for this tunnel when it is served through a public MCP endpoint. Applies to every MCP interaction. Unset means the deployment-wide default applies.")
	Attribute("public_request_burst", Int, "Token-bucket capacity for public_request_rate_per_second: how many requests are admitted back-to-back from an idle tunnel before admission drops to the sustained rate. Unset means twice the sustained rate.")
	Attribute("effective_public_request_rate_per_second", Int, "The sustained anonymous MCP request rate actually applied to this tunnel: the stored value, or the deployment default when no rate is stored.")
	Attribute("effective_public_request_burst", Int, "The token-bucket capacity actually applied to this tunnel: the stored burst when a rate is stored alongside it, twice the stored rate when only a rate is stored, or the deployment default when no rate is stored (a burst stored without a rate is ignored).")
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

	Required("id", "project_id", "name", "key_prefix", "status", "connection_status", "allow_public", "active_connection_count", "active_consumer_session_count", "created_at", "updated_at", "effective_public_request_rate_per_second", "effective_public_request_burst")
})

var TunneledMcpServerConnections = Type("TunneledMcpServerConnections", func() {
	Meta("struct:pkg:path", "types")

	Description("Live connection details for a tunneled MCP server")

	Attribute("collection_state", String, "available or unavailable: whether live connection storage could be read")
	Attribute("observed_at", String, "Server time when this view was read", func() { Format(FormatDateTime) })
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

var TunnelDiagnosticStep = Type("TunnelDiagnosticStep", func() {
	Meta("struct:pkg:path", "types")
	Attribute("state", String, "Probe state", func() { Enum("pass", "fail", "not_applicable", "not_tested") })
	Attribute("duration_ms", Int64, "Elapsed probe time in milliseconds")
	Attribute("failure", String, "Bounded failure category, never raw error text")
	Required("state", "duration_ms", "failure")
})
var TunnelHTTPProgress = Type("TunnelHTTPProgress", func() {
	Meta("struct:pkg:path", "types")
	Attribute("waiting_headers", Int64, "Requests awaiting final HTTP response headers at the last sample")
	Attribute("open_responses", Int64, "Open HTTP response bodies at the last sample; long-lived SSE may be expected")
	Required("waiting_headers", "open_responses")
})
var TunnelDiagnostics = Type("TunnelDiagnostics", func() {
	Meta("struct:pkg:path", "types")
	Attribute("state", String, "Diagnostic collection state", func() { Enum("pending", "available", "stale", "unavailable", "unsupported", "disabled") })
	Attribute("received_at", String, "Gateway receipt time", func() { Format(FormatDateTime) })
	Attribute("sample_age_ms", Int64, "Probe age at view time, -1 if never sampled")
	Attribute("target_state", String, "Reachable means transport only, not MCP success.", func() { Enum("pending", "reachable", "unreachable", "unknown") })
	Attribute("consecutive_failures", Int64, "Consecutive failed transport probes")
	Attribute("dns", TunnelDiagnosticStep)
	Attribute("tcp", TunnelDiagnosticStep)
	Attribute("tls", TunnelDiagnosticStep)
	Attribute("http_progress", TunnelHTTPProgress, "Optional aggregate request progress; absent for older agents")
	Attribute("requests_total", Int64, "HTTP attempts since agent process start")
	Attribute("transport_errors_total", Int64, "Transport errors since agent process start")
	Attribute("last_http_status", Int, "Last observed HTTP status, including authentication challenges")
	Attribute("last_http_response_age_ms", Int64, "HTTP response age at view time, -1 if absent")
	Attribute("last_transport_error", String, "Last transport failure category")
	Attribute("last_transport_error_age_ms", Int64, "Transport failure age at view time, -1 if absent")
	Required("state")
})

var TunnelMetricPoint = Type("TunnelMetricPoint", func() {
	Meta("struct:pkg:path", "types")
	Attribute("time", String, func() { Format(FormatDateTime) })
	Attribute("tool_calls", Int64, "Observed tools/call attempts")
	Attribute("tools_list", Int64, "Observed tools/list attempts")
	Attribute("other_requests", Int64, "Other observed MCP request attempts")
	Attribute("successes", Int64, "Terminal MCP successes completed in this bucket")
	Attribute("errors", Int64, "Terminal errors completed in this bucket")
	Attribute("canceled", Int64)
	Attribute("incomplete", Int64)
	Attribute("p50_ms", Int64, "Histogram upper bound for median completion latency, -1 means above 60 seconds")
	Attribute("p95_ms", Int64, "Histogram upper bound for p95 completion latency, -1 means above 60 seconds")
	Attribute("connections", Float64, "Average of time-aligned sums of observed gateway connections. Missing owners are not zero.")
	Attribute("consumer_sessions", Float64)
	Attribute("active_requests", Float64)
	Attribute("connections_opened", Int64)
	Attribute("request_coverage_samples", Int, "Observed producer-minute heartbeats; proves only observed coverage, not every replica")
	Attribute("collection_partial", Boolean, "An observed producer reported aggregate loss since its last restart")
	Attribute("coverage_samples", Int, "Number of observed 15-second gateway sampling slots")
	Required("time", "coverage_samples", "request_coverage_samples", "collection_partial")
})
var TunnelClientCount = Type("TunnelClientCount", func() {
	Meta("struct:pkg:path", "types")
	Attribute("family", String)
	Attribute("requests", Int64)
	Required("family", "requests")
})
var TunnelMetrics = Type("TunnelMetrics", func() {
	Meta("struct:pkg:path", "types")
	Attribute("state", String, "History availability", func() { Enum("available", "unavailable", "too_large") })
	Attribute("observed_at", String, func() { Format(FormatDateTime) })
	Attribute("last_sample_at", String, func() { Format(FormatDateTime) })
	Attribute("bucket_seconds", Int)
	Attribute("points", ArrayOf(TunnelMetricPoint))
	Attribute("clients", ArrayOf(TunnelClientCount))
	Attribute("active_servers", Int, "Distinct fronting MCP servers with requests in this range")
	Required("state", "observed_at", "bucket_seconds", "points", "clients", "active_servers")
})
