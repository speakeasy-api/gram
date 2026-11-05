---
"server": minor
---

Add an opt-in setting on remote session clients that sends the connecting MCP client's self-reported name to the provider as `speakeasy_client_name` on `/authorize`. It is off by default and only sent for anonymous sessions.
