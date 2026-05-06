package access

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("access", func() {
	Description("Manage roles, team member access control, and authorization challenge events.")
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

	Method("getRBACStatus", func() {
		Description("Returns whether RBAC is currently enabled for the current organization.")
		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
		})

		Result(RBACStatus)

		HTTP(func() {
			GET("/rpc/access.getRBACStatus")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getRBACStatus")
		Meta("openapi:extension:x-speakeasy-name-override", "getRBACStatus")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RBACStatus"}`)
	})

	Method("enableRBAC", func() {
		Description("Enable RBAC for the current organization. Seeds default grants for system roles.")
		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/access.enableRBAC")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "enableRBAC")
		Meta("openapi:extension:x-speakeasy-name-override", "enableRBAC")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "EnableRBAC"}`)
	})

	Method("disableRBAC", func() {
		Description("Disable RBAC enforcement for the current organization.")
		Security(security.Session)

		Payload(func() {
			security.SessionPayload()
		})

		HTTP(func() {
			POST("/rpc/access.disableRBAC")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "disableRBAC")
		Meta("openapi:extension:x-speakeasy-name-override", "disableRBAC")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DisableRBAC"}`)
	})

	Method("listChallenges", func() {
		Description("List authz challenge events from ClickHouse, enriched with resolution state from PostgreSQL.")
		Security(security.ByKey, func() {
			Scope("consumer")
		})
		Security(security.Session)

		Payload(func() {
			Attribute("outcome", String, func() {
				Description("Filter by outcome.")
				Enum("allow", "deny")
			})
			Attribute("principal_urn", String, "Filter by principal URN.")
			Attribute("scope", String, "Filter by scope.")
			Attribute("project_id", String, "Filter to a specific project.")
			Attribute("resolved", Boolean, "Filter by resolution state. True = only resolved, false = only unresolved.")
			Attribute("limit", Int, func() {
				Description("Maximum number of results to return.")
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("offset", Int, func() {
				Description("Number of results to skip.")
				Default(0)
				Minimum(0)
			})
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListChallengesResult)

		HTTP(func() {
			GET("/rpc/access.listChallenges")
			Param("outcome")
			Param("principal_urn")
			Param("scope")
			Param("project_id")
			Param("resolved")
			Param("limit")
			Param("offset")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listChallenges")
		Meta("openapi:extension:x-speakeasy-name-override", "listChallenges")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Challenges"}`)
	})

	Method("resolveChallenge", func() {
		Description("Record resolutions for one or more denied authz challenges. The caller is responsible for assigning the role first.")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Extend(ResolveChallengeForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ResolveChallengesResult)

		HTTP(func() {
			POST("/rpc/access.resolveChallenge")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "resolveChallenge")
		Meta("openapi:extension:x-speakeasy-name-override", "resolveChallenge")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ResolveChallenge"}`)
	})

})

var SelectorModel = Type("Selector", func() {
	Description("A constraint that narrows which resources a grant applies to.")
	Required("resource_kind", "resource_id")

	Attribute("resource_kind", String, func() {
		Description("The kind of resource this selector targets.")
		Enum("project", "mcp", "org", "*")
	})
	Attribute("resource_id", String, func() {
		Description("The resource identifier, or '*' for all resources of this kind.")
	})
	Attribute("disposition", String, func() {
		Description("Tool disposition filter (MCP scopes only).")
		Enum("read_only", "destructive", "idempotent", "open_world")
	})
	Attribute("tool", String, func() {
		Description("Specific tool name filter (MCP scopes only).")
	})
})

var RoleGrantModel = Type("RoleGrant", func() {
	Required("scope")

	Attribute("scope", String, func() {
		Description("The scope slug this grant applies to.")
		Enum("org:read", "org:admin", "project:read", "project:write", "mcp:read", "mcp:write", "mcp:connect")
	})

	Attribute("selectors", ArrayOf(SelectorModel), func() {
		Description("Selector constraints. Null means unrestricted.")
	})
})

// The response for the ListUserGrants endpoint. This endpoint is special in that it returns the inherited scopes the primary scope grants.
var ListRoleGrantModel = Type("ListRoleGrant", func() {
	Required("scope")

	Attribute("scope", String, func() {
		Description("The scope slug this grant applies to.")
		Enum("org:read", "org:admin", "project:read", "project:write", "mcp:read", "mcp:write", "mcp:connect")
	})
	Attribute("sub_scopes", ArrayOf(String), func() {
		Description("The inherited scopes the primary scope grants.")
		Elem(func() {
			Enum("org:read", "org:admin", "project:read", "project:write", "mcp:read", "mcp:write", "mcp:connect")
		})
	})

	Attribute("selectors", ArrayOf(SelectorModel), func() {
		Description("Selector constraints. Null means unrestricted.")
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
		Enum("org:read", "org:admin", "project:read", "project:write", "mcp:read", "mcp:write", "mcp:connect")
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
	Attribute("grants", ArrayOf(ListRoleGrantModel), "The user's effective grants in this organization.")
})

var UpdateMemberRoleForm = Type("UpdateMemberRoleForm", func() {
	Required("user_id", "role_id")

	Attribute("user_id", String, "The user ID to update.")
	Attribute("role_id", String, "The new role ID to assign.")
})

var RBACStatus = Type("RBACStatus", func() {
	Required("rbac_enabled")
	Attribute("rbac_enabled", Boolean, "Whether RBAC enforcement is currently enabled for this organization.")
})

var AuthzChallengeModel = Type("AuthzChallenge", func() {
	Required("id", "timestamp", "organization_id", "principal_urn", "principal_type",
		"operation", "outcome", "reason", "scope", "role_slugs",
		"evaluated_grant_count", "matched_grant_count")

	Attribute("id", String, "Unique challenge identifier.")
	Attribute("timestamp", String, func() {
		Description("When the authz decision was made.")
		Format(FormatDateTime)
	})
	Attribute("organization_id", String, "Organization the principal was acting in.")
	Attribute("project_id", String, "Project scope (empty for org-level checks).")
	Attribute("principal_urn", String, "Principal URN e.g. user:<uuid> or api_key:<id>.")
	Attribute("principal_type", String, func() {
		Description("Kind of principal.")
		Enum("user", "api_key", "assistant")
	})
	Attribute("user_email", String, "Email when available.")
	Attribute("photo_url", String, "User avatar URL when available.")
	Attribute("operation", String, func() {
		Enum("require", "require_any", "filter")
	})
	Attribute("outcome", String, func() {
		Enum("allow", "deny", "error")
	})
	Attribute("reason", String, func() {
		Enum("grant_matched", "no_grants", "scope_unsatisfied", "invalid_check", "rbac_skipped_apikey", "dev_override")
	})
	Attribute("scope", String, "Scope that was checked.")
	Attribute("resource_kind", String, "Resource kind of the check.")
	Attribute("resource_id", String, "Resource ID of the check.")
	Attribute("role_slugs", ArrayOf(String), "Roles the principal had loaded.")
	Attribute("evaluated_grant_count", Int, "Total grants evaluated.")
	Attribute("matched_grant_count", Int, "Number of grants that matched.")

	// Resolution fields — null when unresolved.
	Attribute("resolved_at", String, func() {
		Description("When the challenge was resolved by an admin.")
		Format(FormatDateTime)
	})
	Attribute("resolution_type", String, func() {
		Description("How the challenge was resolved.")
		Enum("role_assigned", "dismissed")
	})
	Attribute("resolved_by", String, "URN of the admin who resolved.")
	Attribute("resolution_role_slug", String, "Role slug assigned (when resolution_type=role_assigned).")
})

var ListChallengesResult = Type("ListChallengesResult", func() {
	Required("challenges", "total")
	Attribute("challenges", ArrayOf(AuthzChallengeModel), "The challenge events.")
	Attribute("total", Int, "Total number of matching challenges for pagination.")
})

var ResolveChallengeForm = Type("ResolveChallengeForm", func() {
	Required("challenge_ids", "principal_urn", "scope", "resolution_type")

	Attribute("challenge_ids", ArrayOf(String), "IDs of the challenges in ClickHouse to resolve.")
	Attribute("principal_urn", String, "Principal that was denied.")
	Attribute("scope", String, "Scope that was denied.")
	Attribute("resource_kind", String, "Resource kind from the challenge.")
	Attribute("resource_id", String, "Resource ID from the challenge.")
	Attribute("resolution_type", String, func() {
		Description("How the challenge is being resolved.")
		Enum("role_assigned", "dismissed")
	})
	Attribute("role_slug", String, "Role slug to assign (required when resolution_type=role_assigned).")
})

var ChallengeResolutionModel = Type("ChallengeResolution", func() {
	Required("id", "organization_id", "challenge_id", "principal_urn", "scope",
		"resolution_type", "resolved_by", "created_at")

	Attribute("id", String, "Resolution record ID.")
	Attribute("organization_id", String, "Organization ID.")
	Attribute("challenge_id", String, "ClickHouse challenge ID.")
	Attribute("principal_urn", String, "Denied principal.")
	Attribute("scope", String, "Denied scope.")
	Attribute("resource_kind", String, "Resource kind.")
	Attribute("resource_id", String, "Resource ID.")
	Attribute("resolution_type", String, func() {
		Enum("role_assigned", "dismissed")
	})
	Attribute("role_slug", String, "Assigned role slug.")
	Attribute("resolved_by", String, "Admin who resolved.")
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
})

var ResolveChallengesResult = Type("ResolveChallengesResult", func() {
	Required("resolutions")
	Attribute("resolutions", ArrayOf(ChallengeResolutionModel), "The created resolution records.")
})
