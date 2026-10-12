---
"dashboard": patch
"server": patch
---

Assistant reads list hosted (toolset-backed) MCP servers under `mcp_servers` with a read-only `toolset_slug`, and the runtime resolves hosted servers attached either way through one path, so a server is never dialed twice. The `toolsets` field is deprecated but keeps working. The onboarding agent's `attach_mcp_server` accepts a `toolset_slug`, and `attach_toolset` remains as an alias.
