---
"server": minor
"dashboard": minor
---

`remoteSessionIssuers.get` can now look an identity provider up by its upstream issuer URL, returning the one the project would use (preferring project over organization over platform) or 404 when nothing describes that URL yet. The dashboard's automatic setup flows use it to decide whether to reuse an existing provider instead of scanning the provider list in the browser, which also lets them reuse platform-catalog providers for the first time.
