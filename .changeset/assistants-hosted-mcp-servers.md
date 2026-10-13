---
"dashboard": patch
"server": patch
---

Assistants resolve hosted (toolset-backed) MCP servers attached either way through one path, so a server is never dialed twice. Responses keep listing hosted servers under `toolsets`. A hosted server sent by its server slug in `mcp_servers` is attached, and the onboarding agent's `attach_mcp_server` accepts a `toolset_slug`, with `attach_toolset` kept as an alias.
