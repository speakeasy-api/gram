---
"server": patch
---

Let an agent put a newly pushed tool onto an existing MCP server through the Platform MCP. `list_project_tools` reports the tools a project produces and the function or API document each one came from, `get_mcp` now reports the tool list a hosted server exposes, and `add_tools_to_mcp` / `remove_tools_from_mcp` change that list one tool at a time under confirmation, an expected version and an idempotency key. The change is computed inside the transaction that reads the current list, so a concurrent edit can no longer silently drop tools, and the result names the plugins the change republishes.
