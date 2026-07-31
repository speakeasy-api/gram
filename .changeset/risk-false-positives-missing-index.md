---
"server": patch
---

Add a partial index on `risk_results (project_id, false_positive_at DESC, id DESC) WHERE false_positive_at IS NOT NULL` so the Policy Center False Positives tab (`risk.listDismissedResults`) no longer forces a sequential scan on projects with a large `risk_results` table. Every existing index on that table only serves the `false_positive_at IS NULL` case, leaving the dismissed-results count and list queries with nothing to scan but the whole table.
