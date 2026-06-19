---
name: gram-telemetry-query-dimensions
description: How to add a new attribute value (dimension) that the generic org-scoped telemetry.query analytics endpoint can group by and filter on. Activate whenever the task is to expose a new breakdown/filter axis (e.g. department, job_title, model, provider, a new WorkOS directory attribute or request attribute) in telemetry.query, or mentions the attribute_metrics_summaries materialized view, queryDimensions, or attributeDimensionRegistry.
---

## What this covers

`telemetry.query` (`POST /rpc/telemetry.query`) is a generic, org-scoped analytics
endpoint. It groups pre-aggregated usage metrics by an allowlisted **dimension**
and filters on those same dimensions. This skill explains how to add a **new
dimension** (a new attribute value to group/filter by).

> **Dimensions vs. measures.** A _dimension_ is a breakdown/filter axis
> (`department_name`, `model`, `role`). A _measure_ is a number being aggregated
> (`total_cost`, `total_tokens`). This skill is about **dimensions only**. Adding
> a new measure is a different (parallel) path through the same files —
> `queryMeasures`, `attributeMeasureSelects`, `AttributeMetricsMeasures`,
> `QueryMeasures`.

Everything is backed by one ClickHouse `AggregatingMergeTree`,
`attribute_metrics_summaries`, fed by `attribute_metrics_summaries_mv`. Each
dimension is one **column** on that table. The query layer never sees raw JSON
paths or SQL from clients — dimensions are a closed allowlist validated at three
layers that must stay in sync.

Activate the **`clickhouse`** skill (schema/MV work) and **`golang`** skill
(repo/service edits) alongside this one.

## The four layers (keep them in sync)

A dimension key like `department_name` must appear in all four places. Adding a
new one means touching each:

| Layer                   | File                                                  | What to add                                                                                                                                                           |
| ----------------------- | ----------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. ClickHouse schema    | `server/clickhouse/schema.sql`                        | a column on `attribute_metrics_summaries`, the matching SELECT + `GROUP BY` in `attribute_metrics_summaries_mv`, and the column in the table's `ORDER BY` sorting key |
| 2. Goa design allowlist | `server/design/telemetry/design.go`                   | the public key string in `queryDimensions`                                                                                                                            |
| 3. Repo registry        | `server/internal/telemetry/repo/attribute_metrics.go` | an `attributeDimensionRegistry` entry mapping the public key → column + kind                                                                                          |
| 4. Generated code       | (run gen tasks)                                       | regenerate Goa server + SDK                                                                                                                                           |

`dimension_values` (the per-group distinct-value lists on each `QueryRow`) is
**automatic** — it iterates `attributeDimensionRegistry`, so a new dimension
shows up there with no extra code.

## Pick the dimension kind

The repo classifies every dimension as one of three `attributeDimensionKind`s.
This drives how it is grouped and filtered:

- **`attributeDimScalar`** — one string value per row (e.g. `department_name`,
  `model`). Group: plain column. Filter: `IN`.
- **`attributeDimArray`** — `Array(String)`, multiple values per user/row (e.g.
  `roles`, `groups`). Group: `arrayJoin()` (attributes spend to each element).
  Filter: `hasAny()`. Stored intact in the sorting key (one array per row) so it
  does **not** multiply row count.
- **`attributeDimProject`** — the `gram_project_id` UUID key column; grouped via
  `toString()`.

Most new identity/request attributes are **scalar**. Use array only when the
attribute is genuinely list-valued per user.

> **Cardinality.** The MV stays cheap because every scalar dimension is
> functionally determined by the user (one department, one job title per user),
> so adding one does not multiply rows. Do **not** add a high-cardinality,
> per-event scalar (e.g. a raw request id) as a dimension — it would explode the
> aggregate.

## Step-by-step

### 1. ClickHouse schema + migration

Edit `server/clickhouse/schema.sql`. For a scalar dimension `foo` derived from a
log attribute `attributes.user.attributes.foo`:

1. Add the column to the `attribute_metrics_summaries` **table**:
   ```sql
   foo String,
   ```
2. Add the derived SELECT to `attribute_metrics_summaries_mv` and the column to
   its `GROUP BY`:
   ```sql
   -- in the SELECT list
   toString(attributes.user.attributes.foo) AS foo,
   -- ...and in GROUP BY
   GROUP BY gram_project_id, time_bucket, department_name, ..., foo;
   ```
   (Array dimensions use `CAST(attributes.user.foo AS Array(String)) AS foo`.
   If a materialized column already exists on `telemetry_logs` — e.g. `user_email`,
   `hook_source` — reference it directly instead of re-deriving from JSON.)
3. Add the column to the table's **`ORDER BY` sorting key** (scalar columns slot
   in with the other identity dimensions; array columns go at the end alongside
   `roles, groups`). The sorting key keeps cardinality bounded.

Then generate the migration (this is a **ClickHouse** migration, governed by the
same expand/contract rules as Postgres — see the migration rules in `CLAUDE.md`):

```sh
mise clickhouse:diff add-foo-attribute-dimension
```

Atlas diffs `schema.sql` and emits the migration into
`server/clickhouse/migrations/` (and the golang-migrate copy), then auto-runs
`clickhouse:gen-materialized-cols`. **Never hand-edit** the generated migration
or `atlas.sum`.

> **MV recreation + backfill.** Changing the MV's SELECT/`GROUP BY` and the
> table sorting key makes Atlas **drop and recreate** the MV. The MV only
> transforms rows ingested _after_ it exists — historic rows are **not**
> re-aggregated, so the new column reads empty (`''`) for old buckets. That is
> acceptable (data ages out at the 30-day TTL); call it out in the PR. Run
> migrations against **local DBs only**.

### 2. Goa design allowlist

In `server/design/telemetry/design.go`, add the public key to `queryDimensions`:

```go
var queryDimensions = []any{
    "department_name",
    // ...
    "foo", // <-- new
}
```

This single slice feeds the `Enum(...)` on both `QueryPayload.group_by` and
`QueryFilter.dimension`, so the new key becomes a valid group-by and filter
value automatically. (The public key need not equal the column name — `email`
maps to `user_email`.)

### 3. Repo registry

In `server/internal/telemetry/repo/attribute_metrics.go`, add the mapping to
`attributeDimensionRegistry` (public key → safe column expression + kind):

```go
var attributeDimensionRegistry = map[string]attributeDimension{
    // ...
    "foo": {column: "foo", kind: attributeDimScalar},
}
```

This is the **only** place a key maps to a real column, and it is what keeps
client input from ever reaching SQL as a raw path. The registry powers grouping,
filtering, **and** the `dimension_values` map — so once it's here, the new
dimension's distinct values automatically appear in every group's
`dimension_values`.

> Keep `queryDimensions` (design) and `attributeDimensionRegistry` (repo) in
> sync. A key in the Enum without a registry entry returns
> `unknown group_by/filter dimension` at runtime; a registry entry not in the
> Enum is unreachable.

### 4. Regenerate + verify

```sh
mise gen:goa-server   # regenerate Goa types/openapi from the design
mise gen:sdk          # regenerate the TypeScript SDK from the OpenAPI spec
```

Then:

```sh
go build ./server/internal/telemetry/...
mise lint:server
pnpm -F dashboard type-check
```

### 5. Tests

- Unit (`query_internal_test.go`): `buildQueryResult` is dimension-agnostic;
  add coverage only if you changed rollup behavior.
- Integration (`query_test.go`, requires ClickHouse): `insertAttributeUsageLog`
  writes a telemetry row. Add your attribute to the inserted JSON, then assert
  the new dimension groups/filters and appears in `dimension_values`. Group-by
  assertions wait on `require.Eventually` because the MV is eventually
  consistent.

```sh
go test ./server/internal/telemetry/ -run TestQuery -count=1
```

## Gotchas

- **Sorting-key drift.** The migration's `ORDER BY` must match `schema.sql`
  exactly, or Atlas will want to rebuild the table. Let `clickhouse:diff`
  generate it; don't hand-write.
- **Empty values are filtered.** `dimension_values` drops `''`, so unset
  attributes show as an empty list. Grouping by a dimension where the attribute
  is unset surfaces under the `''` group (and array dimensions map an empty
  array to a single `''` element so role-less spend isn't dropped).
- **Don't add measures here.** If the request is really "expose a new number to
  aggregate," that's the measures path, not a dimension.
- **Aggregate read combinators.** Measure columns are `*If` aggregate states and
  must be read with the matching `*IfMerge` combinators (see
  `attributeMeasureSelects`). Dimensions are plain columns and need none of this.
