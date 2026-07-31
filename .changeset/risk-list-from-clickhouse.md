---
"server": patch
---

Serve the project-wide Risk Events listing from ClickHouse behind the new
`risk-list-from-clickhouse` per-org flag, keeping the same ordering, filters
and pagination behavior as before. Also fixes a pagination bug where the first
result after a page boundary was skipped.
