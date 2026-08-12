---
"server": minor
---

Cache OAuth client ID metadata documents instead of refetching them on every
authorization request, honoring upstream `Cache-Control` and `Expires` headers
within a 5 minute to 24 hour bound and revalidating with `If-None-Match`. A
fetch or validation failure leaves the cached document untouched and fails the
request rather than serving a stale one.
