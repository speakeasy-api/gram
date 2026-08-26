---
"server": patch
---

The hosted MCP install page no longer shows OAuth-only setup steps (such as `codex mcp login`) for publicly served tunneled MCP servers. Public tunnels are served anonymously, so the page now matches the runtime behavior and only renders OAuth instructions when a connecting client will actually be sent through an OAuth flow.
