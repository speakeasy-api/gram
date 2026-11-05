---
"server": patch
---

Fix connecting, disconnecting, refreshing and verifying a sign-in that an administrator added to an MCP server as a credential. Connecting now names the server as the requested resource, so providers that require a resource no longer refuse the sign-in, and rotate and delete confirmations list the servers that offer the client.
