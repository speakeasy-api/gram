---
"server": minor
---

Access rules that limit an MCP server to tools with certain annotations (for example "read-only") no longer reach tools that have no annotations. Previously such a rule also allowed listing and calling every tool with no recorded annotations. This applies to hosted toolsets, remote MCP servers and tunneled MCP servers. Rules naming a tool, and rules for a whole server or project, are unchanged. To restore access to an affected tool, record its annotations or grant it by name; the Platform MCP `get_mcp_access` tool and the access explanation report which tools a role reaches.
