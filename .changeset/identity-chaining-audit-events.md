---
"server": minor
---

Identity chaining changes are now audited on the remote session client. A binding becoming ready on a client records `remote-session-client:enable-identity-chaining`, changing the requested scopes while it stays ready on the same client records `remote-session-client:update-identity-chaining-scopes`, and a ready binding leaving the client (unlinked, moved to another client, or no longer ready) records `remote-session-client:disable-identity-chaining` with that client's `client_id`. Each entry carries the binding, the user session issuer, the remote session issuer, the resource, the generation, the binding state, and the requested scopes (plus the previous scopes for a scope change). The entries are written in the same transaction as the binding change, and only a user actor carries an email display name, for these entries and for the client a dynamic registration creates.
