---
"server": minor
---

Restore Claude's redacted MCP attribution on cost telemetry via session transcripts. Claude stamps `mcp_server.name='custom'` on api_request OTEL rows for user-configured MCP servers; those rows now park in a `telemetry_logs_staging` ClickHouse table while the Claude hook plugin's Stop/SubagentStop hooks ship the unredacted `(request_id → server/tool)` attribution extracted from the local session transcript. A per-session Temporal workflow joins the two, rewrites the attribution inside the row's attributes JSON, and promotes the row into `telemetry_logs` — so `attribute_metrics_summaries` aggregates true server/tool names. Rows whose attribution never arrives promote verbatim after 30 minutes via a scheduled sweep.
