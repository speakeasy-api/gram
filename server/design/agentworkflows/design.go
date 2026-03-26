package agents

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("agentworkflows", func() {
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
			Extend(WorkflowAgentRequest)
		})

		Result(WorkflowAgentResponseOutput, "The agent response output")

		HTTP(func() {
			POST("/rpc/workflows.createResponse")
			security.ByKeyHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createResponse")
	})

	Method("getResponse", func() {
		Description("Get the status of an async agent response by its ID.")

		Payload(func() {
			security.ByKeyPayload()
			security.ProjectPayload()
			Attribute("response_id", String, "The ID of the response to retrieve")
			Required("response_id")
		})

		Result(WorkflowAgentResponseOutput, "The agent response output")

		HTTP(func() {
			GET("/rpc/workflows.getResponse")
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("response_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getResponse")
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
			DELETE("/rpc/workflows.deleteResponse")
			security.ByKeyHeader()
			security.ProjectHeader()
			Param("response_id")
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteResponse")
	})
})

var WorkflowAgentToolset = Type("WorkflowAgentToolset", func() {
	Description("A toolset reference for agent execution")

	Attribute("toolset_slug", String, "The slug of the toolset to use")
	Attribute("environment_slug", String, "The slug of the environment for auth")

	Required("toolset_slug", "environment_slug")
})

var WorkflowSubAgent = Type("WorkflowSubAgent", func() {
	Description("A sub-agent definition for the agent workflow")

	Attribute("instructions", String, "Instructions for this sub-agent")
	Attribute("name", String, "The name of this sub-agent")
	Attribute("description", String, "Description of what this sub-agent does")
	Attribute("tools", ArrayOf(String), "Tool URNs available to this sub-agent")
	Attribute("toolsets", ArrayOf(WorkflowAgentToolset), "Toolsets available to this sub-agent")
	Attribute("environment_slug", String, "The environment slug for auth")

	Required("name", "description")
})

var WorkflowAgentRequest = Type("WorkflowAgentRequest", func() {
	Description("Request payload for creating an agent response")

	Attribute("model", String, "The model to use for the agent (e.g., openai/gpt-4o)")
	Attribute("instructions", String, "System instructions for the agent")
	Attribute("input", Any, "The input to the agent - can be a string or array of messages")
	Attribute("previous_response_id", String, "ID of a previous response to continue from")
	Attribute("temperature", Float64, "Temperature for model responses")
	Attribute("toolsets", ArrayOf(WorkflowAgentToolset), "Toolsets available to the agent")
	Attribute("sub_agents", ArrayOf(WorkflowSubAgent), "Sub-agents available for delegation")
	Attribute("async", Boolean, "If true, returns immediately with a response ID for polling")
	Attribute("store", Boolean, "If true, stores the response defaults to true")

	Required("model", "input")
})

var WorkflowAgentResponseText = Type("WorkflowAgentResponseText", func() {
	Description("Text format configuration for the response")

	Attribute("format", WorkflowAgentTextFormat, "The format of the text response")

	Required("format")
})

var WorkflowAgentTextFormat = Type("WorkflowAgentTextFormat", func() {
	Description("Text format type")

	Attribute("type", String, "The type of text format (e.g., 'text')")

	Required("type")
})

var WorkflowAgentResponseOutput = Type("WorkflowAgentResponseOutput", func() {
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
	Attribute("text", WorkflowAgentResponseText, "Text format configuration")
	Attribute("result", String, "The final text result from the agent")

	Required("id", "object", "created_at", "status", "model", "output", "temperature", "text", "result")
})
