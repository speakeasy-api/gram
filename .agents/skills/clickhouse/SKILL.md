---
name: clickhouse
description: Use when changing or reviewing Gram ClickHouse schemas, migrations, queries, inserts, access principals, bootstrap SQL, Cloud compatibility, partial migration failures, or performance for analytics, telemetry, risk, authz, and spend features
---

## Official ClickHouse guidance

Gram conventions and the checked-in schema remain authoritative. Also use:

- `clickhouse-best-practices` when reviewing or changing a ClickHouse schema, query, insert strategy, or configuration. Read its applicable rule files and cite the rules in review findings.
- `clickhouse-architecture-advisor` when choosing between ingestion patterns, raw tables and materialized views, partitioning or retention strategies, joins or enrichment, or mutable-state models.

## Infrastructure ownership and Cloud compatibility

Local ClickHouse success is **not** proof of ClickHouse Cloud compatibility. Local containers and CI accept DDL and authentication settings that Cloud rejects.

| Object                                                         | Owner                                          |
| -------------------------------------------------------------- | ---------------------------------------------- |
| Databases, users, roles, credentials, role settings            | Terraform; never application schema migrations |
| Tables, views, materialized views, schema-bound grants/revokes | Atlas migrations, mirrored in golang-migrate   |
| Local/CI/Atlas development-database prerequisites              | `local/clickhouse/initdb/01-marts-definer.sql` |

Provision infrastructure prerequisites **before** applying dependent migrations. Do not put `CREATE DATABASE`, `CREATE USER`, or `CREATE ROLE` into desired schema files or either migration flavor, or drop infrastructure-owned objects in down migrations. If generation proposes these statements, fix the bootstrap/baseline and regenerate; do not accept them just because local replay passes.

**Cloud footguns:**

- `CREATE DATABASE ... ENGINE = Atomic` is not supported in ClickHouse Cloud. Do not explicitly select the local database engine for Cloud.
- `CREATE USER ... HOST NONE` without authentication is still a passwordless user definition. Cloud's default policy rejects it; `HOST NONE` does not waive the password requirement. Never relax that policy to accommodate a definer.
- Provision Cloud definers through Terraform with a generated password that is not exposed to people, logs, outputs, or this repository. A definer does not need an interactive login, but still needs valid authentication configuration.

**Local bootstrap is an exception, not a deployment template.** `01-marts-definer.sql` supplies the `marts` database, `marts_reader` role/limits, and `marts_definer` principal to local containers, CI replay, and Atlas's development database (`server/atlas.hcl`). Keep it idempotent. Its passwordless `HOST NONE` user is local-only; never copy it into Cloud provisioning or migrations.

For marts, edit `server/clickhouse/mart.sql`. Views use `DEFINER = marts_definer SQL SECURITY DEFINER`: reads of underlying tables run with the definer's privileges, not the reader's. Migrations own the definer's narrowly scoped source grants and the reader's grants on approved views, **not** the principals. Atlas ignores grants in desired state, so keep explicit matching grants/revokes in both migration flavors. See [ClickHouse view SQL security](https://clickhouse.com/docs/sql-reference/statements/create/view#sql_security).

## Partially applied migrations

ClickHouse migrations can fail after earlier statements have taken effect. Editing the failed file and rehashing does **not** reconcile those effects with Atlas's recorded progress; repeated in-place edits can leave the runner stuck.

- Treat published/applied migration files as immutable during normal development. Use a forward migration; if a failed revision prevents progress, recovery requires an explicit operator-controlled repair, not another automatic retry.
- Before repair, stop concurrent migration/reconciliation attempts and inspect both the actual objects/grants and Atlas revision state. Identify the exact corrected migration artifact and which statements already ran.
- Reconcile the database to that artifact deliberately. Rewinding with `atlas migrate set` changes revision bookkeeping; it does **not** undo SQL. Only consider rewinding to the preceding revision and applying one migration after proving **every** statement is safe to replay and accounting for existing effects. `IF NOT EXISTS` alone does not prove existing objects have the intended definition.
- Verify database state and migration status before resuming automated reconciliation. Never blindly rewind, mark a failed migration applied, or clear its error to bypass unfinished work.

## Schema design and evolution

The ClickHouse schema is defined in `server/clickhouse/schema.sql`, with marts views in `server/clickhouse/mart.sql`. Edit the relevant desired schema file and generate a migration:

```sh
mise run clickhouse:diff <migration-name>
```

This produces migrations in **two flavors** that must always stay in sync:

- `server/clickhouse/migrations/` — **Atlas** format. This is the source of truth: only these migrations are carried forward and applied in production.
- `server/clickhouse/local/golang_migrate/` — **golang-migrate** format (`.up.sql`/`.down.sql` pairs). Used only for local development, so contributors without an Atlas Pro login can still run migrations.

`mise run clickhouse:diff` generates both flavors together. When adjusting a newly generated, unpublished migration (for example, adding grants Atlas ignores), make the equivalent change in **both** directories, then run `mise run clickhouse:hash` to regenerate `atlas.sum`. Hashing updates checksums, not database or revision state. Apply pending migrations locally with `mise run clickhouse:migrate`.

**No semicolons in `COMMENT '...'` strings.** golang-migrate splits statements naively, so a semicolon inside a column/table comment breaks its parser and the local migrations fail to replay. Rephrase the comment instead.

Three CI checks guard this on every PR:

- **atlas-lint** — runs the Atlas migration linter, plus a porcelain check that migrations were generated and are up to date with both the Postgres and ClickHouse schemas. If you edited `schema.sql` without running `mise run clickhouse:diff`, this fails.
- **golang-migrate-clickhouse** — replays all golang-migrate migrations against a real ClickHouse instance to prove they're valid. If the two flavors drifted, this is where it shows up.
- **migration-order** — fails if a newly added migration in either ClickHouse dir (`server/clickhouse/migrations` or `server/clickhouse/local/golang_migrate`) has a timestamp at or before the latest already on `main`. It also runs in the merge queue, so two PRs branched from the same head cannot both land and produce non-linear history (the INC-418 failure mode).

**Out-of-order timestamps.** If `migration-order` reports a ClickHouse migration timestamp at or before the latest on `main`, do **not** rename files, hand-edit `atlas.sum`, or run `atlas migrate rebase`. Delete the generated Atlas migration and both matching golang-migrate `.up.sql`/`.down.sql` files, identified by migration name; update from `main`; then re-run `mise run clickhouse:diff <name>`. That regenerates both flavors on top with fresh, ordered timestamps and keeps them in sync. Avoid `atlas migrate rebase`: it renames only one dir (drifting the two flavors apart) and, for golang-migrate, moves an already-applied migration to a higher version so `migrate up` silently skips it on teammates' local databases.

## ClickHouse Queries (Telemetry Package)

The `server/internal/telemetry` package uses ClickHouse for high-performance analytics queries. Unlike PostgreSQL queries, ClickHouse queries are not auto-generated by SQLc. The telemetry repository uses Squirrel for dynamic query construction.

> **CRITICAL:** Squirrel is permitted for ClickHouse repository code, including packages outside telemetry. Never use it for PostgreSQL queries; PostgreSQL repositories must use SQLc-generated code. Follow the target ClickHouse package's neighboring query and scan patterns.

### Read from summary views, not raw `telemetry_logs`

The pre-aggregated **materialized views are the default read path** for anything that powers a dashboard, analytics surface, or filter control: `trace_summaries`, `metrics_summaries`, `attribute_metrics_summaries`, `attribute_keys`, `chat_token_summaries`. Query the **raw `telemetry_logs`** table **only** in the rare cases where per-log detail is genuinely required:

- individual log records / a single trace's full log list (detail/inspection views),
- free-text search over the log `body`,
- arbitrary user-supplied attribute-path filters (e.g. `@user.region`) that a fixed-column summary cannot express,
- an event shape not yet captured by any summary (e.g. traceless trigger events).

**Why it matters:** raw `telemetry_logs` reads are full-range table scans with per-row JSON extraction; the summary views are pre-aggregated and cheap. A filter dropdown, summary card, count, or default list that scans raw logs on every page load is a **performance bug** — the unified Tool Logs page regressed exactly this way before being moved back onto `trace_summaries`. If a summary is missing a column you need, prefer **extending the MV (+ a backfill migration)** over falling back to a raw-log scan.

**Reading `trace_summaries`** (one row per `trace_id`, `AggregatingMergeTree`): `GROUP BY trace_id`, use `*Merge` combinators for `AggregateFunction` columns (`anyIfMerge(http_status_code)`) and plain `any()`/`min()`/`sum()`/`max()` for `SimpleAggregateFunction` columns. Tool calls carry a real `trace_id` (recorded by the gateway in `ToolProxy.Do`), so hosted MCP, shadow MCP, skill, and local tool events are all present in the view.

> **Gotcha — `ILLEGAL_AGGREGATION` (code 184):** when a grouped read is wrapped in a CTE/subquery that a _caller_ then aggregates over (e.g. `uniqExact(tool_name)` over a `WITH normalized_events AS (... GROUP BY trace_id ...)`), ClickHouse merges the subquery back into the outer aggregate **if an aggregate alias shadows a base column** (`any(gram_urn) AS gram_urn`). Alias grouped aggregates to non-colliding names — prefix them, e.g. `any(gram_urn) AS g_gram_urn` — so the boundary holds.

### File Structure

- **`queries.sql.go`**: Query implementations using squirrel
- **`pagination.go`**: Cursor pagination helpers (`withPagination`, `withOrdering`, etc.)
- **`README.md`**: Detailed documentation and patterns specific to ClickHouse

### Adding ClickHouse Queries

When asked to add a new ClickHouse query to the telemetry package:

1. **Create a params struct** for query inputs
2. **Build the query using squirrel** (the `sq` var in queries.sql.go is pre-configured for ClickHouse):

   ```go
   type GetMetricsParams struct {
       ProjectID    string
       DeploymentID string  // optional
       Limit        int
   }

   func (q *Queries) GetMetrics(ctx context.Context, arg GetMetricsParams) ([]Metric, error) {
       sb := sq.Select("id", "value", "timestamp").
           From("metrics").
           Where("project_id = ?", arg.ProjectID)

       // Optional filters - explicit conditionals for clarity
       if arg.DeploymentID != "" {
           sb = sb.Where(squirrel.Eq{"deployment_id": arg.DeploymentID})
       }

       sb = sb.Limit(uint64(arg.Limit))

       query, args, err := sb.ToSql()
       if err != nil {
           return nil, fmt.Errorf("building query: %w", err)
       }

       rows, err := q.conn.Query(ctx, query, args...)
       // ... handle rows
   }
   ```

3. **Use pagination helpers** from `pagination.go`:
   - `withPagination(sb, cursor, sortOrder)` - cursor pagination
   - `withOrdering(sb, sortOrder, primaryCol, secondaryCol)` - ORDER BY

### Testing ClickHouse Queries

- Use the target package's existing ClickHouse fixture pattern (`testenv.Launch` or `testenv.NewTestClickhouse`) and a real ClickHouse container.
- For `async_insert=0`, read directly after the insert.
- For `async_insert=1, wait_for_async_insert=1`, read directly after the insert returns.
- For `async_insert=1, wait_for_async_insert=0`, call `testenv.FlushClickHouseAsyncInserts(t, conn)` after the application issues the write and before reading. Synchronize any application goroutine first; do not use `time.Sleep` or polling to wait for the async queue.
- Do not use `clickhouse.WithAsync(false)` to request synchronous insertion: it enables fire-and-forget async inserts. Omit async options or set `async_insert=0` explicitly.
- Use table-driven tests with descriptive `it`-prefix names and helper functions for test data insertion.

See `server/internal/telemetry/README.md` for comprehensive documentation.
