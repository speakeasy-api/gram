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
		Description("Get a remote MCP server by ID")

		Payload(func() {
			Attribute("id", String, "The ID of the remote MCP server")
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(RemoteMcpServer)

		HTTP(func() {
			GET("/rpc/remoteMcp.getServer")
			Param("id")
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
})

var HeaderInput = Type("HeaderInput", func() {
	Description("Input for a remote MCP server header")

	Attribute("name", String, "The header name")
	Attribute("description", String, "Description of the header")
	Attribute("is_required", Boolean, "Whether the header is required")
	Attribute("is_secret", Boolean, "Whether the header value is a secret")
	Attribute("value", String, "Static header value (mutually exclusive with value_from_request_header)")
	Attribute("value_from_request_header", String, "Name of the inbound request header to pass through (mutually exclusive with value)")

	Required("name")
})

var CreateServerForm = Type("CreateServerForm", func() {
	Description("Form for creating a new remote MCP server")

	Attribute("url", String, "The URL of the remote MCP server", func() {
		Format(FormatURI)
	})
	Attribute("transport_type", String, "The transport type for the remote MCP server (e.g. streamable-http)")
	Attribute("headers", ArrayOf(HeaderInput), "Headers to send when proxying requests to the remote server")

	Required("url", "transport_type", "headers")
})

var UpdateServerForm = Type("UpdateServerForm", func() {
	Description("Form for updating a remote MCP server. When headers is provided, it represents the complete desired set of headers — any existing headers not in the list will be removed.")

	Attribute("id", String, "The ID of the remote MCP server to update")
	Attribute("url", String, "The URL of the remote MCP server", func() {
		Format(FormatURI)
	})
	Attribute("transport_type", String, "The transport type for the remote MCP server")
	Attribute("headers", ArrayOf(HeaderInput), "The complete desired set of headers. Omit to leave headers unchanged. Provide an empty array to remove all headers.")

	Required("id")
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
	Attribute("url", String, "The URL of the remote MCP server", func() {
		Format(FormatURI)
	})
	Attribute("transport_type", String, "The transport type for the remote MCP server")
	Attribute("headers", ArrayOf(RemoteMcpServerHeader), "Headers configured for this remote MCP server")
	Attribute("created_at", String, func() {
		Description("When the remote MCP server was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the remote MCP server was last updated")
		Format(FormatDateTime)
	})

	Required("id", "project_id", "url", "transport_type", "headers", "created_at", "updated_at")
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
