---
"server": patch
---

Add a production runbook to backfill an org's historical "(unset)" account-type spend as team on attribute_metrics_summaries (POC-305), applying the #4259 company-credential classification retroactively via the generation/is_active tombstone machinery. Ships with a ClickHouse migration that recreates attribute_metrics_summaries_mv stamping generation 2 (the current generation) instead of 0, so live ingestion is immune to the backfill's generation-0/1 cutover flips and late-arriving rows can never be hidden without a staged replacement.
