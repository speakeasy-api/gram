package agentdefinitions

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("agent_definitions", func() {
	Description("Manages agent definitions that act as tools in MCP servers.")

	Security(security.Session, security.ProjectSlug)
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	shared.DeclareErrorResponses()

	Method("createAgentDefinition", func() {
		Description("Create a new agent definition.")

		Payload(func() {
			Extend(CreateAgentDefinitionForm)

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(AgentDefinitionResult)

		HTTP(func() {
			POST("/rpc/agents.definitions.create")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "createAgentDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateAgentDefinition"}`)
	})

	Method("getAgentDefinition", func() {
		Description("Get an agent definition by ID.")

		Payload(func() {
			Required("id")
			Attribute("id", String, "The ID of the agent definition")

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(AgentDefinitionResult)

		HTTP(func() {
			GET("/rpc/agents.definitions.get")
			Param("id")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "getAgentDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentDefinition"}`)
	})

	Method("listAgentDefinitions", func() {
		Description("List all agent definitions for a project.")

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ListAgentDefinitionsResult)

		HTTP(func() {
			GET("/rpc/agents.definitions.list")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listAgentDefinitions")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentDefinitions"}`)
	})

	Method("updateAgentDefinition", func() {
		Description("Update an existing agent definition.")

		Payload(func() {
			Extend(UpdateAgentDefinitionForm)

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(AgentDefinitionResult)

		HTTP(func() {
			POST("/rpc/agents.definitions.update")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "updateAgentDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "update")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateAgentDefinition"}`)
	})

	Method("deleteAgentDefinition", func() {
		Description("Delete an agent definition.")

		Payload(func() {
			Required("id")
			Attribute("id", String, "The ID of the agent definition to delete")

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/agents.definitions.delete")
			Param("id")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteAgentDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteAgentDefinition"}`)
	})
})

var CreateAgentDefinitionForm = Type("CreateAgentDefinitionForm", func() {
	Required("name", "model", "description", "instruction")

	Attribute("name", shared.Slug, "The name of the agent, used as the corresponding tool name")
	Attribute("model", String, "The default model to use for this agent", func() {
		MaxLength(100)
	})
	Attribute("title", String, "The display title when presented in UIs", func() {
		MaxLength(200)
	})
	Attribute("description", String, "The tool description for this agent when presented as a tool", func() {
		MaxLength(1000)
	})
	Attribute("instruction", String, "The system prompt for the agent")
	Attribute("tools", ArrayOf(String), "The tool URNs this agent can invoke")
})

var UpdateAgentDefinitionForm = Type("UpdateAgentDefinitionForm", func() {
	Required("id")

	Attribute("id", String, "The ID of the agent definition to update")
	Attribute("model", String, "The default model to use for this agent", func() {
		MaxLength(100)
	})
	Attribute("title", String, "The display title when presented in UIs", func() {
		MaxLength(200)
	})
	Attribute("description", String, "The tool description for this agent when presented as a tool", func() {
		MaxLength(1000)
	})
	Attribute("instruction", String, "The system prompt for the agent")
	Attribute("tools", ArrayOf(String), "The tool URNs this agent can invoke")
})

var AgentDefinitionView = Type("AgentDefinitionView", func() {
	Required("id", "name", "tool_urn", "model", "description", "instruction", "tools", "created_at", "updated_at")

	Attribute("id", String, "The ID of the agent definition")
	Attribute("name", String, "The name of the agent")
	Attribute("tool_urn", String, "The tool URN for this agent")
	Attribute("model", String, "The default model")
	Attribute("title", String, "The display title")
	Attribute("description", String, "The tool description")
	Attribute("instruction", String, "The system prompt")
	Attribute("tools", ArrayOf(String), "The tool URNs this agent can invoke")
	Attribute("created_at", String, "When the agent was created", func() {
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, "When the agent was last updated", func() {
		Format(FormatDateTime)
	})
})

var AgentDefinitionResult = Type("AgentDefinitionResult", func() {
	Required("agent_definition")

	Attribute("agent_definition", AgentDefinitionView, "The agent definition")
})

var ListAgentDefinitionsResult = Type("ListAgentDefinitionsResult", func() {
	Required("agent_definitions")

	Attribute("agent_definitions", ArrayOf(AgentDefinitionView), "The list of agent definitions")
})
