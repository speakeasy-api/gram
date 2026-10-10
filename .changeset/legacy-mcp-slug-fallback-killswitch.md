---
"server": patch
---

Hosted MCP requests served through the legacy toolset slug lookup now carry the toolset's canonical MCP server when it has one, so kill switches (evaluated fail-closed, as on the endpoint path) and server-scoped risk policies on prompts/get and resources/read apply to them. Each fallback request is logged with its entry point, toolset, project, request host, canonical-server presence and custom domain state.
