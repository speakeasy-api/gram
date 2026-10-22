---
"server": patch
---

A hosted MCP server whose custom domain cannot serve (deleted, or never verified and activated) can now move its endpoint to the platform domain through `mcpEndpoints.update`. The endpoint keeps its id, and the move checks the platform slug rules and availability. A server on a live custom domain still cannot leave it.
