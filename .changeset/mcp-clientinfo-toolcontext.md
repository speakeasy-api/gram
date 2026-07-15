---
"@gram-ai/functions": minor
---

feat: expose the calling MCP client's identity to tools via `ctx.clientInfo`

`ToolContext` gains an optional `clientInfo` field (`{ name, version }`) populated when a tool is invoked over MCP — preferring the per-call `_meta["io.modelcontextprotocol/clientInfo"]` hint and falling back to the identity captured during the MCP `initialize` handshake. It is `undefined` for direct invocations and is untrusted, self-reported metadata intended for observability and convenience, never authorization.
