---
"server": patch
---

Dispatch batch-only risk scanners through Pub/Sub analysis handlers so their findings reach ClickHouse with stable identities. Keep inline Postgres analysis while applying category scopes before dispatch.
