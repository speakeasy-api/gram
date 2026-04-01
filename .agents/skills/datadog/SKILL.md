---
name: datadog
description: Use Datadog MCP tools to investigate logs, metrics, traces, and incidents for the Gram project. Activate when the user asks about errors, performance issues, incidents, latency, or wants to search telemetry data.
---

# Datadog Observability — Gram Project

## Gram Services

Always filter by the relevant service(s) when querying Datadog:

| Service          | Description                                      |
| ---------------- | ------------------------------------------------ |
| `gram`           | Dashboard frontend (RUM)                         |
| `gram-dashboard` | Dashboard backend                                |
| `gram-server`    | Main backend API server                          |
| `gram-worker`    | Temporal worker                                  |
| `fly`            | Fly.io — where Gram Functions logs are collected |

## Available Tools

Use only the following Datadog MCP tools unless the user explicitly asks for others:

### Logs

- `mcp__datadog-mcp__search_datadog_logs` — Search and filter log events
- `mcp__datadog-mcp__analyze_datadog_logs` — Analyze log patterns and aggregate stats

### Metrics

- `mcp__datadog-mcp__get_datadog_metric` — Get a specific metric's time-series data
- `mcp__datadog-mcp__get_datadog_metric_context` — Get context and metadata for a metric
- `mcp__datadog-mcp__search_datadog_metrics` — Search available metrics by name

### Traces & Spans

- `mcp__datadog-mcp__get_datadog_trace` — Get a specific trace by ID
- `mcp__datadog-mcp__search_datadog_spans` — Search spans (useful for latency investigation)

### Incidents & Monitors

- `mcp__datadog-mcp__search_datadog_incidents` — Search active/recent incidents
- `mcp__datadog-mcp__get_datadog_incident` — Get details for a specific incident
- `mcp__datadog-mcp__search_datadog_monitors` — Find monitors and their current state

### RUM & Events

- `mcp__datadog-mcp__search_datadog_rum_events` — Search Real User Monitoring events (frontend errors, sessions)
- `mcp__datadog-mcp__search_datadog_events` — Search Datadog events stream

### Services & Infrastructure

- `mcp__datadog-mcp__search_datadog_services` — Discover services in APM
- `mcp__datadog-mcp__search_datadog_service_dependencies` — View service dependency map

## Guidelines

- **Always scope queries** to one or more Gram services using the service filter when available.
- **Start narrow, expand if needed**: Query a 15–30 minute window first, then widen.
- **For error investigations**: start with `search_datadog_logs`, filter by `status:error`, then follow trace IDs with `get_datadog_trace`.
- **For latency issues**: use `search_datadog_spans` with `service:<name>` and sort by duration.
- **For frontend issues**: prefer `search_datadog_rum_events` for `gram`.
- **For incidents**: check `search_datadog_incidents` first before deep-diving into logs.
