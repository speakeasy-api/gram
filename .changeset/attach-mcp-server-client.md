---
"server": minor
"dashboard": patch
---

Administrators can attach an upstream OAuth client to one MCP server with `remoteSessions.attachServerClientAssociation`, including a server on a shared user session issuer, and detach now has an undo. The client must already be bound to the server's own issuer. Attach also binds it to the issuers of gateways fronting the server, so their consent can offer it, and detach removes that binding when the client stays on the server's own issuer and nothing else on the gateway's issuer signs in through it. The audit log names the new `remote-session-client:attach-mcp-server` action.
