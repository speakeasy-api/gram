package access

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("access", func() {
	Description("Manage roles, grants, and team member access control.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	Method("listRoles", func() {
		Description("List all roles for the current organization.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListRolesResult)

		HTTP(func() {
			GET("/rpc/access.listRoles")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listRoles")
		Meta("openapi:extension:x-speakeasy-name-override", "listRoles")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Roles"}`)
	})

	Method("getRole", func() {
		Description("Get a role by ID.")

		Payload(func() {
			Attribute("id", String, "The ID of the role.")
			Required("id")
			security.SessionPayload()
		})

		Result(RoleModel)

		HTTP(func() {
			GET("/rpc/access.getRole")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRole")
		Meta("openapi:extension:x-speakeasy-name-override", "getRole")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Role"}`)
	})

	Method("createRole", func() {
		Description("Create a new custom role.")

		Payload(func() {
			Extend(CreateRoleForm)
			security.SessionPayload()
		})

		Result(RoleModel)

		HTTP(func() {
			POST("/rpc/access.createRole")
			security.SessionHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "createRole")
		Meta("openapi:extension:x-speakeasy-name-override", "createRole")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateRole"}`)
	})

	Method("updateRole", func() {
		Description("Update an existing custom role.")

		Payload(func() {
			Extend(UpdateRoleForm)
			security.SessionPayload()
		})

		Result(RoleModel)

		HTTP(func() {
			PUT("/rpc/access.updateRole")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateRole")
		Meta("openapi:extension:x-speakeasy-name-override", "updateRole")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateRole"}`)
	})

	Method("deleteRole", func() {
		Description("Delete a custom role (system roles cannot be deleted).")

		Payload(func() {
			Attribute("id", String, "The ID of the role to delete.")
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/access.deleteRole")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteRole")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteRole")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteRole"}`)
	})

	Method("listScopes", func() {
		Description("List all available scopes and their resource types.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListScopesResult)

		HTTP(func() {
			GET("/rpc/access.listScopes")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listScopes")
		Meta("openapi:extension:x-speakeasy-name-override", "listScopes")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListScopes"}`)
	})

	Method("listMembers", func() {
		Description("List all team members with their role assignments.")

		Payload(func() {
			security.SessionPayload()
		})

		Result(ListMembersResult)

		HTTP(func() {
			GET("/rpc/access.listMembers")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listMembers")
		Meta("openapi:extension:x-speakeasy-name-override", "listMembers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Members"}`)
	})

	Method("updateMemberRole", func() {
		Description("Change a team member's role assignment.")

		Payload(func() {
			Extend(UpdateMemberRoleForm)
			security.SessionPayload()
		})

		Result(MemberModel)

		HTTP(func() {
			PUT("/rpc/access.updateMemberRole")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateMemberRole")
		Meta("openapi:extension:x-speakeasy-name-override", "updateMemberRole")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateMemberRole"}`)
	})

	Method("listGrants", func() {
		Description("List all permissions in your organization, optionally filtered to a specific user or role.")
		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			Attribute("principal_urn", String, func() {
				Description("Filter to a specific user or role (e.g. \"user:user_abc\", \"role:admin\"). Omit to return all grants.")
			})
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListGrantsResult)

		HTTP(func() {
			GET("/rpc/access.listGrants")
			Param("principal_urn")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Grants"}`)
	})

	Method("upsertGrants", func() {
		Description("Grant permissions to one or more users or roles. Safe to call multiple times — if a permission already exists it is left unchanged.")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Extend(GrantsForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(UpsertGrantsResult)

		HTTP(func() {
			POST("/rpc/access.upsertGrants")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "upsertGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "upsert")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpsertGrants"}`)
	})

	Method("removeGrants", func() {
		Description("Revoke specific permissions from users or roles. Each entry must exactly match an existing grant (who, what action, which resource).")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Extend(GrantsForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/access.removeGrants")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "removeGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "remove")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoveGrants"}`)
	})

	Method("removePrincipalGrants", func() {
		Description("Revoke all permissions for a specific user or role.")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Attribute("principal_urn", String, func() {
				Description("The user or role to revoke all permissions from (e.g. \"user:user_abc\", \"role:admin\").")
				Meta("struct:field:type", "urn.Principal", "github.com/speakeasy-api/gram/server/internal/urn")
			})
			Required("principal_urn")
			security.ByKeyPayload()
			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/access.removePrincipalGrants")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "removePrincipalGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "removePrincipal")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemovePrincipalGrants"}`)
	})
})

var GrantEntry = Type("GrantEntry", func() {
	Description("A permission entry identifying who it applies to, what action it covers, and which resource it targets.")
	Required("principal_urn", "scope", "resource")

	Attribute("principal_urn", String, func() {
		Description("The user or role this permission entry applies to (e.g. \"user:user_abc\", \"role:admin\").")
		Meta("struct:field:type", "urn.Principal", "github.com/speakeasy-api/gram/server/internal/urn")
	})
	Attribute("scope", String, func() {
		Description("The action being permitted (e.g. \"build:read\", \"mcp:connect\").")
		MinLength(3)
		MaxLength(60)
	})
	Attribute("resource", String, func() {
		Description("The resource this permission applies to. Use \"*\" for unrestricted access.")
		MaxLength(260)
	})
})

var GrantsForm = Type("GrantsForm", func() {
	Description("A batch of permission entries to apply to access-management operations.")
	Attribute("grants", ArrayOf(GrantEntry), func() {
		Description("The permissions to process.")
		MinLength(1)
		MaxLength(100)
	})
	Required("grants")
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

var Grant = Type("Grant", func() {
	Description("A permission record giving a user or role the ability to perform an action on a resource.")
	Required("id", "organization_id", "principal_urn", "principal_type", "scope", "resource", "created_at", "updated_at")

	Attribute("id", String, func() {
		Description("Unique identifier of this permission.")
		Format(FormatUUID)
	})
	Attribute("organization_id", String, "The organization this permission belongs to.")
	Attribute("principal_urn", String, func() {
		Description("The user or role that holds this permission (e.g. \"user:user_abc\", \"role:admin\").")
	})
	Attribute("principal_type", String, func() {
		Description("Whether the principal is a user or a role.")
	})
	Attribute("scope", String, func() {
		Description("The action this permission allows (e.g. \"build:read\", \"mcp:connect\").")
	})
	Attribute("resource", String, func() {
		Description("The resource this permission applies to. \"*\" means all resources.")
	})
	Attribute("created_at", String, func() {
		Description("When this permission was granted.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When this permission was last updated.")
		Format(FormatDateTime)
	})
})

var ListGrantsResult = Type("ListGrantsResult", func() {
	Required("grants")
	Attribute("grants", ArrayOf(Grant), "The permissions in your organization.")
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

var UpdateMemberRoleForm = Type("UpdateMemberRoleForm", func() {
	Required("user_id", "role_id")

	Attribute("user_id", String, "The user ID to update.")
	Attribute("role_id", String, "The new role ID to assign.")
})

var UpsertGrantsResult = Type("UpsertGrantsResult", func() {
	Required("grants")
	Attribute("grants", ArrayOf(Grant), "The permissions that were created or already existed.")
})
