package shared

import (
	. "goa.design/goa/v3/dsl"
)

var ToolVariation = Type("ToolVariation", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The ID of the tool variation")
	Attribute("group_id", String, "The ID of the tool variation group")
	Attribute("src_tool_urn", String, "The URN of the source tool")
	Attribute("src_tool_name", String, "The name of the source tool")
	Attribute("confirm", String, "The confirmation mode for the tool variation")
	Attribute("confirm_prompt", String, "The confirmation prompt for the tool variation")
	Attribute("name", String, "The name of the tool variation")
	Attribute("description", String, "The description of the tool variation")
	Attribute("summarizer", String, "The summarizer of the tool variation")
	Attribute("title", String, "Display name override for the tool")
	Attribute("read_only_hint", Boolean, "Override: if true, the tool does not modify its environment")
	Attribute("destructive_hint", Boolean, "Override: if true, the tool may perform destructive updates")
	Attribute("idempotent_hint", Boolean, "Override: if true, repeated calls have no additional effect")
	Attribute("open_world_hint", Boolean, "Override: if true, the tool interacts with external entities")
	Attribute("created_at", String, "The creation date of the tool variation")
	Attribute("updated_at", String, "The last update date of the tool variation")

	Required("id", "group_id", "src_tool_name", "src_tool_urn", "created_at", "updated_at")
})
