package agents

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("agents", func() {
	Description("OpenAI Responses API compatible endpoint for running agent workflows.")
	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("chat")
	})
	shared.DeclareErrorResponses()

	Method("createResponse", func() {
		Description("Create a new agent response. Executes an agent workflow with the provided input and tools.")

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()
			Extend(AgentResponseRequest)
		})

		Result(AgentResponseOutput, "The agent response output")

		HTTP(func() {
			POST("/rpc/agents.response")
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createAgentResponse")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
	})

	Method("getResponse", func() {
		Description("Get the status of an async agent response by its ID.")

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("response_id", String, "The ID of the response to retrieve")
			Required("response_id")
		})

		Result(AgentResponseOutput, "The agent response output")

		HTTP(func() {
			GET("/rpc/agents.response")
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("response_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAgentResponse")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
	})

	Method("deleteResponse", func() {
		Description("Deletes any response associated with a given agent run.")

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("response_id", String, "The ID of the response to retrieve")
			Required("response_id")
		})

		HTTP(func() {
			DELETE("/rpc/agents.response")
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("response_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteAgentResponse")
		Meta("openapi:extension:x-speakeasy-name-override", "delete")
	})

	// Agent definition CRUD methods

	Method("createAgentDefinition", func() {
		Description("Create a new agent definition.")

		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})

		Payload(func() {
			Extend(CreateAgentDefinitionForm)

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(AgentDefinitionResult)

		HTTP(func() {
			POST("/rpc/agents.create")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "createAgentDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "createDefinition")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateAgentDefinition"}`)
	})

	Method("getAgentDefinition", func() {
		Description("Get an agent definition by ID.")

		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})

		Payload(func() {
			Required("id")
			Attribute("id", String, "The ID of the agent definition")

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(AgentDefinitionResult)

		HTTP(func() {
			GET("/rpc/agents.getByID")
			Param("id")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "getAgentDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "getDefinition")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentDefinition"}`)
	})

	Method("listAgentDefinitions", func() {
		Description("List all agent definitions for a project.")

		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})

		Payload(func() {
			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(ListAgentDefinitionsResult)

		HTTP(func() {
			GET("/rpc/agents.list")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "listAgentDefinitions")
		Meta("openapi:extension:x-speakeasy-name-override", "listDefinitions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "AgentDefinitions"}`)
	})

	Method("updateAgentDefinition", func() {
		Description("Update an existing agent definition.")

		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})

		Payload(func() {
			Extend(UpdateAgentDefinitionForm)

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})
		Result(AgentDefinitionResult)

		HTTP(func() {
			POST("/rpc/agents.update")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
		})

		Meta("openapi:operationId", "updateAgentDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "updateDefinition")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UpdateAgentDefinition"}`)
	})

	Method("deleteAgentDefinition", func() {
		Description("Delete an agent definition.")

		Security(security.Session, security.ProjectSlug)
		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})

		Payload(func() {
			Required("id")
			Attribute("id", String, "The ID of the agent definition to delete")

			security.ByKeyPayload()
			security.SessionPayload()
			security.ProjectPayload()
		})

		HTTP(func() {
			DELETE("/rpc/agents.delete")
			Param("id")

			security.ByKeyHeader()
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusNoContent)
		})

		Meta("openapi:operationId", "deleteAgentDefinition")
		Meta("openapi:extension:x-speakeasy-name-override", "deleteDefinition")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "DeleteAgentDefinition"}`)
	})
})

var AgentToolset = Type("AgentToolset", func() {
	Description("A toolset reference for agent execution")

	Attribute("toolset_slug", String, "The slug of the toolset to use")
	Attribute("environment_slug", String, "The slug of the environment for auth")

	Required("toolset_slug", "environment_slug")
})

var AgentSubAgent = Type("AgentSubAgent", func() {
	Description("A sub-agent definition for the agent workflow")

	Attribute("instructions", String, "Instructions for this sub-agent")
	Attribute("name", String, "The name of this sub-agent")
	Attribute("description", String, "Description of what this sub-agent does")
	Attribute("tools", ArrayOf(String), "Tool URNs available to this sub-agent")
	Attribute("toolsets", ArrayOf(AgentToolset), "Toolsets available to this sub-agent")
	Attribute("environment_slug", String, "The environment slug for auth")

	Required("name", "description")
})

var AgentResponseRequest = Type("AgentResponseRequest", func() {
	Description("Request payload for creating an agent response")

	Attribute("model", String, "The model to use for the agent (e.g., openai/gpt-4o)")
	Attribute("instructions", String, "System instructions for the agent")
	Attribute("input", Any, "The input to the agent - can be a string or array of messages")
	Attribute("previous_response_id", String, "ID of a previous response to continue from")
	Attribute("temperature", Float64, "Temperature for model responses")
	Attribute("toolsets", ArrayOf(AgentToolset), "Toolsets available to the agent")
	Attribute("sub_agents", ArrayOf(AgentSubAgent), "Sub-agents available for delegation")
	Attribute("async", Boolean, "If true, returns immediately with a response ID for polling")
	Attribute("store", Boolean, "If true, stores the response defaults to true")

	Required("model", "input")
})

var AgentResponseText = Type("AgentResponseText", func() {
	Description("Text format configuration for the response")

	Attribute("format", AgentTextFormat, "The format of the text response")

	Required("format")
})

var AgentTextFormat = Type("AgentTextFormat", func() {
	Description("Text format type")

	Attribute("type", String, "The type of text format (e.g., 'text')")

	Required("type")
})

var AgentResponseOutput = Type("AgentResponseOutput", func() {
	Description("Response output from an agent workflow")

	Attribute("id", String, "Unique identifier for this response")
	Attribute("object", String, "Object type, always 'response'")
	Attribute("created_at", Int64, "Unix timestamp when the response was created")
	Attribute("status", String, func() {
		Description("Status of the response")
		Enum("in_progress", "completed", "failed")
	})
	Attribute("error", String, "Error message if the response failed")
	Attribute("instructions", String, "The instructions that were used")
	Attribute("model", String, "The model that was used")
	Attribute("output", ArrayOf(Any), "Array of output items (messages, tool calls)")
	Attribute("previous_response_id", String, "ID of the previous response if continuing")
	Attribute("temperature", Float64, "Temperature that was used")
	Attribute("text", AgentResponseText, "Text format configuration")
	Attribute("result", String, "The final text result from the agent")

	Required("id", "object", "created_at", "status", "model", "output", "temperature", "text", "result")
})

// Agent definition types

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
