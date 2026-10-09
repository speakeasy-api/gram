---
"dashboard": minor
"server": patch
---

New remote session clients created by Auto-Configure, the install workflow or Platform MCP no longer receive a copy of the discovered scopes; each sign-in resolves them live from the protected resource. Only scopes an operator picks for a manual client are stored. A client's own scopes now beat its provider's scope override everywhere, so the warning that they have no effect under an override is gone. Ships after the `remote-session-live-resource-scopes` flag is enabled broadly: without it, a client with no scope falls back to the issuer's whole catalogue.
