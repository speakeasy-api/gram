---
"server": patch
---

Refresh remote session issuer metadata reactively when an upstream answer says the stored endpoints drifted: a token endpoint answering 404 or 410 on refresh or code exchange, or an ID token signed under a key the issuer's published key set lacks, requests a refresh outside the daily cadence, at most once per issuer every ten minutes. The `gram.remote_session_issuer.metadata_refresh` metric now carries the trigger reason and a `skipped_recent` outcome.
