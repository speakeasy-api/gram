---
"server": minor
---

The admin projects list now reports how many MCP servers each project has, so an operator can read the number off the list instead of opening every project. The count covers both server models at once: every `mcp_servers` row in the project, plus every MCP-enabled toolset that no `mcp_servers` row already describes.
