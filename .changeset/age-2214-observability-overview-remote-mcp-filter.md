---
"server": patch
---

`/rpc/telemetry.getObservabilityOverview` now accepts an optional `remote_mcp_server_id` filter so callers can scope summary, time-series, and per-tool breakdown metrics to a single Remote MCP source. Combinable with the existing `toolset_slug` filter.
