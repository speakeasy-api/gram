---
"server": patch
"dashboard": patch
---

An MCP connection whose upstream token never expires now reads as "Never expires" on the agent sign-in step. Before, the server sent a zero date for the missing expiry, so the dashboard marked the connection as expired and said it ended almost 2026 years ago.
