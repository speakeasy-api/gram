---
"cli": patch
"server": patch
"dashboard": patch
---

`speakeasy auth` now signs in with a PKCE one-time code instead of receiving an API key on its local callback. The CLI sends a code challenge with the browser login, the dashboard returns only a short-lived code bound to that challenge, and the CLI redeems the code with a verifier that never leaves the process. A key for another organization can no longer be swapped in through the callback, and a process that reads the callback gets nothing usable. The callback URL also carries a single-use state token, so stray or forged callbacks are rejected without ending the real login. `cliAuth.authorize` accepts `key_scope: producer` for this flow; it requires org:admin, the same as creating a producer key in the dashboard, and the key is recorded in the audit log.
