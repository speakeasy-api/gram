---
"server": patch
---

A refresh token from an MCP session that reached its length limit now keeps answering "refresh_token has expired" after the 30-second replay window, instead of "unknown or already used", so health views tell the person to sign in again rather than blaming the AI app.
