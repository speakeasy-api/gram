---
"server": patch
---

Platform MCP `get_mcp_readiness` now checks an MCP server again when its last stored check has expired, instead of reporting the expired result as stale. If that check is refused or fails, it still returns the last stored check, marked stale.
