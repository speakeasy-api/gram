---
"server": patch
---

Platform MCP `get_mcp` now pages a server's tool list through `tool_cursor`, and returns `exposure_version` only on the page that completes the read, so a tool change can no longer be confirmed against a partly read list.
