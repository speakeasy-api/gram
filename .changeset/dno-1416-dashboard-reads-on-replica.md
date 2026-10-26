---
"server": patch
---

Dashboard ClickHouse reads now run on the read replica connection (`CLICKHOUSE_READ_*`) instead of the primary: telemetry charts and lists, Explore and widget queries, chat finding counts, AI detection lists, and challenge lists. Every ClickHouse write, the shadow MCP inventory catalog, and ingest stay on the primary. The read replica pool now matches the primary pool size.
