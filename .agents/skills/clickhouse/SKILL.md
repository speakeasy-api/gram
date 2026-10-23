---
name: clickhouse
description: Use when changing or reviewing Speakeasy ClickHouse schemas, migrations, CHECK constraints, queries, inserts, access principals, bootstrap SQL, Cloud compatibility, partial migration failures, or performance for analytics, telemetry, risk, authz, and spend features
---

## Official ClickHouse guidance

Speakeasy conventions and the checked-in schema remain authoritative. Also use:

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

- Treat published/applied migration files as immutable. A failed revision blocks every later migration, so fixing forward is not available; recovery is an explicit operator-controlled repair, never an in-place edit. Re-running `mise run clickhouse:migrate` resumes the file at the first unapplied statement, so it is a repair step only when `system.query_log` on the primary shows that statement never ran on the server, the target holds no rows from it, and it can succeed as written. A whole-table backfill that dropped the connection cannot.
- Before repair, stop concurrent migration attempts (production ClickHouse migrations are applied by an operator running `mise run clickhouse:migrate`, not by CI, so make sure nobody else is running it) and inspect both the actual objects/grants and Atlas revision state. Identify the exact corrected migration artifact and which statements already ran. The revision row can understate the failure: Atlas writes the error to it last, and that write fails too when the connection is gone.
- Reconcile the database to that artifact deliberately. `atlas migrate set` changes revision bookkeeping; it does **not** run or undo SQL. Prefer having a human operator perform the missing effect and then marking the file applied (see the operator repair below). `atlas migrate set <previous>` followed by a re-run replays the whole file; that is an option only when no statement in it has taken effect yet and the failing statement can succeed as written. Resuming and replaying are different retries with different gates. `IF NOT EXISTS` alone does not prove existing objects have the intended definition.
- Verify database state and migration status before resuming automated reconciliation. Mark a file applied only once every statement's effect is present and verified. Never use `atlas migrate set` to skip work that has not happened, and never clear an error to get an automatic retry past unfinished work.

**Operator repair when a statement cannot succeed as written.** Agents never run anything against dev or prod ClickHouse, the same rule the `postgresql` skill states; a human operator performs the repair. Prepare it for them: the statement's effect in small pieces (for a backfill, one source partition per `INSERT`, see below), a verification query (source `GROUP BY` totals against `*Merge` reads of the target, per partition), and the two commands below, run from `server/` with an Atlas login, that record the file as applied once the verification passes:

```sh
atlas migrate set <version> --dir file://clickhouse/migrations --config file://atlas.hcl -u "$CLICKHOUSE_URL"
atlas migrate status --dir file://clickhouse/migrations --config file://atlas.hcl -u "$CLICKHOUSE_URL"
```

Leave the migration file untouched; it is published and hashed, and the local golang-migrate flavor still replays it on small data.

## Schema design and evolution

The ClickHouse schema is defined in `server/clickhouse/schema.sql`, with marts views in `server/clickhouse/mart.sql`. Edit the relevant desired schema file and generate a migration:

```sh
mise run clickhouse:diff <migration-name>
```

This produces migrations in **two flavors** that must always stay in sync:

- `server/clickhouse/migrations/` — **Atlas** format. This is the source of truth: only these migrations are carried forward and applied in production.
- `server/clickhouse/local/golang_migrate/` — **golang-migrate** format (`.up.sql`/`.down.sql` pairs). Used only for local development, so contributors without an Atlas Pro login can still run migrations.

`mise run clickhouse:diff` generates both flavors together. When adjusting a newly generated, unpublished migration (for example, adding grants Atlas ignores), make the equivalent change in **both** directories, then run `mise run clickhouse:hash` to regenerate `atlas.sum`. Hashing updates checksums, not database or revision state. Apply pending migrations locally with `mise run clickhouse:migrate`.

**Every column needs a data classification.** End each column `COMMENT` with a line `@access: <class>` (for example `COMMENT 'Raw log body.\n@access: opaque-restricted'`). Classes are `confidential` (internal IDs, timestamps, Speakeasy-controlled enums, counts, short labels), `confidential-pii` (emails, personal names, IPs, person IDs in external IdPs), `opaque-confidential` (short customer- or producer-chosen identifiers and labels support needs: tool, skill, agent, MCP server and client names, span and event names, producer session IDs, attribute keys), `secret-restricted` (credentials, tokens and their hashes, encrypted auth material), `restricted` (billing-system IDs, customer-configured endpoints, other known-sensitive or uncertain data) or `opaque-restricted` (arbitrary customer, user or model content: messages, prompts, tool payloads, customer JSON, free-form descriptions). A `Nested(...)` column shares one comment across its subcolumns, so use the strictest class among them. The server test suite in CI fails on unclassified columns (run `go test ./internal/dataclassification/...` from `server/` locally; needs Docker); see `server/internal/dataclassification`.

**Keep `CHECK` constraints minimal.** ClickHouse evaluates `CONSTRAINT ... CHECK` on every insert and rejects the whole batch on a violation. Use only simple single-column shape checks: `notEmpty(col)` or a length bound (`length(col) <= 64`). Do not encode enums (`status IN (...)`, `has([...], status)`), numeric ranges, or rules that relate columns (`(status = 'failed') = notEmpty(failure_reason)`). Each rule change would need an `ALTER TABLE ... DROP CONSTRAINT` / `ADD CONSTRAINT` migration in both flavors, while the Go parser or writer that builds the rows changes with the code. Validate there, drop or log rows that fail before the insert, and test it there. Decline reviewer suggestions to add or rely on such constraints, and do not describe their absence as a gap.

**No semicolons in `COMMENT '...'` strings.** golang-migrate splits statements naively, so a semicolon inside a column/table comment breaks its parser and the local migrations fail to replay. Rephrase the comment instead.

**Never backfill a materialized view with one whole-table statement.** A new view only folds rows that arrive after it exists, so history needs a backfill. One `INSERT INTO ... SELECT ... GROUP BY` over the whole source can exceed what a replica survives, and when the replica dies Atlas reports only `driver: bad connection`, leaves the file partially applied, and blocks every later release until the operator repair above. Instead:

- Ship only the DDL in the migration and hand the backfill to a human operator as `clickhouse client` statements against the primary, one source partition per statement (`WHERE toYYYYMMDD(created_at) = <day>` for a table partitioned by day; read the key from `system.tables.partition_key`) with `--max_memory_usage` capped so a bad chunk throws instead of taking the replica down. Size the cap from a measurement rather than a guess: run the largest chunk's SELECT with `FORMAT Null` on the read replica, read its `peak_memory_usage` from `system.query_log` (`type = 'QueryFinish'`), and cap at a few times that while staying well under the replica's memory; the INSERT adds write overhead the SELECT does not show, and the primary carries live load.
- If the data must ship in the migration, write one `INSERT` per partition rather than one statement. Each Atlas statement also has to finish inside the driver's read deadline, which is set once per statement (300s by default, `read_timeout=<duration>` on the connection URL raises it).
- Disjoint chunks are always safe for an `AggregatingMergeTree` target: partial states from different chunks merge to the same answer. Re-running a chunk, or overlapping with the live view, is harmless only for idempotent aggregates (`argMax`, `min`, `max`, `any`); for those, create the view before the backfill so no row lands in the gap. For `sum` or `count` an overlap double counts, so the view goes last: backfill the bulk below a fixed timestamp `T` in chunks, then run one catch-up pass for `>= T` immediately before creating the view, chunked and memory-capped like the bulk; the seconds between that pass and the view's creation are a small, bounded undercount, the safe direction for billing data. A bad `sum` or `count` chunk has no clean undo unless the target is partitioned by a key aligned with the chunks and no view rows share that partition (`ALTER TABLE ... DROP PARTITION`), so verify each chunk against the source before running the next.

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

**Why it matters:** raw `telemetry_logs` reads are full-range table scans with per-row JSON extraction; the summary views are pre-aggregated and cheap. A filter dropdown, summary card, count, or default list that scans raw logs on every page load is a **performance bug** — the unified Tool Logs page regressed exactly this way before being moved back onto `trace_summaries`. If a summary is missing a column you need, prefer **extending the MV with a DDL migration plus a chunked operator backfill** (see "Never backfill a materialized view with one whole-table statement") over falling back to a raw-log scan.

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
- For fire-and-forget writes, set only `async_insert=1` on the query and leave `wait_for_async_insert` to the connection: production sets 0, and the test connection sets 1, so tests read directly after the insert returns. Synchronize any application goroutine that issues the write before reading. Never pin `wait_for_async_insert=0` per query; it overrides the test connection and makes reads race the async queue. Do not flush the queue with `SYSTEM FLUSH ASYNC INSERT QUEUE` as a barrier (it can miss batches already dequeued for a background flush), and do not use `time.Sleep` or polling, which only hides the race.
- Every insert states its async posture. ClickHouse defaults `async_insert` to 1 since 26.2, so an insert that sends no async settings inherits it plus the server connection's `wait_for_async_insert=0` and becomes fire-and-forget in production, while tests (connection `async_insert=0`) never notice. Use `async_insert=0`, or `async_insert=1` with `wait_for_async_insert=1` when the caller reads its own writes. Use `async_insert=0` when a table has CHECK constraints whose violations the caller reports per insert: an async flush merges rows from many queries into one block, so one writer's bad row fails every insert in that flush.
- Do not use `clickhouse.WithAsync(false)` to request synchronous insertion: it enables fire-and-forget async inserts. Set `async_insert=0` explicitly instead; omitting async options is fire-and-forget on the server connection too.
- Use table-driven tests with descriptive `it`-prefix names and helper functions for test data insertion.

See `server/internal/telemetry/README.md` for comprehensive documentation.
