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

var OwnerAssignmentForm = Type("AgentOwnerAssignmentForm", func() {
	Attribute("agent_id", String, "First-class agent identifier", func() { Format(FormatUUID) })
	Attribute("owner_user_id", String, "Eligible same-organization human replacement owner")
	Required("agent_id", "owner_user_id")
})

var Agent = Type("ManagedAgent", func() {
	Required("id", "owner_user_id", "name", "lifecycle", "permissions", "created_at", "updated_at")
	Attribute("id", String, func() { Format(FormatUUID) })
	Attribute("owner_user_id", String)
	Attribute("owner_reassignment_required_at", String, "When owner loss durably blocked this agent", func() { Format(FormatDateTime) })
	Attribute("owner_reassignment_reason", String, "Stable reason that explicit reassignment is required")
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

	for _, operation := range []string{"transfer", "reassign"} {
		Method(operation, func() {
			Meta("openapi:operationId", operation+"Agent")
			Meta("openapi:extension:x-speakeasy-name-override", operation)
			Payload(func() {
				security.SessionPayload()
				Extend(OwnerAssignmentForm)
			})
			Result(Agent)
			HTTP(func() {
				POST("/rpc/agents." + operation)
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
