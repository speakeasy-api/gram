---
"server": minor
"dashboard": minor
---

Add a `cliAuth` service for device-agent enrollment (DNO-388). `cliAuth.authorize` (session-authenticated, member `org:read` scope) stores a PKCE-bound one-time code, and `cliAuth.redeem` (no session — the PKCE code + verifier is the credential) atomically exchanges it for a per-user `[agent, hooks]` API key, returned once. The dashboard CLI callback uses this flow when the request carries `client=device-agent`, so the raw key never travels in a URL; the existing CLI producer-key login is unchanged.
