package agents

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("agents", func() {
	Description("Manage agent definitions for a project.")

	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})

	shared.DeclareErrorResponses()

	Method("createAgentDefinition", func() {
		Description("Create a new agent definition")

		Payload(func() {
			Extend(CreateAgentDefinitionForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(shared.AgentDefinition)

		HTTP(func() {
			POST("/rpc/agents.create")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:extension:x-speakeasy-name-override", "create")
	})

	Method("getAgentDefinition", func() {
		Description("Get an agent definition by ID")

		Payload(func() {
			Required("id")
			Attribute("id", String, "The ID of the agent definition")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(shared.AgentDefinition)

		HTTP(func() {
			GET("/rpc/agents.get")
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAgentDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentDefinition"}`)
	})

	Method("listAgentDefinitions", func() {
		Description("List all agent definitions for a project")

		Payload(func() {
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		Result(ListAgentDefinitionsResult)

		HTTP(func() {
			GET("/rpc/agents.list")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listAgentDefinitions")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentDefinitions"}`)
	})

	Method("updateAgentDefinition", func() {
		Description("Update an existing agent definition")

		Payload(func() {
			Extend(UpdateAgentDefinitionForm)
			security.SessionPayload()
			security.ByKeyPayload()
		})

		Result(shared.AgentDefinition)

		HTTP(func() {
			POST("/rpc/agents.update")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateAgentDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
	})

	Method("deleteAgentDefinition", func() {
		Description("Delete an agent definition by ID")

		Payload(func() {
			Required("id")
			Attribute("id", String, "The ID of the agent definition")
			security.SessionPayload()
			security.ByKeyPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			Param("id")
			security.SessionHeader()
			security.ByKeyHeader()
			security.ProjectHeader()
			DELETE("/rpc/agents.delete")
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteAgentDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
	})
})

var CreateAgentDefinitionForm = Type("CreateAgentDefinitionForm", func() {
	Attribute("name", shared.Slug, "The name of the agent definition")
	Attribute("description", String, "Description of the agent definition")
	Attribute("title", String, "Human-readable display title for the agent definition")
	Attribute("instructions", String, "Instructions for the agent")
	Attribute("tools", ArrayOf(String), "List of tool URNs available to the agent")
	Attribute("model", String, "The model to use for the agent")
	Attribute("read_only_hint", Boolean, "If true, the agent does not modify its environment")
	Attribute("destructive_hint", Boolean, "If true, the agent may perform destructive updates")
	Attribute("idempotent_hint", Boolean, "If true, repeated calls with same arguments have no additional effect")
	Attribute("open_world_hint", Boolean, "If true, the agent interacts with external entities")
	security.ProjectPayload()
	Required("name", "description", "instructions")
})

var UpdateAgentDefinitionForm = Type("UpdateAgentDefinitionForm", func() {
	Attribute("id", String, "The ID of the agent definition to update")
	Attribute("description", String, "Updated description of the agent definition")
	Attribute("title", String, "Updated human-readable display title")
	Attribute("instructions", String, "Updated instructions for the agent")
	Attribute("tools", ArrayOf(String), "Updated list of tool URNs available to the agent")
	Attribute("model", String, "Updated model to use for the agent")
	Attribute("read_only_hint", Boolean, "If true, the agent does not modify its environment")
	Attribute("destructive_hint", Boolean, "If true, the agent may perform destructive updates")
	Attribute("idempotent_hint", Boolean, "If true, repeated calls with same arguments have no additional effect")
	Attribute("open_world_hint", Boolean, "If true, the agent interacts with external entities")
	security.ProjectPayload()
	Required("id")
})

var ListAgentDefinitionsResult = Type("ListAgentDefinitionsResult", func() {
	Attribute("agent_definitions", ArrayOf(shared.AgentDefinition), "The list of agent definitions")
	Required("agent_definitions")
})
