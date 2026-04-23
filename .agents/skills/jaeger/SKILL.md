---
name: jaeger
description: Use the local Jaeger instance to inspect OpenTelemetry traces emitted by the Gram server and worker during development. Activate when testing backend endpoints, debugging request flows, or validating instrumentation.
---

# Jaeger — Local Trace Inspection

Gram runs a local Jaeger All-in-One instance that collects all OpenTelemetry traces from `gram-server` and `gram-worker`.

## Starting Jaeger

```bash
mise run start:jaeger
```

Or it starts automatically as part of `mprocs` (see `mprocs.yaml`).

- **Web UI**: `http://localhost:16686`
- **OTLP gRPC receiver**: `localhost:4317` (same endpoint the server already exports to)

Jaeger is in the Docker Compose `tools` profile — it does not start with `mise run infra:start`. Use `mise run start:jaeger` explicitly.

## Jaeger REST API

All endpoints are on `http://localhost:16686`. Use these to programmatically query traces after running seed data or hitting endpoints.

### List services

```
GET /api/services
```

Returns all instrumented services (e.g., `gram-server`, `gram-worker`).

### Search traces

```
GET /api/traces?service=gram-server&limit=20
GET /api/traces?service=gram-server&operation=POST /v1/mcp/{mcpSlug}&limit=10
GET /api/traces?service=gram-server&tags={"http.status_code":"500"}&limit=10
GET /api/traces?service=gram-server&start=1700000000000000&end=1700003600000000
```

Query parameters:

- `service` (required) — service name
- `operation` — filter by operation/endpoint
- `tags` — JSON object of tag key-value filters
- `start` / `end` — microsecond Unix timestamps
- `limit` — max traces returned (default 20)
- `minDuration` / `maxDuration` — e.g., `1ms`, `500ms`, `2s`
- `lookback` — e.g., `1h`, `2h`, `1d`

### Get a specific trace

```
GET /api/traces/{traceID}
```

Returns all spans for a trace, including cross-service spans between `gram-server` and `gram-worker`.

### List operations for a service

```
GET /api/services/{service}/operations
```

Returns all known operation names (HTTP routes, gRPC methods, Temporal activities).

## Development Workflow

1. **Start Jaeger** — `mise run start:jaeger`
2. **Start server** — `mise start:server --dev-single-process`
3. **Run seed data or hit endpoints** — exercise the code path you're working on
4. **Query Jaeger** — use the API or UI to inspect the resulting traces
5. **Look for**: slow spans, error spans (`error=true` tag), missing instrumentation, N+1 query patterns

## Trace Structure

The Gram server uses OpenTelemetry with these conventions:

- **HTTP spans**: created by `otelhttp` middleware, operation = `HTTP <method> <route>`
- **Database spans**: created by pgx tracing, operation = SQL query
- **Temporal spans**: activity and workflow execution spans
- **Custom spans**: created via `tracer.Start(ctx, "operation-name")` in service code

Key attributes on spans:

- `http.method`, `http.route`, `http.status_code` — HTTP request details
- `db.statement` — SQL query text
- `service.name` — `gram-server` or `gram-worker`
- `error` — `true` if the span recorded an error

## Environment Variables

Configured in `mise.toml`:

| Variable                      | Default                 | Purpose                          |
| ----------------------------- | ----------------------- | -------------------------------- |
| `OTLP_GRPC_HOST`              | `localhost`             | OTLP receiver host               |
| `OTLP_GRPC_PORT`              | `4317`                  | OTLP receiver port               |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4317` | Full endpoint (auto-constructed) |
| `JAEGER_WEB_PORT`             | `16686`                 | Jaeger UI/API port               |
| `GRAM_ENABLE_OTEL_TRACES`     | `1`                     | Enable/disable trace export      |
| `GRAM_ENABLE_OTEL_METRICS`    | `1`                     | Enable/disable metrics export    |
