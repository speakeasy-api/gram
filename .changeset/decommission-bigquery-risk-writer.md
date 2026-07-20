---
---

Decommission the BigQuery risk_findings writer (FindingBQWriter): remove the writer, its Pub/Sub subscription, the BigQuery client/flags/metrics, and the internal/bq package. ClickHouse is the sole risk_findings write path. Backend-only; no package release.
