---
"server": minor
---

OTel logs and spans ingested through /otel/v1 are now teed into per-signal ClickHouse tables (otel_logs, otel_traces) via new Pub/Sub CH-writer subscriptions, so they can power org-scoped event views.
