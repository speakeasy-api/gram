---
"server": minor
"dashboard": patch
---

`remoteSessionClients` and the org-admin client views now source the `user_session_issuer` relationship entirely from the join table. The `RemoteSessionClient` result replaces the single `user_session_issuer_id` with a `user_session_issuer_ids` array (breaking), create/clone accept zero or more `user_session_issuer_ids` so a client can be created standalone, and a client's issuer attachments are now managed through the new `attachUserSessionIssuer` / `detachUserSessionIssuer` endpoints instead of `update`. No more reads or writes of the legacy `remote_session_clients.user_session_issuer_id` column.
