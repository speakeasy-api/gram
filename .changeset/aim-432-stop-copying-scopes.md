---
"dashboard": minor
"server": patch
---

New remote session clients created by Auto-Configure, the install workflow or Platform MCP no longer store a copy of the discovered scopes; each sign-in requests what the MCP server asks for. Dynamic client registration still names those scopes, so providers that limit a client to its registered scopes accept them. Only scopes an operator picks are stored, and saving a client no longer rewrites scopes that were left unchanged. Resource-first scope resolution is now always on, so the `remote-session-live-resource-scopes` flag is removed, along with the warning that a client's scopes lose to its provider's scope override.
