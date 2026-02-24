package shared

import (
	. "goa.design/goa/v3/dsl"
)

var AgentDefinition = Type("AgentDefinition", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The unique identifier of the agent definition")
	Attribute("project_id", String, "The project ID this agent definition belongs to")
	Attribute("tool_urn", String, "The tool URN of the agent definition")
	Attribute("name", String, "The name of the agent definition")
	Attribute("description", String, "Description of the agent definition")
	Attribute("title", String, "Human-readable display title for the agent definition")
	Attribute("instructions", String, "Instructions for the agent")
	Attribute("tools", ArrayOf(String), "List of tool URNs available to the agent")
	Attribute("model", String, "The model to use for the agent")
	Attribute("annotations", ToolAnnotations, "MCP tool annotations providing hints about agent behavior")
	Attribute("created_at", String, func() {
		Description("When the agent definition was created.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("When the agent definition was last updated.")
		Format(FormatDateTime)
	})
	Required("id", "project_id", "tool_urn", "name", "description", "instructions", "tools", "created_at", "updated_at")
})
