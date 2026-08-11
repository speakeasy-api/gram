# risk_findings message_created_at / assistant_id column backfill

Backfills two columns onto **existing ClickHouse `risk_findings` rows** from
Postgres, via ClickHouse **mutations** (`ALTER TABLE ... UPDATE`):

| ClickHouse column    | Type / default                     | Source of truth in Postgres                                                                             |
| -------------------- | ---------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `message_created_at` | `DateTime64(9) DEFAULT created_at` | `chat_messages.created_at` of the scanned message (finding `created_at` when no message resolves)       |
| `assistant_id`       | `String DEFAULT ''`                | `assistant_threads.assistant_id` of a live (`deleted IS FALSE`) thread whose `chat_id` matches the chat |

Rows written before the columns existed carry the DEFAULTs (`message_created_at`
= scan time, `assistant_id` = `''`); this migration rewrites them in place with
the true values. Run it with the migration subcommand:

```
go run ./server/cmd/tools/migrations riskfindingscols [flags]
```

For the generic pipeline concepts (Source / Transform / Sink, `Criteria`,
lifecycle), see [README.md](./README.md).

## Why mutations, not inserts

Re-inserting enriched copies of old rows (and letting the read path dedup)
**does not work here**. The read path dedups duplicate ids by keeping the copy
that sorts first under `message_created_at DESC ... inserted_at DESC`. An old
copy's DEFAULT `message_created_at` equals its `created_at` — the scan time —
which is always **>= the enriched copy's true event time** (the message
predates its scan). The unenriched copy would therefore sort first and win,
making an insert-based backfill a no-op at best. Rewriting the existing row via
a mutation is the only correct shape.

## Stages

Implemented in the `riskfindingscols` package:

| Stage       | Type                           | What it does                                                                                                                                                                                             |
| ----------- | ------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Source      | `riskfindingscols.Source`      | Keyset-paginates `risk_results` by `id` (UUIDv7, so id order is time order), LEFT JOINing `chat_messages` for the event time and a lateral live-`assistant_threads` lookup for the assistant. Resumable. |
| Transformer | `riskfindingscols.Transformer` | Near pass-through (kept for harness symmetry); guards a zero message time by falling back to the finding's `created_at`.                                                                                 |
| Sink        | `riskfindingscols.Sink`        | Batches updates and submits ONE mutation per batch (see below).                                                                                                                                          |

### What the source reads

Only rows that exist in ClickHouse are scanned: `found IS TRUE AND rule_id IS
NOT NULL` mirrors both the live outbox emission and the riskfindings backfill,
so "nothing found" sentinels and dead-letter rows are never mutated.

- **`message_created_at`**: `chat_messages.created_at` via
  `risk_results.chat_message_id`. Content-part-anchored findings (the only rows
  whose message join can miss — message-anchored rows cascade-delete with their
  message) fall back to the finding's own `created_at`, which is exactly the
  ClickHouse column DEFAULT, making the update a no-op for them rather than a
  wrong value.
- **`assistant_id`**: mirrors the live `GetAssistantThreadAssistantIDByChatID`
  lookup — a live (`deleted IS FALSE`) `assistant_threads` row whose `chat_id`
  matches the scanned message's chat, `ORDER BY id LIMIT 1` for determinism.
  Soft-deleted threads and chats without threads yield `''`.

### The mutation

Each sink batch becomes one statement:

```sql
ALTER TABLE risk_findings UPDATE
    message_created_at = transform(toString(id), ['<id>', ...], [toDateTime64('<ns>', 9), ...], message_created_at),
    assistant_id       = transform(toString(id), ['<id>', ...], ['<assistant>', ...], assistant_id)
WHERE id IN ('<id>', ...) AND created_at >= toDateTime64('<min batch created_at - 1h>', 9)
```

- The `created_at` lower bound exists purely for **partition pruning**
  (`risk_findings` is `PARTITION BY toYYYYMMDD(created_at)`): without it every
  mutation would rewrite every partition. ClickHouse `created_at` is the exact
  value copied from Postgres, so the one-hour slack is pure headroom.
- Values are inlined as literals, not bound: clickhouse-go's positional binding
  formats `time.Time` at **second** precision, which would truncate the
  sub-second event times this migration exists to backfill. Timestamps render
  as `toDateTime64('<unix-nanos>', 9)` (timezone-independent, the driver's own
  nanosecond rendering); ids and assistant ids are canonical UUID strings.
- The default batch size is small (500) because each row contributes its id
  three times to the statement text, and ClickHouse's default `max_query_size`
  is 256 KiB.

**Mutations are asynchronous.** `ALTER TABLE ... UPDATE` returns once the
mutation is queued; ClickHouse rewrites the affected parts in the background.
The tool fires and continues, logging each batch's id range. A queued mutation
is durable server-side (it survives restarts), so the resume cursor advances on
submission. Watch progress with:

```sql
SELECT mutation_id, command, parts_to_do, is_done, latest_fail_reason
FROM system.mutations
WHERE table = 'risk_findings' AND NOT is_done;
```

**Idempotency.** Re-running a batch rewrites the same rows to the same values,
so overlap after a resume is harmless — no dedup token or quiescing of the live
writer is needed (the live writer inserts _new_ ids; this tool only updates
_existing_ ones).

## Flags

Secrets are **never** flag values — they would leak through `argv` / `ps`:

| Secret (env var)      | Meaning                               |
| --------------------- | ------------------------------------- |
| `GRAM_DATABASE_URL`   | Postgres connection string (required) |
| `CLICKHOUSE_PASSWORD` | ClickHouse password                   |

Non-secret flags:

| Flag              | Env fallback             | Default     | Meaning                                                                             |
| ----------------- | ------------------------ | ----------- | ----------------------------------------------------------------------------------- |
| `-ch-host`        | `CLICKHOUSE_HOST`        | `localhost` | ClickHouse host (IPv4, IPv6, or DNS)                                                |
| `-ch-database`    | `CLICKHOUSE_DATABASE`    | `default`   | ClickHouse database                                                                 |
| `-ch-username`    | `CLICKHOUSE_USERNAME`    | `gram`      | ClickHouse username                                                                 |
| `-ch-native-port` | `CLICKHOUSE_NATIVE_PORT` | `9440`      | ClickHouse native protocol port                                                     |
| `-ch-insecure`    | `CLICKHOUSE_INSECURE`    | `false`     | Skip ClickHouse TLS verification                                                    |
| `-org`            | —                        | (all)       | Scope to one `organization_id`                                                      |
| `-project`        | —                        | (all)       | Scope to one `project_id` (uuid)                                                    |
| `-from`           | —                        | (beginning) | Lower time bound, RFC3339 (`created_at >= from`); applies with or without `-cursor` |
| `-to`             | —                        | (end)       | Upper time bound, RFC3339 (`created_at < to`)                                       |
| `-cursor`         | —                        | —           | Resume after this `risk_results` id (exclusive); keyset resume position only        |
| `-batch-size`     | —                        | `500`       | Rows per source page and per mutation                                               |
| `-buffer`         | —                        | `500`       | Channel buffer between pipeline stages                                              |
| `-dry-run`        | —                        | `true`      | When true, read + report but submit nothing (and do not connect to ClickHouse)      |

An interrupted run (Ctrl-C / SIGTERM) exits with a **nonzero** status and logs
the `-cursor` to resume from, so shell automation never mistakes a partial
backfill for a completed one. As with the riskfindings migration, **repeat the
original `-org`/`-project`/`-from`/`-to` filters when resuming** — the cursor
does not encode query scope.

## Examples

Dry run over everything (counts and batch ranges only, no ClickHouse
connection):

```bash
GRAM_DATABASE_URL=postgres://gram:gram@127.0.0.1:5439/gram?sslmode=disable \
  go run ./server/cmd/tools/migrations riskfindingscols
```

Apply, scoped to one org and time window:

```bash
GRAM_DATABASE_URL=... CLICKHOUSE_PASSWORD=... \
  go run ./server/cmd/tools/migrations riskfindingscols \
  -ch-host 127.0.0.1 -ch-database gram -ch-username gram \
  -org org_123 -from 2026-05-01T00:00:00Z -to 2026-07-01T00:00:00Z \
  -dry-run=false
```

Resume an interrupted run from the last printed cursor (same scope flags):

```bash
go run ./server/cmd/tools/migrations riskfindingscols \
  -org org_123 -from 2026-05-01T00:00:00Z -to 2026-07-01T00:00:00Z \
  -cursor 019f65f6-ed75-7186-84a5-7ed095aab7b3 -dry-run=false
```

## Safety and caveats

- **Dry run by default.** Nothing is submitted unless you pass
  `-dry-run=false`. A dry run reports no resume cursor — nothing was submitted,
  so there is no checkpoint to resume the real migration from.
- **The columns must exist first.** The tool assumes `message_created_at` and
  `assistant_id` are already on `risk_findings` (they ship in a parallel schema
  change); a mutation referencing a missing column fails server-side.
- **Async completion.** The final report counts _submitted_ mutations, not
  applied ones. Validate after `system.mutations` drains (see below).
- **Scope your runs.** Each batch is one mutation; a full-table run at the
  default batch size can queue thousands. That is safe but slow to drain —
  prefer `-org`/`-from`/`-to` scoping, and let `system.mutations` empty out
  between large runs.
- **90-day TTL.** Rows older than the table TTL are evicted on merge; the
  backfill only ever needs to cover the trailing 90 days of findings.

## Validation

After the mutation queue drains (`SELECT count() FROM system.mutations WHERE
table = 'risk_findings' AND NOT is_done` returns 0):

```sql
-- Enriched rows: message time strictly before scan time.
SELECT count() FROM risk_findings WHERE message_created_at < created_at;

-- Remaining defaulted rows in the migrated window (should be only findings
-- without a resolvable message, e.g. content-part anchored ones).
SELECT count() FROM risk_findings
WHERE message_created_at = created_at AND created_at >= '<from>' AND created_at < '<to>';

-- Assistant attribution coverage.
SELECT countIf(assistant_id != ''), count() FROM risk_findings
WHERE created_at >= '<from>' AND created_at < '<to>';
```

Spot-check any id against Postgres:

```sql
-- Postgres
SELECT r.id, cm.created_at AS message_created_at, t.assistant_id
FROM risk_results r
LEFT JOIN chat_messages cm ON cm.id = r.chat_message_id
LEFT JOIN assistant_threads t ON t.chat_id = cm.chat_id AND t.deleted IS FALSE
WHERE r.id = '<id>';
```
