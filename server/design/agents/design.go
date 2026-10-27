// Package agents declares the human-only first-class agent management API.
package agents

import (
	"fmt"

	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var Lifecycle = Type("AgentLifecycle", String, func() {
	Enum("active", "suspended", "revoked")
})

var Permissions = Type("AgentPermissions", func() {
	Required("read", "write", "authorize", "transfer")
	Attribute("read", Boolean, "Whether the current human may read this agent")
	Attribute("write", Boolean, "Whether the current human may configure or change this agent")
	Attribute("authorize", Boolean, "Whether the current human may manage credentials for this agent")
	Attribute("transfer", Boolean, "Whether the current human may transfer or reassign this agent")
})

var CreateForm = Type("CreateAgentForm", func() {
	Attribute("name", String, func() { MinLength(1); MaxLength(120) })
	// Deliberately unconstrained at the transport: a cleared form field arrives
	// as "" and means organization-wide, which Format(FormatUUID) would reject
	// before the service could read it that way. Non-empty values are parsed
	// and rejected in the service.
	Attribute("project_id", String, "Optional project binding. Omit or send an empty string for an organization-wide agent; independent of policy grants and the Gram-Project header.")
	Attribute("owner_user_id", String, "Eligible same-organization human owner; defaults to the caller")
	Attribute("policy_grants", ArrayOf(PolicyGrantForm), "Optional initial allow-only agent policy ceilings, created atomically with the agent. Effective credential permissions remain limited by the live owner and authorizer.")
	Required("name")
})

var RenameForm = Type("RenameAgentForm", func() {
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("name", String, func() { MinLength(1); MaxLength(120) })
	Required("id", "name")
})

var AgentIDForm = Type("AgentIDForm", func() {
	Attribute("agent_id", String, "First-class agent identifier", func() { Format(FormatUUID) })
	Required("agent_id")
})

var PolicySelector = Type("AgentPolicySelector", func() {
	Description("A constraint that narrows which resources an agent grant applies to.")
	Required("resource_kind", "resource_id")
	Attribute("resource_kind", String, "The kind of resource this selector targets.", func() {
		Enum("project", "mcp", "org", "environment", "skill", "assistant", "risk_policy", "chat", "inference", "agent", "mcp_auth_client", "*")
	})
	Attribute("resource_id", String, "The resource identifier, or '*' for all resources of this kind.")
	Attribute("client_type", String, "Auth client type filter (mcp:auth and mcp:blocked_auth scopes only).", func() {
		Enum("remote_session", "service_account", "static_credential", "pass_through", "ema", "*")
	})
	Attribute("connection_owner", String, "Personal connection owner filter (mcp:auth and mcp:blocked_auth scopes on remote_session clients only): a user ID, '*', or '$represented_user'.")
	Attribute("upstream_client", String, "Upstream OAuth client filter (mcp:auth and mcp:blocked_auth scopes on remote_session clients only). Together with connection_owner it names one person's connection across every auth client sharing that upstream client.")
	Attribute("disposition", String, "Tool disposition filter (MCP scopes only).", func() {
		Enum("read_only", "destructive", "idempotent", "open_world")
	})
	Attribute("tool", String, "Specific tool name filter (MCP scopes only).")
	Attribute("project_id", String, "Project filter (MCP, environment, assistant, inference, mcp:auth and mcp:blocked_auth scopes).")
	Attribute("server_url", String, "Server URL filter (risk policy scopes only).", func() { Format(FormatURI) })
	Attribute("server_identity", String, "Server identity filter (risk policy scopes only).")
})

var PolicyGrantForm = Type("AgentPolicyGrantForm", func() {
	Required("scope", "effect", "selector")
	Attribute("scope", String, "Agent-runtime-safe scope to grant", func() { MinLength(1) })
	Attribute("effect", String, "Grant effect; direct agent policy is allow-only", func() { Enum("allow") })
	Attribute("selector", PolicySelector)
})

var PolicyGrant = Type("AgentPolicyGrant", func() {
	Required("id", "scope", "effect", "selector", "created_at", "updated_at")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("scope", String)
	Attribute("effect", String, func() { Enum("allow") })
	Attribute("selector", PolicySelector)
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
})

var CheckPolicyGrantsForm = Type("CheckAgentPolicyGrantsForm", func() {
	Attribute("agent_id", String, "The agent the grants would be written to. Omit for an agent the caller would create.", func() { Format(FormatUUID) })
	Attribute("owner_user_id", String, "Owner of the agent the caller would create, as agents.create takes it; defaults to the caller. Not allowed with agent_id, whose owner is its own.")
	Attribute("policy_grants", ArrayOf(PolicyGrantForm), "The grants to check, answered in the same order.", func() {
		MinLength(1)
		MaxLength(100)
	})
	Required("policy_grants")
})

var PolicyGrantCheck = Type("AgentPolicyGrantCheck", func() {
	Description("Whether one policy grant could be written, by the same rule agents.create and agents.createPolicyGrant enforce.")
	Required("allowed")
	Attribute("allowed", Boolean, "Whether the grant fits within the caller's and the owner's permissions.")
	Attribute("limited_by", String, "Whose permissions the grant exceeds; absent when allowed.", func() {
		Enum("caller", "owner")
	})
})

var CheckPolicyGrantsResult = Type("CheckAgentPolicyGrantsResult", func() {
	Required("results")
	Attribute("results", ArrayOf(PolicyGrantCheck), "One answer per requested grant, in request order.")
})

var PolicyGrantIDForm = Type("AgentPolicyGrantIDForm", func() {
	Extend(AgentIDForm)
	Attribute("grant_id", String, "Direct policy grant identifier", func() { Format(FormatUUID) })
	Required("grant_id")
})

var ExpectedPolicy = Type("AgentExpectedPolicy", func() {
	Attribute("scope", String, "Scope of the grant originally read", func() { MinLength(1) })
	Attribute("selector", PolicySelector, "Selector of the grant originally read")
	Required("scope", "selector")
})

var DeletePolicyGrantForm = Type("DeleteAgentPolicyGrantForm", func() {
	Extend(PolicyGrantIDForm)
	Attribute("expected_policy", ExpectedPolicy, "Optional delete precondition. Returns conflict if the current scope or selector differs; omission preserves unconditional deletion.")
})

var OwnerAssignmentForm = Type("AgentOwnerAssignmentForm", func() {
	Attribute("agent_id", String, "First-class agent identifier", func() { Format(FormatUUID) })
	Attribute("owner_user_id", String, "Eligible same-organization human replacement owner")
	Required("agent_id", "owner_user_id")
})

var CreatePolicyGrantForm = Type("CreateAgentPolicyGrantForm", func() {
	Extend(AgentIDForm)
	Extend(PolicyGrantForm)
})

var UpdatePolicyGrantForm = Type("UpdateAgentPolicyGrantForm", func() {
	Extend(PolicyGrantIDForm)
	Extend(PolicyGrantForm)
})

var ConnectionBlock = Type("AgentConnectionBlock", func() {
	Description("An agent the caller blocked from their own connection through one upstream OAuth client.")
	Required("id", "agent_id", "remote_session_client_id", "created_at")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("agent_id", String, "The blocked agent", func() { Format(FormatUUID) })
	Attribute("remote_session_client_id", String, "The upstream OAuth client the connection uses; the block covers every auth client sharing it", func() { Format(FormatUUID) })
	Attribute("created_at", String, func() { Format(FormatDateTime) })
})

var ListConnectionBlocksResult = Type("ListAgentConnectionBlocksResult", func() {
	Required("items")
	Attribute("items", ArrayOf(ConnectionBlock), "Blocks the caller wrote on their own connections")
	Attribute("next_cursor", String, "Cursor for the next page; absent when exhausted.")
})

var BlockAgentOnConnectionForm = Type("BlockAgentOnConnectionForm", func() {
	Attribute("agent_id", String, "The agent to block; any agent in the caller's organization", func() { Format(FormatUUID) })
	Attribute("remote_session_client_id", String, "The upstream OAuth client of the caller's own connection", func() { Format(FormatUUID) })
	Required("agent_id", "remote_session_client_id")
})

var ConnectionBlockIDForm = Type("AgentConnectionBlockIDForm", func() {
	Attribute("block_id", String, "Connection block identifier", func() { Format(FormatUUID) })
	Required("block_id")
})

var OwnerProfile = Type("AgentOwnerProfile", func() {
	Required("display_name")
	Attribute("display_name", String)
	Attribute("photo_url", String)

})

var Agent = Type("ManagedAgent", func() {
	Required("id", "owner_user_id", "name", "lifecycle", "permissions", "created_at", "updated_at")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("owner_user_id", String)
	Attribute("owner_profile", OwnerProfile, "Safe profile of the active same-organization owner; does not require directory access")
	Attribute("owner_reassignment_required_at", String, "When owner loss durably blocked this agent", func() { Format(FormatDateTime) })
	Attribute("owner_reassignment_reason", String, "Stable reason that explicit reassignment is required")
	Attribute("name", String)
	Attribute("project_id", String, "The optional project this agent is scoped to; absent for an organization-wide agent", func() { Format(FormatUUID) })
	Attribute("lifecycle", Lifecycle)
	Attribute("connection_needs_refresh", Boolean, "An assigned owner connection has expired or is no longer available; the owner must reconnect and assign it again")
	Attribute("permissions", Permissions)
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
})

var ListAgentsResult = Type("ListAgentsResult", func() {
	Description("A page of managed agents, ordered by name.")

	Attribute("items", ArrayOf(Agent), "The agents in this page.")
	Attribute("next_cursor", String, "Cursor for the next page; absent when exhausted.")
	Required("items")
})

var _ = Service("agents", func() {
	Description("Human-only management of first-class agent principals.")
	Security(security.Session)
	shared.DeclareErrorResponses()
	sessionMethods()

	Method("list", func() {
		shared.CursorPagination()
		Meta("openapi:operationId", "listAgents")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Agents"}`)
		Payload(func() {
			security.SessionPayload()
			Attribute("cursor", String, "Cursor for the next page of agents.")
			Attribute("limit", Int, "The number of agents to return per page.", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("search", String, "Case-insensitive substring match on the agent name.")
			Attribute("lifecycle", ArrayOf(String), "Keep only agents in these lifecycle states.", func() {
				Elem(func() { Enum("active", "suspended", "revoked") })
			})
			Attribute("owner_user_ids", ArrayOf(String), "Keep only agents owned by these users.")
			Attribute("registered_after", String, "Keep only agents registered at or after this moment.", func() {
				Format(FormatDateTime)
			})
			Attribute("registered_before", String, "Keep only agents registered before this moment.", func() {
				Format(FormatDateTime)
			})
			Attribute("name_order", String, "Name order for the page.", func() {
				Enum("asc", "desc")
				Default("asc")
			})
		})
		Result(ListAgentsResult)
		HTTP(func() {
			GET("/rpc/agents.list")
			security.SessionHeader()
			Param("cursor")
			Param("limit")
			Param("search")
			Param("name_order")
			Param("lifecycle")
			Param("owner_user_ids")
			Param("registered_after")
			Param("registered_before")
			Response(StatusOK)
		})
	})

	Method("create", func() {
		Meta("openapi:operationId", "createAgent")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateAgent"}`)
		Payload(func() {
			security.SessionPayload()
			Extend(CreateForm)
		})
		Result(Agent)
		HTTP(func() {
			POST("/rpc/agents.create")
			security.SessionHeader()
			Body(CreateForm)
			Response(StatusCreated)
		})
	})

	Method("get", func() {
		Meta("openapi:operationId", "getAgent")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "Agent"}`)
		Payload(func() {
			security.SessionPayload()
			Attribute("id", String, func() { Format(FormatUUID) })
			Required("id")
		})
		Result(Agent)
		HTTP(func() {
			GET("/rpc/agents.get")
			Param("id")
			security.SessionHeader()
			Response(StatusOK)
		})
	})

	Method("rename", func() {
		Meta("openapi:operationId", "renameAgent")
		Meta("openapi:extension:x-speakeasy-name-override", "rename")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RenameAgent"}`)
		Payload(func() {
			security.SessionPayload()
			Extend(RenameForm)
		})
		Result(Agent)
		HTTP(func() {
			POST("/rpc/agents.rename")
			security.SessionHeader()
			Body(RenameForm)
			Response(StatusOK)
		})
	})

	Method("listDelegableGrants", func() {
		Description("List safe allow-only credential grant candidates shared by the live agent, owner, and current authorizer. Candidates with unrepresentable exclusions are conservatively omitted. Issuance revalidates every grant.")
		Meta("openapi:operationId", "listAgentDelegableGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "listDelegableGrants")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListAgentDelegableGrants"}`)
		Payload(func() {
			security.SessionPayload()
			Extend(AgentIDForm)
			Attribute("toolset_id", String, "Optional MCP authorization resource identifier: the toolset ID when present, otherwise the MCP server ID. Narrows discovery to this server and its project before evaluating exclusions.", func() { Format(FormatUUID) })
			Attribute("toolset_ids", ArrayOf(String, func() { Format(FormatUUID) }), "Optional MCP authorization resource identifiers, discovered in one request. Each narrows discovery like toolset_id; the result is the union of candidates across all of them.", func() { MaxLength(100) })
		})
		Result(ArrayOf(PolicyGrantForm))
		HTTP(func() {
			GET("/rpc/agents.listDelegableGrants")
			Param("agent_id")
			Param("toolset_id")
			Param("toolset_ids")
			security.SessionHeader()
			Response(StatusOK)
		})
	})

	Method("listPolicyGrants", func() {
		Meta("openapi:operationId", "listAgentPolicyGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "listPolicyGrants")
		Payload(func() {
			security.SessionPayload()
			Extend(AgentIDForm)
		})
		Result(ArrayOf(PolicyGrant))
		HTTP(func() {
			GET("/rpc/agents.listPolicyGrants")
			Param("agent_id")
			security.SessionHeader()
			Response(StatusOK)
		})
	})

	Method("checkPolicyGrants", func() {
		Description("Check whether policy grants could be written without writing them: to agent_id, or to a new agent the caller would create for owner_user_id (the caller by default). Uses the authorization and containment rule of agents.create and agents.createPolicyGrant, which still check every write.")
		Meta("openapi:operationId", "checkAgentPolicyGrants")
		Meta("openapi:extension:x-speakeasy-name-override", "checkPolicyGrants")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CheckAgentPolicyGrants"}`)
		Payload(func() {
			security.SessionPayload()
			Extend(CheckPolicyGrantsForm)
		})
		Result(CheckPolicyGrantsResult)
		HTTP(func() {
			POST("/rpc/agents.checkPolicyGrants")
			security.SessionHeader()
			Body(CheckPolicyGrantsForm)
			Response(StatusOK)
		})
	})

	Method("createPolicyGrant", func() {
		Meta("openapi:operationId", "createAgentPolicyGrant")
		Meta("openapi:extension:x-speakeasy-name-override", "createPolicyGrant")
		Payload(func() {
			security.SessionPayload()
			Extend(CreatePolicyGrantForm)
		})
		Result(PolicyGrant)
		HTTP(func() {
			POST("/rpc/agents.createPolicyGrant")
			security.SessionHeader()
			Body(CreatePolicyGrantForm)
			Response(StatusCreated)
		})
	})

	Method("updatePolicyGrant", func() {
		Meta("openapi:operationId", "updateAgentPolicyGrant")
		Meta("openapi:extension:x-speakeasy-name-override", "updatePolicyGrant")
		Payload(func() {
			security.SessionPayload()
			Extend(UpdatePolicyGrantForm)
		})
		Result(PolicyGrant)
		HTTP(func() {
			POST("/rpc/agents.updatePolicyGrant")
			security.SessionHeader()
			Body(UpdatePolicyGrantForm)
			Response(StatusOK)
		})
	})

	Method("deletePolicyGrant", func() {
		Meta("openapi:operationId", "deleteAgentPolicyGrant")
		Meta("openapi:extension:x-speakeasy-name-override", "deletePolicyGrant")
		Payload(func() {
			security.SessionPayload()
			Extend(DeletePolicyGrantForm)
		})
		HTTP(func() {
			POST("/rpc/agents.deletePolicyGrant")
			security.SessionHeader()
			Body(DeletePolicyGrantForm)
			Response(StatusNoContent)
		})
	})
	Method("listConnectionBlocks", func() {
		shared.CursorPagination()
		Description("List the agents the caller blocked from their own connections. Needs no agent management access.")
		Meta("openapi:operationId", "listAgentConnectionBlocks")
		Meta("openapi:extension:x-speakeasy-name-override", "listConnectionBlocks")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentConnectionBlocks"}`)
		Payload(func() {
			security.SessionPayload()
			Attribute("cursor", String, "Cursor for the next page of blocks.")
			Attribute("limit", Int, "The number of blocks to return per page.", func() {
				Default(50)
				Minimum(1)
				Maximum(100)
			})
		})
		Result(ListConnectionBlocksResult)
		HTTP(func() {
			GET("/rpc/agents.listConnectionBlocks")
			security.SessionHeader()
			Param("cursor")
			Param("limit")
			Response(StatusOK)
		})
	})

	Method("blockAgentOnConnection", func() {
		Description("Block one agent in the caller's organization from the caller's own connection through an upstream OAuth client, across every auth client sharing it. Only the connection owner can do this, and it needs no agent management access. Blocks only; it never grants access.")
		Meta("openapi:operationId", "blockAgentOnConnection")
		Meta("openapi:extension:x-speakeasy-name-override", "blockOnConnection")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "BlockAgentOnConnection"}`)
		Payload(func() {
			security.SessionPayload()
			Extend(BlockAgentOnConnectionForm)
		})
		Result(ConnectionBlock)
		HTTP(func() {
			POST("/rpc/agents.blockOnConnection")
			security.SessionHeader()
			Body(BlockAgentOnConnectionForm)
			Response(StatusOK)
		})
	})

	Method("removeConnectionBlock", func() {
		Description("Remove a block the caller wrote on their own connection. Blocks written by an agent manager are not affected.")
		Meta("openapi:operationId", "removeAgentConnectionBlock")
		Meta("openapi:extension:x-speakeasy-name-override", "removeConnectionBlock")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "RemoveAgentConnectionBlock"}`)
		Payload(func() {
			security.SessionPayload()
			Extend(ConnectionBlockIDForm)
		})
		HTTP(func() {
			POST("/rpc/agents.removeConnectionBlock")
			security.SessionHeader()
			Body(ConnectionBlockIDForm)
			Response(StatusNoContent)
		})
	})
	for _, operation := range []struct {
		name string
		hook string
	}{
		{name: "transfer", hook: "TransferAgent"},
		{name: "reassign", hook: "ReassignAgent"},
	} {
		Method(operation.name, func() {
			Meta("openapi:operationId", operation.name+"Agent")
			Meta("openapi:extension:x-speakeasy-name-override", operation.name)
			Meta("openapi:extension:x-speakeasy-react-hook", fmt.Sprintf(`{"name": %q}`, operation.hook))
			Payload(func() {
				security.SessionPayload()
				Extend(OwnerAssignmentForm)
			})
			Result(Agent)
			HTTP(func() {
				POST("/rpc/agents." + operation.name)
				security.SessionHeader()
				Body(OwnerAssignmentForm)
				Response(StatusOK)
			})
		})
	}

	for _, operation := range []string{"suspend", "resume", "revoke", "delete"} {
		Method(operation, func() {
			Meta("openapi:operationId", operation+"Agent")
			Meta("openapi:extension:x-speakeasy-name-override", operation)
			Payload(func() {
				security.SessionPayload()
				Extend(AgentIDForm)
			})
			if operation != "delete" {
				Result(Agent)
			}
			HTTP(func() {
				POST("/rpc/agents." + operation)
				security.SessionHeader()
				Body(AgentIDForm)
				if operation == "delete" {
					Response(StatusNoContent)
				} else {
					Response(StatusOK)
				}
			})
		})
	}
})
