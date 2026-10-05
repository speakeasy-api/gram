---
"server": patch
---

Let an agent create an MCP server from tools a project's functions already produce through the Platform MCP. `create_mcp_from_functions` creates the server and the tool list behind it through the same code the dashboard uses, refuses any tool the project's latest completed deployment does not produce, schedules the tool-search index the new server needs, previews the exact slugs before it is confirmed, and reports whether the server joined the project's Default plugin. Creating a toolset now refuses a name or description longer than its column allows as a bad request instead of an unexpected error, stores a blank description as none, and fails instead of auto-enabling the toolset when the organization's enabled-server count cannot be read.
