package remotemcp

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("remoteMcp", func() {
	Description("Managing remote MCP servers.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createServer", func() {
		Description("Create a new remote MCP server")

		Payload(func() {
			Extend(CreateServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteMcpServer)

		HTTP(func() {
			POST("/rpc/remoteMcp.createServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createRemoteMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "createServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateRemoteMcpServer"}`)
	})

	Method("listServers", func() {
		Description("List all remote MCP servers for a project")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListServersResult)

		HTTP(func() {
			GET("/rpc/remoteMcp.listServers")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRemoteMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "listServers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoteMcpServers"}`)
	})

	Method("getServer", func() {
		Description("Get a remote MCP server by ID or slug. Exactly one of id or slug must be provided.")

		Payload(func() {
			Attribute("id", String, "The ID of the remote MCP server. Mutually exclusive with slug.", func() {
				Format(FormatUUID)
			})
			Attribute("slug", String, "The slug of the remote MCP server. Mutually exclusive with id.")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteMcpServer)

		HTTP(func() {
			GET("/rpc/remoteMcp.getServer")
			Param("id")
			Param("slug")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRemoteMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "getServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetRemoteMcpServer"}`)
	})

	Method("updateServer", func() {
		Description("Update a remote MCP server")

		Payload(func() {
			Extend(UpdateServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteMcpServer)

		HTTP(func() {
			POST("/rpc/remoteMcp.updateServer")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateRemoteMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "updateServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateRemoteMcpServer"}`)
	})

	Method("discoverProtectedResourceMetadata", func() {
		Description("Probe the remote MCP server's origin for an RFC 9728 .well-known/oauth-protected-resource document and return either the parsed metadata or a typed unavailability reason. Runs server-side under guardian.Policy so production resource servers without CORS can still be inspected.")

		Payload(func() {
			Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server to probe.", func() {
				Format(FormatUUID)
			})
			Required("remote_mcp_server_id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ProtectedResourceMetadataDiscovery)

		HTTP(func() {
			POST("/rpc/remoteMcp.discoverProtectedResourceMetadata")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "discoverRemoteMcpProtectedResourceMetadata")
		Meta("openapi:extension:x-speakeasy-name-override", "discoverProtectedResourceMetadata")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DiscoverRemoteMcpProtectedResourceMetadata"}`)
	})

	Method("verifyURL", func() {
		Description("Probe a candidate remote MCP server URL by issuing an MCP initialize request and reporting the outcome. Used to give users a reachability signal before they save a new or updated remote MCP server. Treats reachable-but-401/403 responses as verified — auth verification is intentionally out of scope.")

		Payload(func() {
			Extend(VerifyURLForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(VerifyURLResult)

		HTTP(func() {
			POST("/rpc/remoteMcp.verifyURL")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "verifyRemoteMcpURL")
		Meta("openapi:extension:x-speakeasy-name-override", "verifyURL")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "VerifyRemoteMcpURL"}`)
	})

	Method("deleteServer", func() {
		Description("Delete a remote MCP server")

		Payload(func() {
			Attribute("id", String, "The ID of the remote MCP server to delete")
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/remoteMcp.deleteServer")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteRemoteMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteRemoteMcpServer"}`)
	})

	Method("listServerHeaders", func() {
		Description("List the headers configured for a remote MCP server")

		Payload(func() {
			Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server", func() {
				Format(FormatUUID)
			})
			Required("remote_mcp_server_id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListServerHeadersResult)

		HTTP(func() {
			GET("/rpc/remoteMcp.listServerHeaders")
			Param("remote_mcp_server_id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRemoteMcpServerHeaders")
		Meta("openapi:extension:x-speakeasy-name-override", "listServerHeaders")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoteMcpServerHeaders"}`)
	})

	Method("getServerHeader", func() {
		Description("Get a remote MCP server header by ID")

		Payload(func() {
			Attribute("id", String, "The ID of the header", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteMcpServerHeader)

		HTTP(func() {
			GET("/rpc/remoteMcp.getServerHeader")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRemoteMcpServerHeader")
		Meta("openapi:extension:x-speakeasy-name-override", "getServerHeader")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetRemoteMcpServerHeader"}`)
	})

	Method("createServerHeader", func() {
		Description("Create a header on a remote MCP server")

		Payload(func() {
			Extend(CreateServerHeaderForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteMcpServerHeader)

		HTTP(func() {
			POST("/rpc/remoteMcp.createServerHeader")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createRemoteMcpServerHeader")
		Meta("openapi:extension:x-speakeasy-name-override", "createServerHeader")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateRemoteMcpServerHeader"}`)
	})

	Method("updateServerHeader", func() {
		Description("Update a remote MCP server header")

		Payload(func() {
			Extend(UpdateServerHeaderForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteMcpServerHeader)

		HTTP(func() {
			POST("/rpc/remoteMcp.updateServerHeader")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateRemoteMcpServerHeader")
		Meta("openapi:extension:x-speakeasy-name-override", "updateServerHeader")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateRemoteMcpServerHeader"}`)
	})

	Method("deleteServerHeader", func() {
		Description("Delete a remote MCP server header")

		Payload(func() {
			Attribute("id", String, "The ID of the header to delete", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/remoteMcp.deleteServerHeader")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteRemoteMcpServerHeader")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteServerHeader")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteRemoteMcpServerHeader"}`)
	})
})

var CreateServerForm = Type("CreateServerForm", func() {
	Description("Form for creating a new remote MCP server")

	Attribute("name", String, "Optional human-readable name for the remote MCP server. Empty values are stored as null.")
	Attribute("url", String, "The URL of the remote MCP server", func() {
		Format(FormatURI)
	})
	Attribute("transport_type", String, "The transport type for the remote MCP server (e.g. streamable-http)")

	Required("url", "transport_type")
})

var UpdateServerForm = Type("UpdateServerForm", func() {
	Description("Form for updating a remote MCP server")

	Attribute("id", String, "The ID of the remote MCP server to update")
	Attribute("name", String, "Optional human-readable name. Pass an empty string to clear the existing name.")
	Attribute("url", String, "The URL of the remote MCP server", func() {
		Format(FormatURI)
	})
	Attribute("transport_type", String, "The transport type for the remote MCP server")

	Required("id")
})

var CreateServerHeaderForm = Type("CreateServerHeaderForm", func() {
	Description("Form for creating a header on a remote MCP server. Exactly one of value or value_from_request_header must be provided.")

	Attribute("remote_mcp_server_id", String, "The ID of the remote MCP server to add the header to", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The header name")
	Attribute("description", String, "Description of the header")
	Attribute("is_required", Boolean, "Whether the header is required. Defaults to false.")
	Attribute("is_secret", Boolean, "Whether the header value is a secret. Defaults to false. Incompatible with value_from_request_header.")
	Attribute("value", String, "Static header value (mutually exclusive with value_from_request_header)")
	Attribute("value_from_request_header", String, "Name of the inbound request header to pass through (mutually exclusive with value)")

	Required("remote_mcp_server_id", "name")
})

var UpdateServerHeaderForm = Type("UpdateServerHeaderForm", func() {
	Description("Form for updating a remote MCP server header. Replaces every mutable field, so omitted optional fields are reset to their defaults. The one exception is value: omitting it for a header that is already secret preserves the stored value rather than clearing it.")

	Attribute("id", String, "The ID of the header to update", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The header name")
	Attribute("description", String, "Description of the header")
	Attribute("is_required", Boolean, "Whether the header is required. Defaults to false.")
	Attribute("is_secret", Boolean, "Whether the header value is a secret. Defaults to false. Incompatible with value_from_request_header.")
	Attribute("value", String, "Static header value (mutually exclusive with value_from_request_header). Omit on an existing secret header to preserve its stored value.")
	Attribute("value_from_request_header", String, "Name of the inbound request header to pass through (mutually exclusive with value)")

	Required("id", "name")
})

var ProtectedResourceMetadata = Type("ProtectedResourceMetadata", func() {
	Description("RFC 9728 OAuth Protected Resource Metadata advertised by a remote MCP server. Only fields the dashboard renders are typed; the RFC allows additional members.")

	Attribute("resource", String, "The resource server's identifier.")
	Attribute("authorization_servers", ArrayOf(String), "Authorization servers that can issue access tokens for this resource.")
	Attribute("scopes_supported", ArrayOf(String), "Scopes advertised by the resource server.")
	Attribute("bearer_methods_supported", ArrayOf(String), "Bearer token presentation methods accepted by the resource server.")
	Attribute("resource_documentation", String, "URL of human-readable documentation for the resource server.")
})

var ProtectedResourceMetadataUnavailable = Type("ProtectedResourceMetadataUnavailable", func() {
	Description("Reason an RFC 9728 protected resource metadata probe was unavailable. Surfaced when available is false.")

	Attribute("code", String, "Machine-readable failure code (e.g. not_found, http_error, transport_error, timeout, malformed, host_blocked, invalid_url). Intentionally a free-form string so adding new failure modes is not a breaking SDK change.")
	Attribute("message", String, "Human-readable summary of the unavailability reason, composed by the backend. Dashboards should render verbatim.")

	Required("code", "message")
})

var ProtectedResourceMetadataDiscovery = Type("ProtectedResourceMetadataDiscovery", func() {
	Description("Outcome of an RFC 9728 protected resource metadata probe against a remote MCP server. available=true exposes the parsed metadata; available=false exposes a typed unavailability reason. Always returned with HTTP 200 — probe failures (including 404 from upstream) are not errors at this layer because non-OAuth resource servers are an expected, normal outcome.")

	Attribute("available", Boolean, "True when the upstream advertised an RFC 9728 document. False for any unavailability reason — see the unavailable field for the cause.")
	Attribute("metadata", ProtectedResourceMetadata, "Parsed RFC 9728 document. Present when available is true.")
	Attribute("unavailable", ProtectedResourceMetadataUnavailable, "Reason the probe was unavailable. Present when available is false.")
	Attribute("discovery_warnings", ArrayOf(String), "Informational deviations from RFC 9728 detected on a successful probe (e.g. missing resource field, mismatched resource value). Empty when available is false.")

	Required("available", "discovery_warnings")
})

var VerifyURLForm = Type("VerifyURLForm", func() {
	Description("Form for probing a remote MCP server URL")

	Attribute("url", String, "The URL of the remote MCP server to probe", func() {
		Format(FormatURI)
	})
	Attribute("transport_type", String, "The transport type for the remote MCP server (e.g. streamable-http)")

	Required("url", "transport_type")
})

var VerifyURLResult = Type("VerifyURLResult", func() {
	Description("Outcome of a remote MCP server URL verification")

	Attribute("verified", Boolean, "Whether the URL responded in a way consistent with a remote MCP server")
	Attribute("http_status", Int, "HTTP status code returned by the URL, if any")
	Attribute("message", String, "Human-readable summary of the verification outcome")

	Required("verified", "message")
})

var RemoteMcpServer = Type("RemoteMcpServer", func() {
	Meta("struct:pkg:path", "types")

	Description("A remote MCP server configuration")

	Attribute("id", String, "The ID of the remote MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, "The project ID this remote MCP server belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Optional human-readable name for the remote MCP server")
	Attribute("slug", String, "URL-friendly slug derived from the URL and ID.")
	Attribute("url", String, "The URL of the remote MCP server", func() {
		Format(FormatURI)
	})
	Attribute("transport_type", String, "The transport type for the remote MCP server")
	Attribute("created_at", String, func() {
		Description("When the remote MCP server was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the remote MCP server was last updated")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "url", "transport_type", "created_at", "updated_at")
})

var RemoteMcpServerHeader = Type("RemoteMcpServerHeader", func() {
	Meta("struct:pkg:path", "types")

	Description("A header configured for a remote MCP server")

	Attribute("id", String, "The ID of the header", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The header name")
	Attribute("description", String, "Description of the header")
	Attribute("is_required", Boolean, "Whether the header is required")
	Attribute("is_secret", Boolean, "Whether the header value is a secret")
	Attribute("value", String, "The header value (redacted if secret)")
	Attribute("value_from_request_header", String, "Name of the inbound request header to pass through")
	Attribute("created_at", String, func() {
		Description("When the header was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the header was last updated")
		Format(FormatDateTime)
	})

	Required("id", "name", "is_required", "is_secret", "created_at", "updated_at")
})

var ListServersResult = Type("ListServersResult", func() {
	Description("Result type for listing remote MCP servers")

	Attribute("remote_mcp_servers", ArrayOf(RemoteMcpServer))
	Required("remote_mcp_servers")
})

var ListServerHeadersResult = Type("ListServerHeadersResult", func() {
	Description("Result type for listing the headers of a remote MCP server")

	Attribute("headers", ArrayOf(RemoteMcpServerHeader))
	Required("headers")
})
