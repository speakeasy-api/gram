---
"server": patch
---

Sign-ins through a gateway now request the scopes of the member MCP server the connection is for, discovered the same way as a sign-in to that server directly, instead of falling back to the identity provider's whole scope list. As with direct sign-ins, the member's scopes now take precedence over a provider's scope override. The consent page shows the same scopes.
