---
"dashboard": minor
---

Server selection is now optional when creating an agent identity. An agent created with no MCP server only sends hook events: it is granted hook ingestion in its project and device-agent plugin sync, and the install step gives the key as the observability plugin's `SPEAKEASY_AI_HOOKS_*` environment instead of an MCP gateway setup link.
