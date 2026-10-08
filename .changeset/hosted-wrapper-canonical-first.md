---
"server": patch
---

A hosted MCP server's own wrapper now takes precedence over other MCP servers built from the same toolset, such as gateway members. Publishing plugins no longer fails as ambiguous for a hosted server whose toolset also backs a gateway member once its network access has been edited. Legacy `/mcp/<slug>` tool calls are attributed to that server, and admin health counts those calls as its traffic.
