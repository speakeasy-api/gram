package shared

import (
	. "goa.design/goa/v3/dsl"
)

var PromptTemplate = Type("PromptTemplate", func() {
	Required("id", "history_id", "name", "prompt", "engine", "kind", "tools_hint", "created_at", "updated_at")

	Attribute("id", String, "The ID of the prompt template")
	Attribute("history_id", String, "The revision tree ID for the prompt template")
	Attribute("predecessor_id", String, "The previous version of the prompt template to use as predecessor")

	Attribute("name", Slug, "The name of the prompt template")
	Attribute("prompt", String, "The template content")
	Attribute("description", String, "The description of the prompt template")
	Attribute("arguments", String, func() {
		Description("The JSON Schema defining the placeholders found in the prompt template")
		Format(FormatJSON)
	})
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
	Attribute("created_at", String, func() {
		Description("The creation date of the prompt template.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the prompt template.")
		Format(FormatDateTime)
	})

	Meta("struct:pkg:path", "types")
})
