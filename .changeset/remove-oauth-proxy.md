---
"server": minor
---

Remove the legacy OAuth proxy provider system now that toolsets are migrated to user session issuers: the `/oauth/*` proxy serving path, the `oauth/providers` package, the proxy management endpoints (`toolsets.addOAuthProxyServer` / `updateOAuthProxyServer`), the throwaway migration enablement (`remoteSessionClients.cloneClientFromOAuthProxyProvider`, `userSessionIssuers.migrateLegacyGramRegistrations`), the `AdditionalCacheKeys` cache fan-out mechanism, and the OAuth-proxy audit _emit_ path (historical audit entries still render). `external_oauth_server_metadata` is unaffected. The `oauth_proxy_*` tables and the `toolsets.oauth_proxy_server_id` column are left in place for a later data-drop migration.
