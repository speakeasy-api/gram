---
"server": patch
"admin": patch
---

The server no longer reads the retired OAuth proxy link on toolsets. A hosted MCP server that still carries the link can now change its visibility, it is no longer reported as requiring OAuth unless it has a sign-in provider or an external OAuth server, and admin server health no longer reports `oauth_proxy` as its legacy authentication.
