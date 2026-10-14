---
"dashboard": patch
"server": patch
---

Skills distributed to a plugin are now also served over the plugin's hosted MCP servers using the MCP Skills extension, behind the `skills-over-mcp` flag. Connected agents can list, verify and load them without installing the plugin package, and each load is recorded as an activation. The MCP server page gains a Skills tab for adding and removing served skills, the skill page shows the served digest and which servers serve it, and plugin skill rows show when a skill was last loaded over MCP.
