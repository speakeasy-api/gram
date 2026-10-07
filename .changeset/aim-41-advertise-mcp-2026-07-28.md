---
"server": minor
---

Hosted MCP servers (`/mcp/{slug}`), MCP gateways, agent gateways (`/agent-mcp/{agentID}`), and platform toolsets (`/platform/mcp/{toolsetSlug}`) now serve MCP protocol revision `2026-07-28` alongside the earlier revisions. A request declaring `2026-07-28` gets that revision's behavior: `server/discover` instead of `initialize`, spec-mandated HTTP statuses for protocol errors, and no session ids. Clients that negotiate an earlier revision at `initialize` see no change. A notification declaring an unsupported protocol revision is now refused with HTTP 400 instead of being acknowledged. Platform toolset errors raised before a request is dispatched, such as a missing token or an unknown toolset, are now JSON-RPC error responses. MCP gateway responses no longer carry an `MCP-Protocol-Version` header, which no revision defines on responses.
