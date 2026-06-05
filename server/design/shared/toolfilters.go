package shared

import (
	. "goa.design/goa/v3/dsl"
)

var ToolFilterTool = Type("ToolFilterTool", func() {
	Meta("struct:pkg:path", "types")

	Description("A tool referenced by a tool filter scope, identified by URN and display name.")

	Attribute("tool_urn", String, "The URN of the tool")
	Attribute("name", String, "The display name of the tool, with any variation rename from the resolved group applied (matching the runtime wire)")

	Required("tool_urn", "name")
})

var ToolFilterScope = Type("ToolFilterScope", func() {
	Meta("struct:pkg:path", "types")

	Description("A filter tag (\"scope\") and the tools reachable when filtering by it via the runtime ?tags= parameter.")

	Attribute("tag", String, "The filter tag")
	Attribute("tool_count", Int, "The number of tools under this scope")
	Attribute("tools", ArrayOf(ToolFilterTool), "The tools under this scope")

	Required("tag", "tool_count", "tools")
})

var ListToolFiltersResult = Type("ListToolFiltersResult", func() {
	Meta("struct:pkg:path", "types")

	Description("The tool filtering configuration in effect for an MCP server: the resolved tool variations group, the available filter scopes (tags), and the tools excluded from all filters. Read-only. Filtering is reported as enabled only when an explicit tool variations group is configured on the MCP server or its toolset; the per-group effective-tag derivation matches the runtime ?tags= filter for that group.")

	Attribute("filtering_enabled", Boolean, "Whether tool filtering is enabled, i.e. the resolution chain (mcp_servers then toolsets) yields a non-null tool variations group. When false, scopes and excluded are empty. A project-default (source-level) variations group is not treated as filtering here.")
	Attribute("tool_variations_group_id", String, "The ID of the resolved tool variations group, if filtering is enabled.", func() {
		Format(FormatUUID)
	})
	Attribute("tool_variations_group_name", String, "The name of the resolved tool variations group, if filtering is enabled.")
	Attribute("scopes", ArrayOf(ToolFilterScope), "The available filter scopes (tags), each with its member tools. Union of effective tags across the server's tools.")
	Attribute("excluded", ArrayOf(ToolFilterTool), "Tools whose effective tag set is empty: reachable only without a ?tags= filter.")

	Required("filtering_enabled", "scopes", "excluded")
})
