---
name: jaeger
description: Use the local Jaeger instance to inspect OpenTelemetry traces emitted by the Gram server and worker during development. Activate when testing backend endpoints, debugging request flows, or validating instrumentation.
---

# Jaeger — Local Trace Inspection

Gram runs a local OpenTelemetry Collector that receives all OTLP signals from `gram-server` and `gram-worker`, routing traces to Jaeger and metrics to Prometheus. Everything starts automatically with `mise run infra:start`.

## Architecture

```
App → OTLP :$OTLP_GRPC_PORT → OTel Collector → traces → Jaeger
                                               → metrics → Prometheus (scrapes collector)
                                               → spanmetrics connector → Prometheus (RED metrics from traces)
```

- **OTel Collector** receives all OTLP (traces + metrics) on `$OTLP_GRPC_PORT`
- **Jaeger** receives traces from the collector (not directly from the app)
- **Prometheus** scrapes the collector's metrics exporter and stores both app metrics and span-derived RED metrics

## Discovering Ports

Jaeger ports are configured via environment variables in `mise.toml`. **Always resolve ports from env vars** — never hardcode them, as they may differ across worktrees or local overrides.

```bash
# Jaeger UI/API port
echo $JAEGER_WEB_PORT

# OTLP gRPC receiver port
echo $OTLP_GRPC_PORT
```

- **Web UI**: `http://localhost:$JAEGER_WEB_PORT`
- **OTLP gRPC receiver**: `localhost:$OTLP_GRPC_PORT`
- **REST API**: `http://localhost:$JAEGER_WEB_PORT/api/...`

## Jaeger REST API

Use these endpoints to programmatically query traces after running seed data or hitting endpoints. Replace `$JAEGER_WEB_PORT` with the resolved value.

### List services

```
GET http://localhost:$JAEGER_WEB_PORT/api/services
```

Returns all instrumented services (e.g., `gram-server`, `gram-worker`).

### Search traces

```
GET http://localhost:$JAEGER_WEB_PORT/api/traces?service=gram-server&limit=20
GET http://localhost:$JAEGER_WEB_PORT/api/traces?service=gram-server&operation=POST /v1/mcp/{mcpSlug}&limit=10
GET http://localhost:$JAEGER_WEB_PORT/api/traces?service=gram-server&tags={"http.status_code":"500"}&limit=10
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
GET http://localhost:$JAEGER_WEB_PORT/api/traces/{traceID}
```

Returns all spans for a trace, including cross-service spans between `gram-server` and `gram-worker`.

### List operations for a service

```
GET http://localhost:$JAEGER_WEB_PORT/api/services/{service}/operations
```

Returns all known operation names (HTTP routes, gRPC methods, Temporal activities).

## Development Workflow

1. **Start infra** — `mise run infra:start` (Jaeger starts automatically)
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

## Prometheus — Local Metrics

Prometheus is available at `http://localhost:$PROMETHEUS_PORT`. It stores:

- **App metrics** — custom counters, histograms, gauges exported via OTLP from server/worker
- **Span metrics** — RED metrics (rate, errors, duration) derived from traces by the OTel Collector's `spanmetrics` connector

```bash
# Prometheus UI
echo $PROMETHEUS_PORT

# Example PromQL queries
# Request rate by service:    calls_total{service_name="gram-server"}
# Error rate:                 calls_total{status_code="STATUS_CODE_ERROR"}
# P99 latency:                histogram_quantile(0.99, sum(rate(duration_milliseconds_bucket[5m])) by (le, service_name))
```

## Environment Variables

Configured in `mise.toml`:

| Variable                      | Default                 | Purpose                             |
| ----------------------------- | ----------------------- | ----------------------------------- |
| `OTLP_GRPC_HOST`              | `localhost`             | OTLP receiver host (OTel Collector) |
| `OTLP_GRPC_PORT`              | `4317`                  | OTLP receiver port (OTel Collector) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4317` | Full endpoint (auto-constructed)    |
| `JAEGER_WEB_PORT`             | `16686`                 | Jaeger UI/API port (traces)         |
| `PROMETHEUS_PORT`             | `9099`                  | Prometheus UI/API port (metrics)    |
| `GRAM_ENABLE_OTEL_TRACES`     | `1`                     | Enable/disable trace export         |
| `GRAM_ENABLE_OTEL_METRICS`    | `1`                     | Enable/disable metrics export       |
