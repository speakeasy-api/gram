// Package agents declares the human-only first-class agent management API.
package agents

import (
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
	Attribute("owner_user_id", String, "Eligible same-organization human owner; defaults to the caller")
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
		Enum("project", "mcp", "org", "environment", "skill", "risk_policy", "chat", "agent", "*")
	})
	Attribute("resource_id", String, "The resource identifier, or '*' for all resources of this kind.")
	Attribute("disposition", String, "Tool disposition filter (MCP scopes only).", func() {
		Enum("read_only", "destructive", "idempotent", "open_world")
	})
	Attribute("tool", String, "Specific tool name filter (MCP scopes only).")
	Attribute("project_id", String, "Project filter (MCP scopes only).")
	Attribute("server_url", String, "Server URL filter (risk policy scopes only).", func() { Format(FormatURI) })
	Attribute("server_identity", String, "Server identity filter (risk policy scopes only).")
})

var PolicyGrantForm = Type("AgentPolicyGrantForm", func() {
	Required("scope", "effect", "selector")
	Attribute("scope", String, "Agent-runtime-safe scope to grant", func() { MinLength(1) })
	Attribute("effect", String, "Grant effect; direct agent policy is allow-only", func() { Enum("allow", "deny") })
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

var PolicyGrantIDForm = Type("AgentPolicyGrantIDForm", func() {
	Extend(AgentIDForm)
	Attribute("grant_id", String, "Direct policy grant identifier", func() { Format(FormatUUID) })
	Required("grant_id")
})

var Agent = Type("ManagedAgent", func() {
	Required("id", "owner_user_id", "name", "lifecycle", "permissions", "created_at", "updated_at")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("owner_user_id", String)
	Attribute("name", String)
	Attribute("lifecycle", Lifecycle)
	Attribute("permissions", Permissions)
	Attribute("created_at", String, func() { Format(FormatDateTime) })
	Attribute("updated_at", String, func() { Format(FormatDateTime) })
})

var _ = Service("agents", func() {
	Description("Human-only management of first-class agent principals.")
	Security(security.Session)
	shared.DeclareErrorResponses()

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

	Method("createPolicyGrant", func() {
		Meta("openapi:operationId", "createAgentPolicyGrant")
		Meta("openapi:extension:x-speakeasy-name-override", "createPolicyGrant")
		Payload(func() {
			security.SessionPayload()
			Extend(AgentIDForm)
			Extend(PolicyGrantForm)
		})
		Result(PolicyGrant)
		HTTP(func() {
			POST("/rpc/agents.createPolicyGrant")
			security.SessionHeader()
			Body(func() {
				Attribute("agent_id")
				Attribute("scope")
				Attribute("effect")
				Attribute("selector")
			})
			Response(StatusCreated)
		})
	})

	Method("updatePolicyGrant", func() {
		Meta("openapi:operationId", "updateAgentPolicyGrant")
		Meta("openapi:extension:x-speakeasy-name-override", "updatePolicyGrant")
		Payload(func() {
			security.SessionPayload()
			Extend(PolicyGrantIDForm)
			Extend(PolicyGrantForm)
		})
		Result(PolicyGrant)
		HTTP(func() {
			POST("/rpc/agents.updatePolicyGrant")
			security.SessionHeader()
			Body(func() {
				Attribute("agent_id")
				Attribute("grant_id")
				Attribute("scope")
				Attribute("effect")
				Attribute("selector")
			})
			Response(StatusOK)
		})
	})

	Method("deletePolicyGrant", func() {
		Meta("openapi:operationId", "deleteAgentPolicyGrant")
		Meta("openapi:extension:x-speakeasy-name-override", "deletePolicyGrant")
		Payload(func() {
			security.SessionPayload()
			Extend(PolicyGrantIDForm)
		})
		HTTP(func() {
			POST("/rpc/agents.deletePolicyGrant")
			security.SessionHeader()
			Body(PolicyGrantIDForm)
			Response(StatusNoContent)
		})
	})

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
