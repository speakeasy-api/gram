package access

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("access", func() {
	Description("Manage roles and team member access control.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("listRoles", func() {
		Description("List all roles for the current organization.")
		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListRolesResult)

		HTTP(func() {
			GET("/rpc/access.listRoles")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRoles")
		Meta("openapi:extension:x-speakeasy-name-override", "listRoles")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Roles"}`)
	})

	Method("getRole", func() {
		Description("Get a role by ID.")
		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			Attribute("id", String, "The ID of the role.")
			Required("id")
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(RoleModel)

		HTTP(func() {
			GET("/rpc/access.getRole")
			Param("id")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRole")
		Meta("openapi:extension:x-speakeasy-name-override", "getRole")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Role"}`)
	})

	Method("createRole", func() {
		Description("Create a new custom role.")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Extend(CreateRoleForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(RoleModel)

		HTTP(func() {
			POST("/rpc/access.createRole")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "createRole")
		Meta("openapi:extension:x-speakeasy-name-override", "createRole")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateRole"}`)
	})

	Method("updateRole", func() {
		Description("Update an existing custom role.")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Extend(UpdateRoleForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(RoleModel)

		HTTP(func() {
			PUT("/rpc/access.updateRole")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateRole")
		Meta("openapi:extension:x-speakeasy-name-override", "updateRole")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateRole"}`)
	})

	Method("deleteRole", func() {
		Description("Delete a custom role (system roles cannot be deleted).")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Attribute("id", String, "The ID of the role to delete.")
			Required("id")
			security.ByKeyPayload()
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/access.deleteRole")
			Param("id")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteRole")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteRole")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteRole"}`)
	})

	Method("listScopes", func() {
		Description("List all available scopes and their resource types.")
		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListScopesResult)

		HTTP(func() {
			GET("/rpc/access.listScopes")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listScopes")
		Meta("openapi:extension:x-speakeasy-name-override", "listScopes")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListScopes"}`)
	})

	Method("listMembers", func() {
		Description("List all team members with their role assignments.")
		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListMembersResult)

		HTTP(func() {
			GET("/rpc/access.listMembers")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMembers")
		Meta("openapi:extension:x-speakeasy-name-override", "listMembers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Members"}`)
	})

	Method("listGrants", func() {
		Description("List the current user's effective grants, including inherited role grants.")
		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListUserGrantsResult)

		HTTP(func() {
			GET("/rpc/access.listGrants")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "listGrants")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Grants"}`)
	})

	Method("updateMemberRole", func() {
		Description("Change a team member's role assignment.")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Extend(UpdateMemberRoleForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(MemberModel)

		HTTP(func() {
			PUT("/rpc/access.updateMemberRole")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateMemberRole")
		Meta("openapi:extension:x-speakeasy-name-override", "updateMemberRole")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateMemberRole"}`)
	})

})

var RoleGrantModel = Type("RoleGrant", func() {
	Required("scope")

	Attribute("scope", String, func() {
		Description("The scope slug this grant applies to.")
		Enum("org:read", "org:admin", "build:read", "build:write", "mcp:read", "mcp:write", "mcp:connect")
	})
	Attribute("resources", ArrayOf(String), func() {
		Description("Resource allowlist. Null means unrestricted access. An array means only the listed resource IDs.")
	})
})

var RoleModel = Type("Role", func() {
	Required("id", "name", "description", "is_system", "grants", "member_count", "created_at", "updated_at")

	Attribute("id", String, "Unique role identifier.")
	Attribute("name", String, "Display name of the role.")
	Attribute("description", String, "Human-readable description.")
	Attribute("is_system", Boolean, "Whether this is a built-in system role that cannot be deleted.")
	Attribute("grants", ArrayOf(RoleGrantModel), "Scope grants assigned to this role.")
	Attribute("member_count", Int, "Number of members assigned to this role.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
})

var ListRolesResult = Type("ListRolesResult", func() {
	Required("roles")
	Attribute("roles", ArrayOf(RoleModel), "The roles in your organization.")
})

var ScopeModel = Type("ScopeDefinition", func() {
	Required("slug", "description", "resource_type")

	Attribute("slug", String, func() {
		Description("Unique scope identifier.")
		Enum("org:read", "org:admin", "build:read", "build:write", "mcp:read", "mcp:write", "mcp:connect")
	})
	Attribute("description", String, "What this scope protects.")
	Attribute("resource_type", String, func() {
		Description("The type of resource this scope applies to.")
		Enum("org", "project", "mcp")
	})
})

var ListScopesResult = Type("ListScopesResult", func() {
	Required("scopes")
	Attribute("scopes", ArrayOf(ScopeModel), "The scopes available in access control.")
})

var CreateRoleForm = Type("CreateRoleForm", func() {
	Required("name", "description", "grants")

	Attribute("name", String, "Display name for the role.")
	Attribute("description", String, "Description of what this role can do.")
	Attribute("grants", ArrayOf(RoleGrantModel), "Scope grants to assign.")
	Attribute("member_ids", ArrayOf(String), "Optional member IDs to additionally assign to this role on creation.")
})

var UpdateRoleForm = Type("UpdateRoleForm", func() {
	Required("id")

	Attribute("id", String, "The ID of the role to update.")
	Attribute("name", String, "Updated display name.")
	Attribute("description", String, "Updated description.")
	Attribute("grants", ArrayOf(RoleGrantModel), "Updated scope grants.")
	Attribute("member_ids", ArrayOf(String), "Optional member IDs to additionally assign to this role. Existing assignments are preserved.")
})

var MemberModel = Type("AccessMember", func() {
	Required("id", "name", "email", "role_id", "joined_at")

	Attribute("id", String, "User ID.")
	Attribute("name", String, "Display name.")
	Attribute("email", String, "Email address.")
	Attribute("photo_url", String, "Avatar URL.")
	Attribute("role_id", String, "Currently assigned role ID.")
	Attribute("joined_at", String, func() {
		Description("When the member joined the organization.")
		Format(FormatDateTime)
	})
})

var ListMembersResult = Type("ListMembersResult", func() {
	Required("members")
	Attribute("members", ArrayOf(MemberModel), "The members in your organization.")
})

var ListUserGrantsResult = Type("ListUserGrantsResult", func() {
	Required("grants")
	Attribute("grants", ArrayOf(RoleGrantModel), "The user's effective grants in this organization.")
})

var UpdateMemberRoleForm = Type("UpdateMemberRoleForm", func() {
	Required("user_id", "role_id")

	Attribute("user_id", String, "The user ID to update.")
	Attribute("role_id", String, "The new role ID to assign.")
})
