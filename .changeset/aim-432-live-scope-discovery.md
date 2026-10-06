---
"server": patch
---

Remote-session logins now request the scopes their protected resource asks for — the client's scope, else the resource's last challenge, else an operator pin on the resource, else the resource's live RFC 9728 `scopes_supported` — before falling back to the issuer's override or catalogue. The new order rolls out behind the PostHog organization flag `remote-session-live-resource-scopes`; with the flag off, the issuer's scope override still beats a client's own scope, as before. The issuer `omit_scope_fallback` setting and the resource scope pin have no API yet and are set via SQL.
