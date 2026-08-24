package metamcp

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("metaMcp", func() {
	Description("Managing meta MCP servers: aggregate servers that front an explicitly managed set of MCP servers through a single endpoint.")
	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createMetaMcpServer", func() {
		Description("Create a new meta MCP server")

		Payload(func() {
			Extend(CreateMetaMcpServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MetaMcpServer)

		HTTP(func() {
			POST("/rpc/metaMcp.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createMetaMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateMetaMcpServer"}`)
	})

	Method("getMetaMcpServer", func() {
		Description("Get a meta MCP server by id")

		Payload(func() {
			Attribute("id", String, "The ID of the meta MCP server", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MetaMcpServer)

		HTTP(func() {
			GET("/rpc/metaMcp.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getMetaMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetMetaMcpServer"}`)
	})

	Method("listMetaMcpServers", func() {
		Description("List meta MCP servers for a project")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListMetaMcpServersResult)

		HTTP(func() {
			GET("/rpc/metaMcp.list")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMetaMcpServers")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "MetaMcpServers"}`)
	})

	Method("updateMetaMcpServer", func() {
		Description("Update a meta MCP server. This is a full-record replace: a user_session_issuer_id omitted from the request becomes null on the stored record. Visibility is the exception — omitting it preserves the stored value, so a caller that does not manage visibility cannot re-enable a disabled gateway by saving an unrelated field.")

		Payload(func() {
			Extend(UpdateMetaMcpServerForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MetaMcpServer)

		HTTP(func() {
			POST("/rpc/metaMcp.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateMetaMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateMetaMcpServer"}`)
	})

	Method("deleteMetaMcpServer", func() {
		Description("Delete a meta MCP server. Its live memberships and MCP endpoints are deleted along with it.")

		Payload(func() {
			Attribute("id", String, "The ID of the meta MCP server to delete", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/metaMcp.delete")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteMetaMcpServer")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteMetaMcpServer"}`)
	})

	Method("listMetaMcpMembers", func() {
		Description("List the members of a meta MCP server, ordered by sort order")

		Payload(func() {
			Attribute("meta_mcp_server_id", String, "The ID of the meta MCP server", func() {
				Format(FormatUUID)
			})
			Required("meta_mcp_server_id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListMetaMcpMembersResult)

		HTTP(func() {
			GET("/rpc/metaMcp.listMembers")
			Param("meta_mcp_server_id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMetaMcpMembers")
		Meta("openapi:extension:x-speakeasy-name-override", "listMembers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "MetaMcpMembers"}`)
	})

	Method("addMetaMcpMember", func() {
		Description("Add an MCP server to a meta MCP server's member set")

		Payload(func() {
			Extend(AddMetaMcpMemberForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MetaMcpMember)

		HTTP(func() {
			POST("/rpc/metaMcp.addMember")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "addMetaMcpMember")
		Meta("openapi:extension:x-speakeasy-name-override", "addMember")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AddMetaMcpMember"}`)
	})

	Method("updateMetaMcpMember", func() {
		Description("Update a meta MCP membership's sort order")

		Payload(func() {
			Extend(UpdateMetaMcpMemberForm)
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(MetaMcpMember)

		HTTP(func() {
			POST("/rpc/metaMcp.updateMember")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateMetaMcpMember")
		Meta("openapi:extension:x-speakeasy-name-override", "updateMember")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateMetaMcpMember"}`)
	})

	Method("removeMetaMcpMember", func() {
		Description("Remove a member from a meta MCP server")

		Payload(func() {
			Attribute("id", String, "The ID of the membership to remove", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/metaMcp.removeMember")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "removeMetaMcpMember")
		Meta("openapi:extension:x-speakeasy-name-override", "removeMember")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoveMetaMcpMember"}`)
	})
})

var MetaMcpServerVisibility = Type("MetaMcpServerVisibility", String, func() {
	Description("The visibility of a meta MCP server. Disabled refuses traffic; private requires a user session.")
	Enum("disabled", "private")
	Meta("struct:pkg:path", "types")
})

var CreateMetaMcpServerForm = Type("CreateMetaMcpServerForm", func() {
	Description("Form for creating a new meta MCP server. URL addressability is managed separately through MCP endpoints.")

	Attribute("name", String, "The display name of the meta MCP server", func() {
		MinLength(1)
		MaxLength(100)
	})
	Attribute("user_session_issuer_id", String, "The ID of the user session issuer used to authenticate callers. Omit for no issuer.", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", MetaMcpServerVisibility, "The visibility of the gateway. Defaults to private, which requires callers to authenticate.")

	Required("name")
})

var UpdateMetaMcpServerForm = Type("UpdateMetaMcpServerForm", func() {
	Description("Form for updating a meta MCP server. This is a full-record replace: a user_session_issuer_id omitted from the request becomes null on the stored record. Visibility is the exception — omitting it preserves the stored value, so a caller that does not manage visibility cannot re-enable a disabled gateway by saving an unrelated field.")

	Attribute("id", String, "The ID of the meta MCP server to update", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The display name of the meta MCP server", func() {
		MinLength(1)
		MaxLength(100)
	})
	Attribute("user_session_issuer_id", String, "The ID of the user session issuer used to authenticate callers. Omit for no issuer.", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", MetaMcpServerVisibility, "The visibility of the gateway. Omit to leave it unchanged.")

	Required("id", "name")
})

var MetaMcpServer = Type("MetaMcpServer", func() {
	Meta("struct:pkg:path", "types")

	Description("A meta MCP server: an aggregate server fronting an explicitly managed set of MCP servers. URL addressability lives on its MCP endpoints.")

	Attribute("id", String, "The ID of the meta MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "The organization this meta MCP server belongs to")
	Attribute("project_id", String, "The project this meta MCP server belongs to", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "The display name of the meta MCP server")
	Attribute("user_session_issuer_id", String, "The ID of the user session issuer used to authenticate callers. Null when no issuer is attached.", func() {
		Format(FormatUUID)
	})
	Attribute("visibility", MetaMcpServerVisibility, "The visibility of the gateway.")
	Attribute("created_at", String, func() {
		Description("When the meta MCP server was created")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the meta MCP server was last updated")
		Format(FormatDateTime)
	})

	Required("id", "organization_id", "project_id", "name", "visibility", "created_at", "updated_at")
})

var AddMetaMcpMemberForm = Type("AddMetaMcpMemberForm", func() {
	Description("Form for adding an MCP server to a meta MCP server's member set.")

	Attribute("meta_mcp_server_id", String, "The ID of the meta MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_id", String, "The ID of the MCP server to add as a member", func() {
		Format(FormatUUID)
	})
	Attribute("sort_order", Int, "The position of the member in the ordered member set. Defaults to 0.")

	Required("meta_mcp_server_id", "mcp_server_id")
})

var UpdateMetaMcpMemberForm = Type("UpdateMetaMcpMemberForm", func() {
	Description("Form for updating a meta MCP membership.")

	Attribute("id", String, "The ID of the membership to update", func() {
		Format(FormatUUID)
	})
	Attribute("sort_order", Int, "The position of the member in the ordered member set")

	Required("id", "sort_order")
})

var MetaMcpMember = Type("MetaMcpMember", func() {
	Meta("struct:pkg:path", "types")

	Description("A membership row linking an MCP server into a meta MCP server's member set.")

	Attribute("id", String, "The ID of the membership", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_id", String, "The ID of the member MCP server", func() {
		Format(FormatUUID)
	})
	Attribute("mcp_server_name", String, "The name of the member MCP server, when configured")
	Attribute("mcp_server_slug", String, "The slug of the member MCP server, when configured")
	Attribute("sort_order", Int, "The position of the member in the ordered member set")

	Required("id", "mcp_server_id", "sort_order")
})

var ListMetaMcpServersResult = Type("ListMetaMcpServersResult", func() {
	Description("Result type for listing meta MCP servers")

	Attribute("meta_mcp_servers", ArrayOf(MetaMcpServer))
	Required("meta_mcp_servers")
})

var ListMetaMcpMembersResult = Type("ListMetaMcpMembersResult", func() {
	Description("Result type for listing meta MCP members")

	Attribute("members", ArrayOf(MetaMcpMember))
	Required("members")
})
