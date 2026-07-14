---
"server": patch
---

Billing tokens-under-management reads over attribute_metrics_summaries now filter tombstoned rows (is_active = 1), matching the costs page reads, so generations soft-deleted by the backfill runbook are excluded from billed totals and breakdowns.
