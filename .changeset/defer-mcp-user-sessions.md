---
"dashboard": patch
---

Require an explicit Connect before establishing a user session when viewing an MCP server. The server detail Tools tab and the playground no longer mint a user-session token on render for issuer-gated servers; a Connect button gates the mint, so merely viewing a page no longer creates an active user session.
