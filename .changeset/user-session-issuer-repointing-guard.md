---
"dashboard": patch
---

Saving a remote MCP server's User Identity, or attaching another identity provider to a server's user session issuer, now asks for confirmation when other servers share that user session issuer and would be affected: their upstream authorization server moves or is cleared, or their client is replaced and everyone signs in again. The confirmation lists those servers and any gateways that lose a provider client, including those in other projects on an organization user session issuer, and suggests a dedicated user session issuer. It cannot be confirmed when servers the user cannot view share the issuer, or when the server refuses the change, and it waits for a fresh check rather than trusting a cached one.
