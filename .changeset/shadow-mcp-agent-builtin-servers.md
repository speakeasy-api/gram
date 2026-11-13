---
"server": patch
"hooks": patch
---

Shadow MCP no longer blocks or reports a coding agent's own built-in MCP servers, including new ones that no list names yet. A server is judged by its URL when one is known from the hook or the session's complete MCP inventory. Until that complete inventory arrives, the partial list a sender reports first and the device's last complete inventory (kept for 14 days, keyed by the per-device id) are used as install evidence only: they can show a server is installed but never that it is absent. A connector named by a UUID, a server started by a user launch command, and a server the inventory lists stay under Shadow MCP; a launch command that runs from a coding agent's own app bundle, and any other server with no URL, command, or install evidence, count as built-ins. Each built-in call records why in `gram.mcp.builtin_evidence`. The Claude guard no longer blocks built-ins while the session inventory is still loading, and the Cursor relay reports Cursor's server identifier so Cursor's own servers are told apart from plugin and user servers.
