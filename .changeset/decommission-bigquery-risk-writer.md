---
---

Decommission the BigQuery risk_findings writer (FindingBQWriter): remove the writer, the BigQuery client/flags/metrics, and the internal/bq package. The FindingBQWriter proto/subscription is retained but marked `deprecated` (cc-gen labels the GCP resource rather than deleting it) and no longer has a consumer. ClickHouse is the sole risk_findings write path. Backend-only; no package release.
