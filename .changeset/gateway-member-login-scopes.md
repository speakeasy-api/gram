---
"server": patch
---

Sign-ins through a gateway now request the scopes of the member MCP server the connection is for, discovered the same way as a sign-in to that server directly, instead of falling back to the identity provider's override or whole scope list. The consent page shows the same scopes.
