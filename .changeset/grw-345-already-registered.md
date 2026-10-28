---
"server": patch
---

Platform MCP `register_catalog_mcp` and `register_remote_mcp` now return `already_registered: true` when the MCP server was already in the project, so agents say it was already there instead of reporting it as newly added.
