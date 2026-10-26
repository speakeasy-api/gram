---
"server": patch
---

Read-only ClickHouse queries now run on the read replica connection (`CLICKHOUSE_READ_*`) instead of the primary: telemetry charts and lists, Explore and widget queries, chat finding counts, AI detection lists, challenge lists, Platform MCP telemetry tools, onboarding hook reads, skill insights, tokens-under-management reads, and the admin support coverage, server health and hook client views. Every ClickHouse write, the shadow MCP inventory catalog, the risk findings tools, and ingest stay on the primary. The read replica pool now matches the primary pool size.
