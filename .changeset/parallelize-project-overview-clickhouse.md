---
"server": patch
---

Reduce project overview latency by running independent ClickHouse aggregations concurrently, tracing each query, and computing chat resolutions in a single PostgreSQL pass.
