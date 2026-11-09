---
"server": minor
---

Staff Admin MCP now exposes every write operation the server implements while `GRAM_ADMIN_MCP_WRITES_ENABLED` is on. The `GRAM_ADMIN_MCP_WRITE_OPERATIONS` setting is gone, so each new write no longer needs a deployment change to go live.
