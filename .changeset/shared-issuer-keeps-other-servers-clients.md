---
"server": patch
"dashboard": patch
---

Saving an MCP server's User Identity no longer removes the sign-in client other servers on the same issuer use. A client for a new provider is added alongside the others, a provider that already has a client reuses it, and a server's callers only need that server's own clients. Turning off User Identity on a server now only removes that server's own clients.
