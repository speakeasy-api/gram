---
"server": patch
---

Platform MCP `register_remote_mcp` now returns `next_action: connect_sign_in` and `automatic_client_registration` when the server's OAuth provider issues a client automatically, so agents offer to connect sign-in in chat instead of only sending the user to the dashboard. The dashboard setup link stays as the fallback.
