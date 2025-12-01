package agents

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("agents", func() {
	Description("OpenAI Responses API compatible endpoint for running agent workflows.")
	Security(security.ByKey)
	shared.DeclareErrorResponses()

	Method("createResponse", func() {
		Description("Create a new agent response. Executes an agent workflow with the provided input and tools.")

		Payload(func() {
			security.ByKeyPayload()
			Attribute("body", AgentResponseRequest, "The agent response request body")
			Required("body")
		})

		Result(AgentResponseOutput, "The agent response output")

		HTTP(func() {
			POST("/rpc/agents.response")
			security.ByKeyHeader()
			Body("body")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createAgentResponse")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
	})

	Method("getResponse", func() {
		Description("Get the status of an async agent response by its ID.")

		Payload(func() {
			security.ByKeyPayload()
			Attribute("response_id", String, "The ID of the response to retrieve")
			Required("response_id")
		})

		Result(AgentResponseOutput, "The agent response output")

		HTTP(func() {
			GET("/rpc/agents.response")
			security.ByKeyHeader()
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
			Attribute("response_id", String, "The ID of the response to retrieve")
			Required("response_id")
		})

		HTTP(func() {
			DELETE("/rpc/agents.response")
			security.ByKeyHeader()
			Param("response_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAgentResponse")
		Meta("openapi:extension:x-speakeasy-name-override", "get")
	})
})

var AgentToolset = Type("AgentToolset", func() {
	Description("A toolset reference for agent execution")

	Attribute("toolset_slug", String, "The slug of the toolset to use")
	Attribute("environment_slug", String, "The slug of the environment for auth")
	Attribute("headers", MapOf(String, String), "Optional headers to pass to the toolset")

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

	Attribute("project_slug", String, "The slug of the project to run the agent in")
	Attribute("model", String, "The model to use for the agent (e.g., openai/gpt-4o)")
	Attribute("instructions", String, "System instructions for the agent")
	Attribute("input", Any, "The input to the agent - can be a string or array of messages")
	Attribute("previous_response_id", String, "ID of a previous response to continue from")
	Attribute("temperature", Float64, "Temperature for model responses")
	Attribute("toolsets", ArrayOf(AgentToolset), "Toolsets available to the agent")
	Attribute("sub_agents", ArrayOf(AgentSubAgent), "Sub-agents available for delegation")
	Attribute("async", Boolean, "If true, returns immediately with a response ID for polling")
	Attribute("store", Boolean, "If true, stores the response defaults to true")

	Required("project_slug", "model", "input")
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
