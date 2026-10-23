---
"dashboard": patch
---

The Playground and the MCP tool listing now connect to MCP servers that reject the `2026-07-28` protocol version. When a server answers protocol discovery with an unsupported protocol version error, the dashboard retries with the `initialize` handshake on an older version instead of failing to list tools. Servers that accept `2026-07-28` keep using it.
