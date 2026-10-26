---
"server": patch
---

A hosted MCP server whose custom domain never went live, or was deleted, can now move its endpoint to the platform domain through `mcpEndpoints.update`. The endpoint keeps its id, and the move applies the platform slug rules and availability check. A server on a live custom domain, or on one that health checks disabled and that can be reverified, still cannot leave it.
