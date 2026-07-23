---
"server": patch
---

Simplify the ClickHouse connection wrapper to span-context forwarding only. The client-side spans, the `clickhouse.client.query.duration` histogram, and the operation/table label derivation introduced in the previous release are removed: per-query latency is investigated via ClickHouse's own `query_log`/`opentelemetry_span_log` (joined by trace id, which the wrapper still forwards on every call), and service-level latency comes from the ClickHouse Cloud Datadog integration.
