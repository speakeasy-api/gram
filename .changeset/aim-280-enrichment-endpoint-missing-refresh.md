---
"server": patch
---

Refresh remote session issuer metadata reactively when the stored userinfo or introspection endpoint answers 404 or 410 without an OAuth error body, under the new `enrichment_endpoint_missing` reason on `gram.remote_session_issuer.metadata_refresh`. A 404 or 410 carrying an OAuth error body (`invalid_token`, `invalid_client`, ...) is the endpoint answering, not drift, and requests nothing. The refresh follows the existing reactive cadence: at most once per issuer every ten minutes.
