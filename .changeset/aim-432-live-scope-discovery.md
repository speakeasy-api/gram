---
"server": patch
---

Remote-session logins now request the scopes their protected resource asks for — the client's scope, else the resource's last challenge, else an operator pin on the resource, else the resource's live RFC 9728 `scopes_supported` — before falling back to the issuer's override or catalogue. The resource steps roll out behind the PostHog organization flag `remote-session-live-resource-scopes`. Independently of the flag, a client with its own scope now beats its identity provider's scope override. The issuer `omit_scope_fallback` setting and the resource scope pin have no API yet and are set via SQL.
