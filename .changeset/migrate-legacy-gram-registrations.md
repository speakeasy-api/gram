---
"server": minor
---

Add the `userSessionIssuers.migrateLegacyGramRegistrations` endpoint — a one-off path that lifts the legacy Redis dynamic-client registrations of a gram-type `oauth_proxy_provider` into `user_session_clients` on a `user_session_issuer`, so migrated MCP clients skip re-registration and re-auth. It is the gram counterpart of `remoteSessionClients.cloneClientFromOAuthProxyProvider` (which covers custom providers) and reuses the same registration-migration logic; both are removed when the legacy OAuth proxy is retired.
