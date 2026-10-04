---
"server": patch
---

Let an agent create an MCP server from tools a project's functions already produce through the Platform MCP. `create_mcp_from_functions` creates a private server and the tool list behind it through the same code the dashboard uses, refuses any tool the project's latest completed deployment does not produce, schedules the tool-search index the new server needs, and previews the exact slugs before it is confirmed. Creating a toolset with a name or description longer than its column allows now returns a bad-request error instead of an unexpected one.
