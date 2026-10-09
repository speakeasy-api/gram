---
"server": patch
---

Mapping a custom domain root to a hosted MCP server no longer adds a second endpoint to it when the server is served at another address, which left later edits to that server failing. The root picker offers a hosted server only when it is already served on that domain.
