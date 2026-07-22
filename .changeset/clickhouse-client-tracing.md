---
"server": patch
---

Trace every ClickHouse client call by default: the connection is wrapped once at creation so all Query/Select/Exec calls — from any repo, current or future — emit client spans labeled with the target table and issuing function (no SQL text), forward their span context to ClickHouse's server-side execution spans, and record a duration histogram (clickhouse.client.query.duration) per table, operation, and outcome for dashboards and monitors.
