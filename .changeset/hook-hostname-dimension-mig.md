---
"server": patch
---

Add a `hook_hostname` sort-key dimension to `attribute_metrics_summaries` (non-destructive ALTER + atomic MV MODIFY QUERY). The device hostname the Go hooks report rides into the aggregate so the user breakdown can fall back to the device for sessions that carry no email. Historic buckets read empty for the new column; live ingestion populates it from `gram.hook.hostname`.
