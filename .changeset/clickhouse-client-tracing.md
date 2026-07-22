---
"server": patch
---

Trace every ClickHouse client call by default: the connection is wrapped once at creation so all Query/Select/Exec calls — from any repo, current or future — emit client spans carrying the query text, forward their span context to ClickHouse's server-side execution spans, and record a per-table duration histogram (clickhouse.client.query.duration) for dashboards and monitors.
