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

	Method("updateMemberRoles", func() {
		Description("Update a team member's role assignments.")
		Security(security.ByKey, func() {
			Scope("producer")
		})
		Security(security.Session)

		Payload(func() {
			Extend(UpdateMemberRolesForm)
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(MemberModel)

		HTTP(func() {
			PUT("/rpc/access.updateMemberRoles")
			security.ByKeyHeader()
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateMemberRoles")
		Meta("openapi:extension:x-speakeasy-name-override", "updateMemberRoles")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateMemberRoles"}`)
	})

	Method("listShadowMCPApprovalRequests", func() {
		Description("List Shadow MCP approval requests for the current organization. Requires organization admin access because requests include requester and block details.")
		Security(security.Session)

		Payload(func() {
			Attribute("status", String, func() {
				Enum("requested", "approved", "denied")
			})
			Attribute("project_id", String, func() {
				Format(FormatUUID)
			})
			Attribute("limit", Int, func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("cursor", String, "Cursor for the next page of results.")
			security.SessionPayload()
		})

		Result(ListShadowMCPApprovalRequestsResult)

		HTTP(func() {
			GET("/rpc/access.listShadowMcpRequests")
			Param("status")
			Param("project_id")
			Param("limit")
			Param("cursor")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listShadowMCPApprovalRequests")
		Meta("openapi:extension:x-speakeasy-name-override", "listShadowMCPApprovalRequests")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ShadowMCPApprovalRequests"}`)
	})

	Method("createShadowMCPApprovalRequest", func() {
		Description("Create or return an active Shadow MCP approval request.")
		Security(security.Session)

		Payload(func() {
			Extend(CreateShadowMCPApprovalRequestForm)
			security.SessionPayload()
		})

		Result(ShadowMCPApprovalRequestModel)

		HTTP(func() {
			POST("/rpc/access.createShadowMcpRequest")
			security.SessionHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "createShadowMCPApprovalRequest")
		Meta("openapi:extension:x-speakeasy-name-override", "createShadowMCPApprovalRequest")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateShadowMCPApprovalRequest", "type": "mutation"}`)
	})

	Method("approveShadowMCPApprovalRequest", func() {
		Description("Approve a Shadow MCP request, creating an allow rule scoped to the organization or project.")
		Security(security.Session)

		Payload(func() {
			Extend(ApproveShadowMCPApprovalRequestForm)
			security.SessionPayload()
		})

		Result(ShadowMCPApprovalDecisionResult)

		HTTP(func() {
			POST("/rpc/access.approveShadowMcpRequest")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "approveShadowMCPApprovalRequest")
		Meta("openapi:extension:x-speakeasy-name-override", "approveShadowMCPApprovalRequest")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ApproveShadowMCPApprovalRequest", "type": "mutation"}`)
	})

	Method("denyShadowMCPApprovalRequest", func() {
		Description("Deny a Shadow MCP request and optionally create a deny rule.")
		Security(security.Session)

		Payload(func() {
			Extend(DenyShadowMCPApprovalRequestForm)
			security.SessionPayload()
		})

		Result(ShadowMCPApprovalDecisionResult)

		HTTP(func() {
			POST("/rpc/access.denyShadowMcpRequest")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "denyShadowMCPApprovalRequest")
		Meta("openapi:extension:x-speakeasy-name-override", "denyShadowMCPApprovalRequest")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DenyShadowMCPApprovalRequest", "type": "mutation"}`)
	})

	Method("listShadowMCPAccessRules", func() {
		Description("List managed Shadow MCP allow and deny rules.")
		Security(security.Session)

		Payload(func() {
			Attribute("disposition", String, func() {
				Enum("allowed", "denied")
			})
			Attribute("access_scope", String, func() {
				Enum("organization", "project")
			})
			Attribute("project_id", String, func() {
				Format(FormatUUID)
			})
			Attribute("limit", Int, func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("cursor", String, "Cursor for the next page of results.")
			security.SessionPayload()
		})

		Result(ListShadowMCPAccessRulesResult)

		HTTP(func() {
			GET("/rpc/access.listShadowMcpRules")
			Param("disposition")
			Param("access_scope")
			Param("project_id")
			Param("limit")
			Param("cursor")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listShadowMCPAccessRules")
		Meta("openapi:extension:x-speakeasy-name-override", "listShadowMCPAccessRules")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ShadowMCPAccessRules"}`)
	})

	Method("listShadowMCPInventory", func() {
		Description("List project-scoped Shadow MCP server inventory composed from observed URLs, telemetry usage, and access-rule state.")
		Security(security.Session)

		Payload(func() {
			Attribute("project_id", String, func() {
				Format(FormatUUID)
			})
			Attribute("limit", Int, func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("cursor", String, "Cursor for the next page of results.")
			Required("project_id")
			security.SessionPayload()
		})

		Result(ListShadowMCPInventoryResult)

		HTTP(func() {
			GET("/rpc/access.shadowMcp.inventory.list")
			Param("project_id")
			Param("limit")
			Param("cursor")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listShadowMCPInventory")
		Meta("openapi:extension:x-speakeasy-name-override", "listShadowMCPInventory")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ShadowMCPInventory"}`)
	})

	Method("listShadowMCPInventoryUsers", func() {
		Description("List users with observed telemetry usage for one project-scoped Shadow MCP server URL.")
		Security(security.Session)

		Payload(func() {
			Attribute("project_id", String, func() {
				Format(FormatUUID)
			})
			Attribute("server_url", String, "Shadow MCP server URL to expand.")
			Attribute("limit", Int, func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("cursor", String, "Cursor for the next page of results.")
			Required("project_id", "server_url")
			security.SessionPayload()
		})

		Result(ListShadowMCPInventoryUsersResult)

		HTTP(func() {
			GET("/rpc/access.shadowMcp.inventory.users.list")
			Param("project_id")
			Param("server_url")
			Param("limit")
			Param("cursor")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listShadowMCPInventoryUsers")
		Meta("openapi:extension:x-speakeasy-name-override", "listShadowMCPInventoryUsers")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ShadowMCPInventoryUsers"}`)
	})

	Method("allowShadowMCPInventoryServer", func() {
		Description("Allow a project-scoped Shadow MCP server URL by creating or updating its explicit URL access rule.")
		Security(security.Session)

		Payload(func() {
			Extend(ShadowMCPInventoryServerAccessForm)
			security.SessionPayload()
		})

		Result(ShadowMCPInventoryAccessStateResult)

		HTTP(func() {
			POST("/rpc/access.shadowMcp.inventory.allow")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "allowShadowMCPInventoryServer")
		Meta("openapi:extension:x-speakeasy-name-override", "allowShadowMCPInventoryServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AllowShadowMCPInventoryServer", "type": "mutation"}`)
	})

	Method("blockShadowMCPInventoryServer", func() {
		Description("Block a project-scoped Shadow MCP server URL by creating or updating its explicit URL access rule.")
		Security(security.Session)

		Payload(func() {
			Extend(ShadowMCPInventoryServerAccessForm)
			security.SessionPayload()
		})

		Result(ShadowMCPInventoryAccessStateResult)

		HTTP(func() {
			POST("/rpc/access.shadowMcp.inventory.block")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "blockShadowMCPInventoryServer")
		Meta("openapi:extension:x-speakeasy-name-override", "blockShadowMCPInventoryServer")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "BlockShadowMCPInventoryServer", "type": "mutation"}`)
	})

	Method("clearShadowMCPInventoryServerAccess", func() {
		Description("Clear the explicit project-scoped URL access rule for a Shadow MCP server URL.")
		Security(security.Session)

		Payload(func() {
			Required("project_id", "server_url")
			Attribute("project_id", String, func() {
				Format(FormatUUID)
			})
			Attribute("server_url", String)
			Attribute("reason", String)
			security.SessionPayload()
		})

		Result(ShadowMCPInventoryAccessStateResult)

		HTTP(func() {
			POST("/rpc/access.shadowMcp.inventory.clear")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "clearShadowMCPInventoryServerAccess")
		Meta("openapi:extension:x-speakeasy-name-override", "clearShadowMCPInventoryServerAccess")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ClearShadowMCPInventoryServerAccess", "type": "mutation"}`)
	})

	Method("createShadowMCPAccessRule", func() {
		Description("Create a managed Shadow MCP access rule.")
		Security(security.Session)

		Payload(func() {
			Extend(CreateShadowMCPAccessRuleForm)
			security.SessionPayload()
		})

		Result(CreateShadowMCPAccessRuleResult)

		HTTP(func() {
			POST("/rpc/access.createShadowMcpRule")
			security.SessionHeader()
			Response(StatusCreated)
		})

		Meta("openapi:operationId", "createShadowMCPAccessRule")
		Meta("openapi:extension:x-speakeasy-name-override", "createShadowMCPAccessRule")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateShadowMCPAccessRule", "type": "mutation"}`)
	})

	Method("updateShadowMCPAccessRule", func() {
		Description("Update a managed Shadow MCP access rule.")
		Security(security.Session)

		Payload(func() {
			Extend(UpdateShadowMCPAccessRuleForm)
			security.SessionPayload()
		})

		Result(ShadowMCPAccessRuleModel)

		HTTP(func() {
			PUT("/rpc/access.updateShadowMcpRule")
			security.SessionHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateShadowMCPAccessRule")
		Meta("openapi:extension:x-speakeasy-name-override", "updateShadowMCPAccessRule")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateShadowMCPAccessRule", "type": "mutation"}`)
	})

	Method("deleteShadowMCPAccessRule", func() {
		Description("Delete a managed Shadow MCP access rule.")
		Security(security.Session)

		Payload(func() {
			Attribute("id", String, func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
		})

		HTTP(func() {
			DELETE("/rpc/access.deleteShadowMcpRule")
			Param("id")
			security.SessionHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteShadowMCPAccessRule")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteShadowMCPAccessRule")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteShadowMCPAccessRule", "type": "mutation"}`)
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
			Attribute("ids", ArrayOf(String), "Fetch specific challenges by ID. When set, other filters and pagination are ignored.")
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
			Param("ids")
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

	Method("listChallengeBuckets", func() {
		Description("List authz challenges grouped into time-based burst buckets. Consecutive challenges with the same dimensions within a 10-minute window are collapsed into a single bucket.")
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
				Description("Maximum number of buckets to return.")
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("offset", Int, func() {
				Description("Number of buckets to skip.")
				Default(0)
				Minimum(0)
			})
			security.ByKeyPayload()
			security.SessionPayload()
		})

		Result(ListChallengeBucketsResult)

		HTTP(func() {
			GET("/rpc/access.listChallengeBuckets")
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

		Meta("openapi:operationId", "listChallengeBuckets")
		Meta("openapi:extension:x-speakeasy-name-override", "listChallengeBuckets")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ChallengeBuckets"}`)
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
		Enum("project", "mcp", "org", "environment", "risk_policy", "chat", "*")
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
	Attribute("project_id", String, func() {
		Description("Project filter (MCP scopes only). When set with resource_id='*', grants access to all servers in the project.")
	})
	Attribute("server_url", String, func() {
		Description("Server URL filter (risk policy scopes only). Include the URI scheme, for example https://api.example.com.")
		Format(FormatURI)
	})
})

var RoleGrantModel = Type("RoleGrant", func() {
	Required("scope")

	Attribute("scope", String, func() {
		Description("The scope slug this grant applies to.")
		Enum("org:read", "org:blocked_read", "org:admin", "org:blocked_admin", "project:read", "project:blocked_read", "project:write", "project:blocked_write", "mcp:read", "mcp:blocked_read", "mcp:write", "mcp:blocked_write", "mcp:connect", "mcp:blocked_connect", "environment:read", "environment:blocked_read", "environment:write", "environment:blocked_write", "risk_policy:evaluate", "risk_policy:bypass", "chat:read")
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
		Enum("org:read", "org:blocked_read", "org:admin", "org:blocked_admin", "project:read", "project:blocked_read", "project:write", "project:blocked_write", "mcp:read", "mcp:blocked_read", "mcp:write", "mcp:blocked_write", "mcp:connect", "mcp:blocked_connect", "environment:read", "environment:blocked_read", "environment:write", "environment:blocked_write", "risk_policy:evaluate", "risk_policy:bypass", "chat:read")
	})

	Attribute("sub_scopes", ArrayOf(String), func() {
		Description("The inherited scopes the primary scope grants.")
		Elem(func() {
			Enum("org:read", "org:blocked_read", "org:admin", "org:blocked_admin", "project:read", "project:blocked_read", "project:write", "project:blocked_write", "mcp:read", "mcp:blocked_read", "mcp:write", "mcp:blocked_write", "mcp:connect", "mcp:blocked_connect", "environment:read", "environment:blocked_read", "environment:write", "environment:blocked_write", "risk_policy:evaluate", "risk_policy:bypass", "chat:read")
		})
	})

	Attribute("selectors", ArrayOf(SelectorModel), func() {
		Description("Selector constraints. Null means unrestricted.")
	})
})

var RoleModel = Type("Role", func() {
	Required("id", "principal_urn", "name", "slug", "description", "is_system", "grants", "member_count", "created_at", "updated_at")

	Attribute("id", String, "Unique role identifier.")
	Attribute("principal_urn", String, "Canonical principal URN for this role.")
	Attribute("name", String, "Display name of the role.")
	Attribute("slug", String, "Stable WorkOS role slug.")
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
	Required("slug", "description", "resource_type", "visibility")

	Attribute("slug", String, func() {
		Description("Unique scope identifier.")
		Enum("org:read", "org:blocked_read", "org:admin", "org:blocked_admin", "project:read", "project:blocked_read", "project:write", "project:blocked_write", "mcp:read", "mcp:blocked_read", "mcp:write", "mcp:blocked_write", "mcp:connect", "mcp:blocked_connect", "environment:read", "environment:blocked_read", "environment:write", "environment:blocked_write", "risk_policy:evaluate", "risk_policy:bypass", "chat:read")
	})
	Attribute("description", String, "What this scope protects.")
	Attribute("resource_type", String, func() {
		Description("The type of resource this scope applies to.")
		Enum("org", "project", "mcp", "environment", "risk_policy", "chat")
	})
	Attribute("visibility", String, func() {
		Description("Whether this scope is a first-class permission or an internal storage/evaluation scope.")
		Enum("user_visible", "internal")
	})
	Attribute("exclusion_scope", String, func() {
		Description("The scope used to store exception rules for this scope.")
		Enum("org:blocked_read", "org:blocked_admin", "project:blocked_read", "project:blocked_write", "mcp:blocked_read", "mcp:blocked_write", "mcp:blocked_connect", "environment:blocked_read", "environment:blocked_write", "risk_policy:bypass")
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
	Attribute("add_grants", ArrayOf(RoleGrantModel), "Scope grants to add.")
	Attribute("remove_grants", ArrayOf(RoleGrantModel), "Scope grants to remove.")
	Attribute("member_ids", ArrayOf(String), "Optional member IDs to additionally assign to this role. Existing assignments are preserved.")
})

var MemberModel = Type("AccessMember", func() {
	Required("id", "principal_urn", "name", "email", "role_ids", "joined_at")

	Attribute("id", String, "User ID.")
	Attribute("principal_urn", String, "Canonical principal URN for this member.")
	Attribute("name", String, "Display name.")
	Attribute("email", String, "Email address.")
	Attribute("photo_url", String, "Avatar URL.")
	Attribute("role_ids", ArrayOf(String), "All role IDs assigned to this member.")
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

var UpdateMemberRolesForm = Type("UpdateMemberRolesForm", func() {
	Required("user_id", "role_ids")

	Attribute("user_id", String, "The user ID to update.")
	Attribute("role_ids", ArrayOf(String), "The role IDs to assign. Replaces all existing role assignments.")
})

var ShadowMCPApprovalRequestModel = Type("ShadowMCPApprovalRequest", func() {
	Required("id", "organization_id", "project_id", "resource_type", "status", "blocked_count", "requested_at", "created_at", "updated_at")

	Attribute("id", String, func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String)
	Attribute("project_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("resource_type", String)
	Attribute("requester_user_id", String)
	Attribute("requester_email", String)
	Attribute("requester_display_name", String)
	Attribute("status", String, func() {
		Enum("requested", "approved", "denied")
	})
	Attribute("risk_policy_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("risk_result_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("observed_name", String)
	Attribute("observed_full_url", String)
	Attribute("observed_url_host", String)
	Attribute("observed_server_identity", String)
	Attribute("tool_name", String)
	Attribute("tool_call", String)
	Attribute("block_reason", String)
	Attribute("blocked_count", Int)
	Attribute("first_blocked_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("last_blocked_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("requested_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("decided_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("decided_by", String)
	Attribute("decision_note", String)
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
})

var ListShadowMCPApprovalRequestsResult = Type("ListShadowMCPApprovalRequestsResult", func() {
	Required("requests")
	Attribute("requests", ArrayOf(ShadowMCPApprovalRequestModel))
	Attribute("next_cursor", String, "Cursor for the next page of results.")
})

var ShadowMCPAccessRuleModel = Type("ShadowMCPAccessRule", func() {
	Required("id", "organization_id", "access_scope", "resource_type", "disposition", "match_breadth", "match_value", "display_name", "created_at", "updated_at")

	Attribute("id", String, func() {
		Format(FormatUUID)
	})
	Attribute("organization_id", String)
	Attribute("project_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("access_scope", String, func() {
		Enum("organization", "project")
	})
	Attribute("resource_type", String)
	Attribute("disposition", String, func() {
		Enum("allowed", "denied")
	})
	Attribute("match_breadth", String, func() {
		Enum("full_url", "url_host")
	})
	Attribute("match_value", String)
	Attribute("display_name", String)
	Attribute("observed_full_url", String)
	Attribute("observed_url_host", String)
	Attribute("observed_server_identity", String)
	Attribute("source_request_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("created_by", String)
	Attribute("updated_by", String)
	Attribute("reason", String)
	Attribute("created_at", String, func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Format(FormatDateTime)
	})
})

var ListShadowMCPAccessRulesResult = Type("ListShadowMCPAccessRulesResult", func() {
	Required("rules")
	Attribute("rules", ArrayOf(ShadowMCPAccessRuleModel))
	Attribute("next_cursor", String, "Cursor for the next page of results.")
})

var ShadowMCPInventoryAccessRuleMatchModel = Type("ShadowMCPInventoryAccessRuleMatch", func() {
	Required("id", "access_scope", "disposition", "match_breadth", "match_value", "display_name")

	Attribute("id", String, func() {
		Format(FormatUUID)
	})
	Attribute("project_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("access_scope", String, func() {
		Enum("project")
	})
	Attribute("disposition", String, func() {
		Enum("allowed", "denied")
	})
	Attribute("match_breadth", String, func() {
		Enum("full_url")
	})
	Attribute("match_value", String)
	Attribute("display_name", String)
})

var ShadowMCPInventoryServerModel = Type("ShadowMCPInventoryServer", func() {
	Required("canonical_server_url", "url_host", "first_seen", "last_seen", "observed_use_count", "user_count", "top_users", "access")

	Attribute("canonical_server_url", String)
	Attribute("url_host", String)
	Attribute("server_name", String)
	Attribute("first_seen", String, func() {
		Format(FormatDateTime)
	})
	Attribute("last_seen", String, func() {
		Format(FormatDateTime)
	})
	Attribute("last_called", String, func() {
		Format(FormatDateTime)
	})
	Attribute("observed_use_count", Int)
	Attribute("user_count", Int)
	Attribute("top_users", ArrayOf(String))
	Attribute("access", String, func() {
		Enum("none", "allowed", "denied")
	})
	Attribute("rule", ShadowMCPInventoryAccessRuleMatchModel)
})

var ListShadowMCPInventoryResult = Type("ListShadowMCPInventoryResult", func() {
	Required("servers")
	Attribute("servers", ArrayOf(ShadowMCPInventoryServerModel))
	Attribute("next_cursor", String, "Cursor for the next page of results.")
})

var ShadowMCPInventoryUserModel = Type("ShadowMCPInventoryUser", func() {
	Required("user_key", "last_called", "observed_use_count")

	Attribute("user_key", String)
	Attribute("last_called", String, func() {
		Format(FormatDateTime)
	})
	Attribute("observed_use_count", Int)
})

var ListShadowMCPInventoryUsersResult = Type("ListShadowMCPInventoryUsersResult", func() {
	Required("users")
	Attribute("users", ArrayOf(ShadowMCPInventoryUserModel))
	Attribute("next_cursor", String, "Cursor for the next page of results.")
})

var ShadowMCPInventoryAccessStateResult = Type("ShadowMCPInventoryAccessState", func() {
	Required("canonical_server_url", "url_host", "access")

	Attribute("canonical_server_url", String)
	Attribute("url_host", String)
	Attribute("access", String, func() {
		Enum("none", "allowed", "denied")
	})
	Attribute("rule", ShadowMCPInventoryAccessRuleMatchModel)
})

var ShadowMCPApprovalDecisionResult = Type("ShadowMCPApprovalDecisionResult", func() {
	Required("request", "rules")
	Attribute("request", ShadowMCPApprovalRequestModel)
	Attribute("rule", ShadowMCPAccessRuleModel)
	Attribute("rules", ArrayOf(ShadowMCPAccessRuleModel))
})

var CreateShadowMCPAccessRuleResult = Type("CreateShadowMCPAccessRuleResult", func() {
	Required("rules")
	Attribute("rules", ArrayOf(ShadowMCPAccessRuleModel))
})

var CreateShadowMCPApprovalRequestForm = Type("CreateShadowMCPApprovalRequestForm", func() {
	Required("request_token")

	Attribute("request_token", String, "Signed token from the Shadow MCP block response.")
})

var ApproveShadowMCPApprovalRequestForm = Type("ApproveShadowMCPApprovalRequestForm", func() {
	Required("id", "access_scope", "match_breadth", "match_value", "display_name")

	Attribute("id", String, func() {
		Format(FormatUUID)
	})
	Attribute("access_scope", String, func() {
		Enum("organization", "project")
	})
	Attribute("project_ids", ArrayOf(String), "Project ids to create project-scoped rules for. Empty falls back to the request project.")
	Attribute("match_breadth", String, func() {
		Enum("full_url", "url_host")
	})
	Attribute("match_value", String)
	Attribute("display_name", String)
	Attribute("observed_full_url", String)
	Attribute("observed_url_host", String)
	Attribute("observed_server_identity", String)
	Attribute("reason", String)
})

var DenyShadowMCPApprovalRequestForm = Type("DenyShadowMCPApprovalRequestForm", func() {
	Required("id", "create_deny_rule")

	Attribute("id", String, func() {
		Format(FormatUUID)
	})
	Attribute("create_deny_rule", Boolean)
	Attribute("project_ids", ArrayOf(String), "Project ids to create project-scoped deny rules for. Empty falls back to the request project.")
	Attribute("match_breadth", String, func() {
		Enum("full_url", "url_host")
	})
	Attribute("match_value", String)
	Attribute("display_name", String)
	Attribute("observed_full_url", String)
	Attribute("observed_url_host", String)
	Attribute("observed_server_identity", String)
	Attribute("reason", String)
})

var CreateShadowMCPAccessRuleForm = Type("CreateShadowMCPAccessRuleForm", func() {
	Extend(ShadowMCPAccessRuleForm)
	Attribute("project_ids", ArrayOf(String), "Project ids to create project-scoped rules for. Empty uses project_id for single-rule creation.")
})

var ShadowMCPInventoryServerAccessForm = Type("ShadowMCPInventoryServerAccessForm", func() {
	Required("project_id", "server_url")

	Attribute("project_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("server_url", String)
	Attribute("server_name", String)
	Attribute("reason", String)
})

var ShadowMCPAccessRuleForm = Type("ShadowMCPAccessRuleForm", func() {
	Required("disposition", "access_scope", "match_breadth", "match_value", "display_name")

	Attribute("disposition", String, func() {
		Enum("allowed", "denied")
	})
	Attribute("access_scope", String, func() {
		Enum("organization", "project")
	})
	Attribute("project_id", String, func() {
		Format(FormatUUID)
	})
	Attribute("match_breadth", String, func() {
		Enum("full_url", "url_host")
	})
	Attribute("match_value", String)
	Attribute("display_name", String)
	Attribute("observed_full_url", String)
	Attribute("observed_url_host", String)
	Attribute("observed_server_identity", String)
	Attribute("reason", String)
})

var UpdateShadowMCPAccessRuleForm = Type("UpdateShadowMCPAccessRuleForm", func() {
	Required("id", "disposition", "match_breadth", "match_value", "display_name")

	Attribute("id", String, func() {
		Format(FormatUUID)
	})
	Extend(ShadowMCPAccessRuleForm)
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
		Enum("grant_matched", "no_grants", "scope_unsatisfied", "deny_grant", "invalid_check", "rbac_skipped_apikey", "dev_override")
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

var ChallengeBucketModel = Type("ChallengeBucket", func() {
	Description("A group of consecutive challenges with the same dimensions that occurred within a 10-minute window.")
	Required("id", "last_seen", "first_seen", "organization_id", "principal_urn", "principal_type",
		"operation", "outcome", "reason", "scope", "role_slugs",
		"evaluated_grant_count", "matched_grant_count",
		"challenge_count", "challenge_ids")

	Attribute("id", String, "ID of the most recent challenge in the bucket.")
	Attribute("last_seen", String, func() {
		Description("Timestamp of the most recent challenge in the bucket.")
		Format(FormatDateTime)
	})
	Attribute("first_seen", String, func() {
		Description("Timestamp of the earliest challenge in the bucket.")
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
		Enum("grant_matched", "no_grants", "scope_unsatisfied", "deny_grant", "invalid_check", "rbac_skipped_apikey", "dev_override")
	})
	Attribute("scope", String, "Scope that was checked.")
	Attribute("resource_kind", String, "Resource kind of the check.")
	Attribute("resource_id", String, "Resource ID of the check.")
	Attribute("role_slugs", ArrayOf(String), "Roles the principal had loaded.")
	Attribute("evaluated_grant_count", Int, "Total grants evaluated.")
	Attribute("matched_grant_count", Int, "Number of grants that matched.")
	Attribute("challenge_count", Int, "Number of individual challenges in this bucket.")
	Attribute("challenge_ids", ArrayOf(String), "IDs of all challenges in this bucket.")

	// Resolution fields — null when unresolved.
	Attribute("resolved_at", String, func() {
		Description("When the bucket was resolved by an admin.")
		Format(FormatDateTime)
	})
	Attribute("resolution_type", String, func() {
		Description("How the bucket was resolved.")
		Enum("role_assigned", "dismissed")
	})
	Attribute("resolved_by", String, "URN of the admin who resolved.")
	Attribute("resolution_role_slug", String, "Role slug assigned (when resolution_type=role_assigned).")
})

var ListChallengeBucketsResult = Type("ListChallengeBucketsResult", func() {
	Required("buckets", "total")
	Attribute("buckets", ArrayOf(ChallengeBucketModel), "The challenge buckets.")
	Attribute("total", Int, "Total number of matching buckets for pagination.")
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
