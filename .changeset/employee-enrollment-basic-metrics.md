---
"server": minor
"dashboard": patch
---

Speed up the Employee Enrollment page (DNO-618). `telemetry.searchUsers` gains a `metrics` level: `full` (default, unchanged) computes the complete set of aggregates, while `basic` projects only user identity, first/last activity, input/output token sums, and the raw user ids the account-enrichment join needs — skipping the per-tool and per-hook-source map aggregations (`sumMapIf`), chat-cardinality (`uniqExactIf`), and cost/cache/avg columns that dominate the per-row ClickHouse work. The enrollment list, which renders only the lean fields (linked accounts come from Postgres), now requests `basic`, so its query no longer builds breakdowns it discards.
