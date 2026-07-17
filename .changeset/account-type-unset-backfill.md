---
"server": patch
---

Add a production runbook to backfill an org's historical "(unset)" account-type spend as team on attribute_metrics_summaries (POC-305), staged as generation 2 via the generation/is_active tombstone machinery. Ships with a ClickHouse migration that atomically swaps attribute_metrics_summaries_mv's query in place (ALTER TABLE ... MODIFY QUERY, no ingestion gap) so live ingestion is also stamped generation 2 — the backfill and fresh rows share one generation, immune to the generation-0/1 cutover flips, converging on a single live generation once the old ones are cleaned up.
