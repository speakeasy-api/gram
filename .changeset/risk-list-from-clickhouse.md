---
"server": patch
---

Serve the project-wide Risk Events listing from ClickHouse behind the new
`risk-list-from-clickhouse` per-org flag. The ClickHouse path keeps the same
event-time ordering, cursor format, filters and redaction behavior as the
Postgres listing (chat-scoped reads stay on Postgres, which alone holds raw
match content), and enriches chat titles and tool-call block links from
Postgres per page. Also fixes a pagination off-by-one shared with the
Postgres path where the row after a page boundary was skipped by the cursor.
