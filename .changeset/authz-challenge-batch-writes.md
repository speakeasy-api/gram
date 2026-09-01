---
"server": patch
---

Authz challenge logging now writes to ClickHouse in batches rather than one insert per event. The previous per-row insert held a pooled ClickHouse connection for the duration of a server-side flush, capping the subscriber's throughput and exhausting the connection pool shared with the other ClickHouse writers, so challenge, event feed, and risk finding ingestion could all fall behind a growing Pub/Sub backlog under sustained challenge volume.
