package shared

import (
	. "goa.design/goa/v3/dsl"
)

var PromptTemplate = Type("PromptTemplate", func() {
	Required("id", "history_id", "name", "prompt", "engine", "kind", "tools_hint", "tool_urn", "created_at", "updated_at")

	Description("A prompt template")

	Extend(BaseToolAttributes)

	Attribute("history_id", String, "The revision tree ID for the prompt template")
	Attribute("predecessor_id", String, "The previous version of the prompt template to use as predecessor")
	Attribute("prompt", String, "The template content")
	Attribute("engine", String, func() {
		Description("The template engine")
		Enum("mustache")
	})
	Attribute("kind", String, func() {
		Description("The kind of prompt the template is used for")
		Enum("prompt", "higher_order_tool")
	})
	Attribute("tools_hint", ArrayOf(String), func() {
		Description("The suggested tool names associated with the prompt template")
		MaxLength(20)
	})
	Attribute("tool_urns_hint", ArrayOf(String), func() {
		Description("The suggested tool URNS associated with the prompt template")
		MaxLength(20)
	})

	Meta("struct:pkg:path", "types")
})

var PromptTemplateEntry = Type("PromptTemplateEntry", func() {
	Required("id", "name")

	Attribute("id", String, "The ID of the prompt template")
	Attribute("name", Slug, "The name of the prompt template")
	Attribute("kind", String, "The kind of the prompt template")

	Meta("struct:pkg:path", "types")
})
