package shared

import (
	. "goa.design/goa/v3/dsl"
)

var ToolVariation = Type("ToolVariation", func() {
	Meta("struct:pkg:path", "types")

	Attribute("id", String, "The ID of the tool variation")
	Attribute("group_id", String, "The ID of the tool variation group")
	Attribute("src_tool_name", String, "The name of the source tool")
	Attribute("confirm", String, "The confirmation mode for the tool variation")
	Attribute("confirm_prompt", String, "The confirmation prompt for the tool variation")
	Attribute("name", String, "The name of the tool variation")
	Attribute("summary", String, "The summary of the tool variation")
	Attribute("description", String, "The description of the tool variation")
	Attribute("tags", ArrayOf(String), "The tags of the tool variation")
	Attribute("summarizer", String, "The summarizer of the tool variation")
	Attribute("created_at", String, "The creation date of the tool variation")
	Attribute("updated_at", String, "The last update date of the tool variation")

	Required("id", "group_id", "src_tool_name", "created_at", "updated_at")
})
