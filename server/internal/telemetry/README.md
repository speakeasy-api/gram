# Telemetry Package

This package handles telemetry data storage and retrieval using ClickHouse for high-performance analytics queries. It implements a **wide event** model where rich, semi-structured data is stored as JSON attributes, with **materialized columns** automatically extracted from the JSON for indexing and fast filtering.

## Design Philosophy: Wide Events

We follow the **wide event** (or "wide structured log") pattern. Instead of scattering data across many narrow tables or pre-aggregating into metrics, we store each event as a single, richly-attributed row. New dimensions can be added by simply including new keys in the `attributes` JSON — no schema changes required.

**References:**

- [Observability Engineering (O'Reilly)](https://info.honeycomb.io/observability-engineering-oreilly-book-2022) — Chapter 4, "Wide Events"
- [Charity Majors — Observability is about wide events](https://charity.wtf/2019/02/05/logs-vs-structured-events/)
- [Liz Fong-Jones — The Glorious Future of Wide Events](https://isburmistrov.substack.com/p/all-you-need-is-wide-events-not-metrics)
- [ClickHouse — Schema Design for Observability](https://clickhouse.com/docs/use-cases/observability/schema-design)
- [ClickHouse — Working with Time Series Data](https://clickhouse.com/blog/working-with-time-series-data-and-functions-ClickHouse)

### How it works in Gram

Each telemetry log row in `telemetry_logs` contains:

| Column Group | Purpose | Examples |
|---|---|---|
| **Core OTel fields** | Standard log record identity | `id`, `time_unix_nano`, `severity_text`, `body` |
| **Trace context** | Distributed tracing correlation | `trace_id` (W3C 32-hex), `span_id` (W3C 16-hex) |
| **`attributes` (JSON)** | The wide event payload — WHAT happened | HTTP details, GenAI metrics, tool info, user IDs, etc. |
| **`resource_attributes` (JSON)** | WHO/WHERE produced the event | `service.name`, `service.version` |
| **Materialized columns** | Auto-extracted from JSON at insert time for fast filtering | `project_id`, `deployment_id`, `urn`, `chat_id`, `user_id`, `external_user_id`, `api_key_id` |

The `attributes` JSON column is the heart of the wide event — it holds arbitrarily many key-value pairs following OTel semantic conventions.

**Materialized columns** are the primary mechanism for making frequently-queried JSON paths performant. They are computed from JSON expressions at insert time (e.g., `toString(attributes.user.id)`) and stored on disk as regular columns with bloom filter indices. See the [Materialized Columns](#materialized-columns) section for details.

> **Deprecation note:** The table currently also has legacy denormalized columns (`gram_project_id`, `gram_deployment_id`, etc.) that are populated manually in Go code. These are being replaced by materialized columns and will be removed. See the [ToolInfo deprecation notice](#deprecation-notice-toolinfo).

### Schema design

```
ENGINE = MergeTree
PRIMARY KEY (gram_project_id, time_unix_nano, id)
PARTITION BY toYYYYMMDD(fromUnixTimestamp64Nano(time_unix_nano))
TTL 30 days
```

The primary key starts with `gram_project_id`, which means **all queries are scoped to a single project by design**. This ensures there are no accidental cross-project data leaks — ClickHouse physically groups data by project, and any query without a project filter would require a full table scan.

See `server/clickhouse/schema.sql` for the full DDL.

## OpenTelemetry Semantic Conventions

We align with the [OTel Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/) wherever an applicable convention exists. All attribute keys are defined in `server/internal/attr/conventions.go` using the `go.opentelemetry.io/otel/semconv` package.

### Standard OTel keys (examples, not exhaustive)

The table below shows representative keys from each category. See `conventions.go` for the full set.

| Category | Example Keys | Reference |
|---|---|---|
| **HTTP** | `http.request.method`, `http.response.status_code`, `http.route`, `url.full` | [HTTP semconv](https://opentelemetry.io/docs/specs/semconv/http/) |
| **GenAI** (experimental) | `gen_ai.operation.name`, `gen_ai.request.model`, `gen_ai.response.model`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.conversation.id` | [GenAI semconv](https://opentelemetry.io/docs/specs/semconv/gen-ai/) |
| **GenAI Tools** | `gen_ai.tool.call.id`, `gen_ai.tool.name`, `gen_ai.tool.type` | [GenAI semconv](https://opentelemetry.io/docs/specs/semconv/gen-ai/) |
| **GenAI Evaluation** | `gen_ai.evaluation.name`, `gen_ai.evaluation.score.value`, `gen_ai.evaluation.score.label` | [GenAI semconv](https://opentelemetry.io/docs/specs/semconv/gen-ai/) |
| **Service/Resource** | `service.name`, `service.version` | [Resource semconv](https://opentelemetry.io/docs/specs/semconv/resource/) |
| **User** | `user.id` | [General semconv](https://opentelemetry.io/docs/specs/semconv/general/attributes/) |
| **Error** | `error.message`, `exception.stacktrace` | [Error semconv](https://opentelemetry.io/docs/specs/semconv/exceptions/) |

### Custom Gram keys (`gram.*`)

The `gram.*` prefix is used for attributes that are **Gram system-generated** — they identify internal Gram concepts that have no OTel equivalent. These are not fallbacks; they are explicitly namespaced to make it clear the attribute originates from the Gram platform.

| Key | Description |
|---|---|
| `gram.project.id` | Gram project UUID |
| `gram.deployment.id` | Deployment UUID |
| `gram.function.id` | Serverless function UUID |
| `gram.tool.urn` | Tool URN (e.g., `tools:function:my-source:my-tool`) |
| `gram.tool.name` | Human-readable tool name |
| `gram.external_user.id` | End-user ID from the customer's system |
| `gram.resource.urn` | Internal resource identifier (e.g., `agents:chat:completion`) |
| `gram.log.severity_text` | Severity override from caller |
| `gram.log.body` | Log body/message content |
| `gram.api_key.id` | API key used for the request |

### Adding new attributes

1. Check the [OTel semconv registry](https://opentelemetry.io/docs/specs/semconv/) first — use an existing key if one fits.
2. If no convention exists, use the `gram.` prefix with dot-delimited namespacing (e.g., `gram.chat.id`).
3. Define the key in `server/internal/attr/conventions.go` with a helper function pair (`KeyValue` + `Slog`).
4. Add the attribute at the call site using the typed helper, never raw strings.
5. **To persist the attribute**, pass it in the `Attributes` map when calling `CreateLog`. All attributes flow through this single entry point — `crud.go` handles splitting them into span vs. resource attributes and serializing to JSON.

### Resource vs. Span attributes

Attributes are split at write time in `crud.go` based on the `ResourceAttributeKeys` set:

- **Resource attributes** describe the entity producing telemetry. These rarely change and are defined by `ResourceAttributeKeys` — currently `service.name` and `service.version`.
- **Span (log) attributes** describe the specific event (HTTP status, token counts, user IDs, etc.). Everything not in `ResourceAttributeKeys` goes here.

This follows the [OTel resource model](https://opentelemetry.io/docs/specs/otel/resource/sdk/).

> **Note:** `gram.deployment.id` is currently listed in `ResourceAttributeKeys` but arguably belongs in span attributes since a deployment is closer to the operation context than the producing service. This should be revisited.

## Emitting Telemetry Data

All telemetry is emitted through `Service.CreateLog(LogParams)` in `crud.go`. Every attribute you want stored in ClickHouse must be passed in the `Attributes` map — this is the single entry point for writing wide events.

### Basic pattern: direct attribute map

Build a `map[attr.Key]any`, populate it with typed attribute keys from the `attr` package, and pass it to `CreateLog`:

```go
attrs := map[attr.Key]any{
    attr.ResourceURNKey:           "agents:chat:completion",
    attr.LogBodyKey:               "LLM chat completion: model=gpt-4o",
    attr.GenAIOperationNameKey:    telemetry.GenAIOperationChat,
    attr.GenAIRequestModelKey:     "gpt-4o",
    attr.GenAIUsageInputTokensKey: 150,
    attr.GenAIConversationIDKey:   chatID.String(),
}

// Conditionally add optional attributes
if userID != "" {
    attrs[attr.UserIDKey] = userID
}

svc.CreateLog(telemetry.LogParams{
    Timestamp:  time.Now(),
    ToolInfo:   toolInfo,  // will be removed (see below)
    Attributes: attrs,
})
```

This pattern is used for GenAI chat completions (`chat/impl.go`) and evaluation results (`chat_resolutions/analyze_segment.go`).

### HTTP tool calls: `HTTPLogAttributes` recorder

For HTTP-based tool calls, use the `HTTPLogAttributes` recorder. It's a `map[attr.Key]any` with typed setter methods that accumulate attributes throughout the request lifecycle:

```go
attrRecorder := make(telemetry.HTTPLogAttributes)

// Recorded progressively as data becomes available
attrRecorder.RecordMethod(req.Method)
attrRecorder.RecordRoute(req.URL.Path)
attrRecorder.RecordServerURL(serverURL, repo.ToolTypeHTTP)
attrRecorder.RecordRequestHeaders(headers, isSensitive)
attrRecorder.RecordTraceContext(ctx)  // extracts trace/span IDs from OTel context

// After the response comes back
attrRecorder.RecordStatusCode(resp.StatusCode)
attrRecorder.RecordDuration(duration)
attrRecorder.RecordResponseHeaders(responseHeaders)

// You can also set attributes directly on the map
attrRecorder[attr.GenAIConversationIDKey] = chatID
attrRecorder[attr.UserIDKey] = userID
```

The recorder is passed as the `Attributes` field of `LogParams`. This is the pattern used by the HTTP roundtripper (`roundtripper.go`) and tool call execution (`instances/impl.go`).

### Key rules

- **Always use typed `attr.Key` constants** — never raw strings. Define new keys in `attr/conventions.go`.
- **`CreateLog` is fire-and-forget** — it runs on a background context and won't block or return errors to the caller. ClickHouse has [async inserts](https://clickhouse.com/docs/optimize/asynchronous-inserts) enabled, so the client buffers rows and flushes in batches — no goroutine needed.
- **Everything goes through `Attributes`** — the `ToolInfo` struct also converts to attributes internally via `AsAttributes()`, but new fields should be passed directly in the map.
- **Severity is auto-inferred** — from `attr.LogSeverityKey` if set, otherwise from `http.response.status_code` (5xx → ERROR, 4xx → WARN, else INFO).
- **Provider is auto-inferred** — if `gen_ai.request.model` is set, `crud.go` automatically infers and sets `gen_ai.provider.name` (e.g., `openai`, `anthropic`).

## Deprecation Notice: `ToolInfo`

> **`ToolInfo` is deprecated and will be removed in a future iteration.**

### What it does today

`ToolInfo` (`telemetry.go`) is a struct that callers pass when creating a log. It bundles a fixed set of fields (`ProjectID`, `DeploymentID`, `FunctionID`, `URN`, `Name`, `OrganizationID`) and converts them to attributes via `AsAttributes()`. In `crud.go`, these are also used to populate the legacy denormalized columns.

### Why it exists

`ToolInfo` was introduced so the telemetry system could know the data types of key fields **ahead of time** at write time — before they landed in the untyped `attributes` JSON blob. This was necessary when we relied on extracted columns in ClickHouse that required knowing the shape of the data upfront. `ToolInfo` effectively acted as a schema hint, telling ClickHouse "these specific paths in the JSON will always be strings/UUIDs."

### Why it's being deprecated

With **materialized columns**, ClickHouse extracts and type-casts values from JSON automatically using expressions like:

```sql
ADD COLUMN project_id String MATERIALIZED toString(attributes.gram.project.id)
```

This eliminates the need for the Go layer to pre-declare field types. Callers will simply pass all data as attributes in the `map[attr.Key]any`, and the ClickHouse schema handles extraction and indexing.

### Migration path

1. New code should pass all data as attributes in the `map[attr.Key]any` — avoid adding fields to `ToolInfo`.
2. Existing `ToolInfo` fields will be migrated to materialized columns incrementally.
3. Once the legacy denormalized columns are fully replaced by materialized equivalents, `ToolInfo` and the manual extraction logic in `crud.go` can be removed.

## Materialized Columns vs. Materialized Views

ClickHouse offers two mechanisms for pre-computing data from wide events. Choose based on the access pattern.

### Materialized Columns

**What:** A column whose value is computed from an expression at insert time and stored on disk alongside the row. Querying the materialized column is as fast as querying any regular column.

**When to use:**
- You frequently filter or GROUP BY a specific JSON path (e.g., `WHERE user_id = ?`).
- The extraction expression is simple (e.g., `toString(attributes.user.id)`).
- You want the value co-located with the row for point lookups.

**Example** (from our schema):

```sql
-- Column: auto-extracted from JSON at insert time
project_id String MATERIALIZED toString(attributes.gram.project.id)

-- Index: bloom filter for fast point lookups
CREATE INDEX idx_telemetry_logs_mat_user_id ON telemetry_logs (user_id)
  TYPE bloom_filter(0.01) GRANULARITY 1;
```

**Current materialized columns:** `project_id`, `deployment_id`, `function_id`, `urn`, `chat_id`, `user_id`, `external_user_id`, `api_key_id` — all with bloom filter indices. See `server/clickhouse/schema.sql` for the full list.

### Materialized Views

**What:** A separate table populated by a query that runs on each insert to the source table. Useful for pre-aggregating data.

**When to use:**
- You need pre-computed aggregations (counts, sums, averages) that would be expensive to compute at query time.
- The aggregation is used frequently (e.g., dashboard summaries).
- You're okay with eventual consistency (the view updates asynchronously on insert).

**Example** (hypothetical):

```sql
CREATE MATERIALIZED VIEW telemetry_daily_token_usage
ENGINE = SummingMergeTree()
ORDER BY (gram_project_id, day, model)
AS SELECT
    gram_project_id,
    toDate(fromUnixTimestamp64Nano(time_unix_nano)) AS day,
    toString(attributes.gen_ai.response.model) AS model,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))) AS input_tokens,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))) AS output_tokens,
    count() AS request_count
FROM telemetry_logs
WHERE toString(attributes.gen_ai.operation.name) = 'chat'
GROUP BY gram_project_id, day, model;
```

### Decision guide

| Question | Materialized Column | Materialized View |
|---|---|---|
| Need to filter/GROUP BY a JSON path? | Yes | Overkill |
| Need pre-aggregated rollups? | No | Yes |
| Should new rows be queryable immediately? | Yes (sync on insert) | Mostly (async, slight delay) |
| Adds storage overhead? | Minimal (one column) | Significant (separate table) |
| Requires separate maintenance? | No | Yes (schema + view definition) |

### TODOs

- [ ] **Daily token usage rollup** — `GetProjectMetricsSummary` and `GetUserMetricsSummary` currently scan all matching rows. A `SummingMergeTree` materialized view partitioned by day would make dashboard queries near-instant.
- [ ] **Chat session summary view** — `SearchChats` aggregates per-chat metrics on every query. A materialized view with `AggregatingMergeTree` could maintain running totals.
- [ ] **Tool call success/failure rates** — The tool breakdown maps (`tool_counts`, `tool_success_counts`) in metrics queries are expensive aggregations that would benefit from a pre-aggregated view.
- [ ] **Remove legacy denormalized columns** — Complete the migration from manually-populated `gram_*` columns to expression-based materialized columns, then remove the manual extraction logic in `crud.go` and deprecate `ToolInfo`.

---

## ClickHouse Queries

Unlike PostgreSQL queries in other packages, ClickHouse queries are **not auto-generated** by sqlc since sqlc doesn't support ClickHouse. We use the **squirrel** query builder for dynamic query construction.

> **IMPORTANT:** Squirrel is ONLY used for ClickHouse queries in this package. All other database queries (PostgreSQL) MUST use sqlc-generated code. Do not use squirrel elsewhere in the codebase.

### Query Files

- **`repo/queries.sql.go`**: Query implementations using squirrel query builder
- **`repo/pagination.go`**: Cursor pagination helpers

### Adding a New Query

**Implement in `repo/queries.sql.go`** using squirrel:

1. Create a params struct for the query inputs
2. Use `sq.Select(...)` to build queries (the `sq` var is pre-configured for ClickHouse `?` placeholders)
3. Add optional filters with explicit `if` statements for clarity
4. Use helper functions from `pagination.go` for cursor pagination

### Extracting values from JSON attributes

Since attributes are stored as JSON, you'll need to extract and cast values in SQL. ClickHouse auto-unflattens dotted keys (e.g., `"gram.project.id"` becomes nested `{gram:{project:{id:...}}}`) so use dot notation to access nested paths. See [ClickHouse #69846](https://github.com/ClickHouse/ClickHouse/issues/69846) for details.

```sql
-- String extraction
toString(attributes.gram.tool.urn)

-- Integer extraction (with safe fallback)
toInt32OrZero(toString(attributes.http.response.status_code))

-- UUID extraction (nullable)
toUUIDOrNull(toString(attributes.gram.project.id))

-- Conditional aggregation
sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)),
      toString(attributes.gen_ai.operation.name) = 'chat')
```

### Pagination Helpers

`repo/pagination.go` provides cursor pagination:
- `withPagination(sb, cursor, sortOrder)` — WHERE-based cursor for simple queries
- `withHavingPagination(sb, cursor, sortOrder, projectID, groupColumn, timeExpr)` — HAVING-based cursor for GROUP BY queries
- `withHavingTuplePagination(...)` — HAVING with tuple comparison for tie-breaking
- `withOrdering(sb, sortOrder, primaryCol, secondaryCol)` — ORDER BY helper

### Example Pattern

```go
type ListItemsParams struct {
    ProjectID    string
    DeploymentID string  // optional filter
    SortOrder    string
    Cursor       string
    Limit        int
}

func (q *Queries) ListItems(ctx context.Context, arg ListItemsParams) ([]Item, error) {
    sb := sq.Select("id", "name", "created_at").
        From("telemetry_logs").
        Where("gram_project_id = ?", arg.ProjectID)

    if arg.DeploymentID != "" {
        sb = sb.Where(squirrel.Eq{"deployment_id": arg.DeploymentID})
    }

    sb = withPagination(sb, arg.Cursor, arg.SortOrder)
    sb = withOrdering(sb, arg.SortOrder, "created_at", "id")
    sb = sb.Limit(uint64(arg.Limit))

    query, args, err := sb.ToSql()
    if err != nil {
        return nil, fmt.Errorf("building query: %w", err)
    }

    rows, err := q.conn.Query(ctx, query, args...)
    // ... handle rows
}
```

## Pagination

### Service Layer Pagination (limit + 1 pattern)

Pagination logic lives in the **service layer** (`impl.go`), not the repo layer. The repo returns raw results, and the service handles cursor computation.

1. Client requests N items per page
2. Service queries repo with N+1 items
3. If N+1 items returned → compute `nextCursor` from item N, trim to N items
4. If ≤N items returned → `nextCursor = nil`, return all items
5. Cursor is the UUID of the last returned item

## Testing

Tests use testcontainers to spin up a real ClickHouse instance.

Key testing patterns:
- Use `testenv.Launch()` in `TestMain` to set up infrastructure
- Create helper functions for inserting test data
- Use table-driven tests with descriptive names
- Use `require.Eventually` after inserts to handle ClickHouse eventual consistency — **do not use `time.Sleep`**

## Data Models

See `repo/models.go` for struct definitions with ClickHouse field tags (`ch:"field_name"`).
