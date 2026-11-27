---
"server": patch
---

The client identity recorded for each MCP session now expires after seven days without use. Each time the session's identity is written or read the expiry is extended, so active sessions keep their attribution, and records for MCP servers that are no longer used no longer stay in Redis indefinitely.
