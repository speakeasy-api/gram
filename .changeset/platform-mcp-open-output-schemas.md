---
"server": patch
---

Platform MCP tools advertise output schemas that accept properties they do not name, so an agent that connected before a tool's result gained a field keeps working instead of rejecting every result. Agents can list and inspect MCP servers that sit in plugins again: `find_mcp` and `get_mcp` results that name a plugin no longer fail the client's schema check, and each membership always carries `state` and `publication_state`.
