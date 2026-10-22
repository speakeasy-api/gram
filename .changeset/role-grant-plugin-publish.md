---
"server": patch
---

Changing a role's grants or an MCP server's audience now republishes the affected role plugins within seconds. Previously these changes only queued a durable publication request, so plugins could wait for the hourly republish sweep before devices saw the new servers.
