---
"server": patch
---

Add a production runbook to backfill an org's historical "(unset)" account-type spend as team on attribute_metrics_summaries (POC-305), staged as a dedicated generation 3 via the generation/is_active tombstone machinery. Ships with a ClickHouse migration that atomically swaps attribute_metrics_summaries_mv's query in place (ALTER TABLE ... MODIFY QUERY, no ingestion gap) to stamp generation 2 instead of 0, so live ingestion is immune to the backfill's generation-0/1 cutover flips and late-arriving rows can never be hidden without a staged replacement.
