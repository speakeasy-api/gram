---
"server": minor
---

Remote MCP URL verification now recognizes servers that speak MCP `2026-07-28`. The probe first asks the server to describe itself with `server/discover` and falls back to the `initialize` handshake, so servers that implement either one verify and can be installed from the catalog. Previously, a server that supported only `2026-07-28` was reported as not being an MCP server.
