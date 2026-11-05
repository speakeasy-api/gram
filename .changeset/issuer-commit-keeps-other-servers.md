---
"server": patch
"dashboard": patch
---

Configuring an MCP server's identity no longer removes the sign-in client another server on the same User Identity issuer uses. With upstream auth enforcement on, the new client is offered on the configured server only, and a new server no longer inherits another server's client. With enforcement off, the save is refused and the server stays disabled, with a message explaining how to give it a dedicated issuer.
