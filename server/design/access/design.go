package access

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

// ---------------------------------------------------------------------------
// Service: access — RBAC roles, grants, and member management
//
// Based on the RFC "Gram RBAC — Scope & Permission Design":
//   - 7 system-defined scopes: org:read, org:admin, build:read, build:write,
//     mcp:read, mcp:write, mcp:connect
//   - Grants are additive (union), no deny
//   - Each grant is unrestricted (resources=null) or allowlisted (resources=[...])
// ---------------------------------------------------------------------------

var _ = Service("access", func() {
	Description("Manage roles, grants, and team member access control.")
	Security(security.Session)
	shared.DeclareErrorResponses()

	// ── Roles ──────────────────────────────────────────────────────────

	Method("listRoles", func() {
		Description("List all roles for the current organization")

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
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListRoles"}`)
	})

	Method("getRole", func() {
		Description("Get a role by ID")

		Payload(func() {
			Attribute("id", String, "The ID of the role")
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
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "GetRole"}`)
	})

	Method("createRole", func() {
		Description("Create a new custom role")

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
		Description("Update an existing custom role")

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
		Description("Delete a custom role (system roles cannot be deleted)")

		Payload(func() {
			Attribute("id", String, "The ID of the role to delete")
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

	// ── Scopes ─────────────────────────────────────────────────────────

	Method("listScopes", func() {
		Description("List all available scopes and their resource types")

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

	// ── Members ────────────────────────────────────────────────────────

	Method("listMembers", func() {
		Description("List all team members with their role assignments")

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
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListMembers"}`)
	})

	Method("updateMemberRole", func() {
		Description("Change a team member's role assignment")

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
})

// ---------------------------------------------------------------------------
// Types — Roles
// ---------------------------------------------------------------------------

var ScopeModel = Type("ScopeDefinition", func() {
	Required("slug", "description", "resource_type")

	Attribute("slug", String, func() {
		Description("Unique scope identifier")
		Enum("org:read", "org:admin", "build:read", "build:write", "mcp:read", "mcp:write", "mcp:connect")
	})
	Attribute("description", String, "What this scope protects")
	Attribute("resource_type", String, func() {
		Description("The type of resource this scope applies to")
		Enum("org", "project", "mcp")
	})
})

var ListScopesResult = Type("ListScopesResult", func() {
	Required("scopes")
	Attribute("scopes", ArrayOf(ScopeModel))
})

var RoleGrantModel = Type("RoleGrant", func() {
	Required("scope")

	Attribute("scope", String, func() {
		Description("The scope slug this grant applies to")
		Enum("org:read", "org:admin", "build:read", "build:write", "mcp:read", "mcp:write", "mcp:connect")
	})
	Attribute("resources", ArrayOf(String), func() {
		Description("Resource allowlist. Null means unrestricted (all resources of this type in the org). An array means only the listed resource IDs.")
	})
})

var RoleModel = Type("Role", func() {
	Required("id", "name", "description", "is_system", "grants", "member_count", "created_at", "updated_at")

	Attribute("id", String, "Unique role identifier")
	Attribute("name", String, "Display name of the role")
	Attribute("description", String, "Human-readable description")
	Attribute("is_system", Boolean, "Whether this is a built-in system role that cannot be deleted")
	Attribute("grants", ArrayOf(RoleGrantModel), "Scope grants assigned to this role")
	Attribute("member_count", Int, "Number of members assigned to this role")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
})

var ListRolesResult = Type("ListRolesResult", func() {
	Required("roles")
	Attribute("roles", ArrayOf(RoleModel))
})

var CreateRoleForm = Type("CreateRoleForm", func() {
	Required("name", "description", "grants")

	Attribute("name", String, "Display name for the role")
	Attribute("description", String, "Description of what this role can do")
	Attribute("grants", ArrayOf(RoleGrantModel), "Scope grants to assign")
	Attribute("member_ids", ArrayOf(String), "Optional member IDs to assign on creation")
})

var UpdateRoleForm = Type("UpdateRoleForm", func() {
	Required("id")

	Attribute("id", String, "The ID of the role to update")
	Attribute("name", String, "Updated display name")
	Attribute("description", String, "Updated description")
	Attribute("grants", ArrayOf(RoleGrantModel), "Updated scope grants")
	Attribute("member_ids", ArrayOf(String), "Optional member IDs to reassign to this role")
})

// ---------------------------------------------------------------------------
// Types — Members
// ---------------------------------------------------------------------------

var MemberModel = Type("AccessMember", func() {
	Required("id", "name", "email", "role_id", "joined_at")

	Attribute("id", String, "User ID")
	Attribute("name", String, "Display name")
	Attribute("email", String, "Email address")
	Attribute("photo_url", String, "Avatar URL")
	Attribute("role_id", String, "Currently assigned role ID")
	Attribute("joined_at", String, func() {
		Description("When the member joined the organization")
		Format(FormatDateTime)
	})
})

var ListMembersResult = Type("ListMembersResult", func() {
	Required("members")
	Attribute("members", ArrayOf(MemberModel))
})

var UpdateMemberRoleForm = Type("UpdateMemberRoleForm", func() {
	Required("user_id", "role_id")

	Attribute("user_id", String, "The user ID to update")
	Attribute("role_id", String, "The new role ID to assign")
})
