# migrations

An offline operator tool for one-off data changes. Each is a subcommand of one
binary and comes in one of two shapes.

## Pipeline migrations

These move or rewrite data between stores through the generic, reusable
**Source → Transform → Sink** pipeline described under [Concepts](#concepts).
Each is a set of three small implementations wired together by the shared
harness.

| Migration                                                                           | Doc                                                                |
| ----------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| Postgres `risk_results` → ClickHouse `risk_findings`                                | [RISK_RESULTS_MIGRATION.md](./RISK_RESULTS_MIGRATION.md)           |
| `risk_findings` `message_created_at`/`assistant_id` column backfill (via mutations) | [RISKFINDINGS_COLS_MIGRATION.md](./RISKFINDINGS_COLS_MIGRATION.md) |

## Application-data backfills

These fix up PostgreSQL application data in place. They do not use the pipeline:
each runs its own queries and transactions, defaults to a dry run, and writes
only with `-apply` (or its own explicit write flag) plus confirmation of the
target environment and database. Its guide documents the flags, output, and
recovery.

| Backfill                                                                    | Doc                                                                                    |
| --------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| Pro organization enterprise-access entitlements                             | [PRO_ENTITLEMENTS_BACKFILL.md](./PRO_ENTITLEMENTS_BACKFILL.md)                         |
| OpenRouter key disable causes for legacy rows                               | [OPENROUTER_DISABLE_CAUSES_MIGRATION.md](./OPENROUTER_DISABLE_CAUSES_MIGRATION.md)     |
| Default auth permissions (`mcp:auth` defaults and legacy API key policies)  | [AUTH_PERMISSION_DEFAULTS_BACKFILL.md](./AUTH_PERMISSION_DEFAULTS_BACKFILL.md)         |
| Stored assistant grants for legacy built-in Admin and Member roles          | [ASSISTANT_ROLE_GRANTS_BACKFILL.md](./ASSISTANT_ROLE_GRANTS_BACKFILL.md)               |
| Kit runtime workload admission audit entries moved to `security_audit_logs` | [AUDIT_KIT_RUNTIME_SECURITY_BACKFILL.md](./AUDIT_KIT_RUNTIME_SECURITY_BACKFILL.md)     |
| `remote_sessions.organization_id` from each session's client                | [REMOTE_SESSIONS_ORGANIZATION_BACKFILL.md](./REMOTE_SESSIONS_ORGANIZATION_BACKFILL.md) |

The rest of this document covers the pipeline.

## Concepts

```
Source[A]  --srcCh-->  Transformer[A,B]  --sink.Input()-->  Sink[B]
```

The three stages run concurrently (`pipeline.Run`):

- **Source** scans an origin page-by-page and publishes records to a channel. It
  owns its own checkpoint bookkeeping so a run is resumable.
- **Transformer** maps each source record to zero or more sink records (returning
  an empty slice drops the record; returning several fans one record out).
- **Sink** owns a buffered input channel (`Input()`), batches whatever it drains,
  and writes each batch to the destination.

The stages are generic interfaces in `pipeline/pipeline.go`:

```go
type Criteria map[string]any

type Source[T any] interface {
	Read(ctx context.Context, criteria Criteria, out chan<- T) error
}

type Transformer[A, B any] interface {
	Transform(ctx context.Context, in A) ([]B, error)
}

type Sink[T any] interface {
	Input() chan<- T
	Run(ctx context.Context) error
}
```

### Wiring and lifecycle

`pipeline.Run` connects the stages with an `errgroup` and two channels:

- The **source** publishes to `srcCh`. It does **not** close `srcCh` — `Run`
  owns that channel and closes it after `Source.Read` returns, so a `Read`
  implementation must never close its `out` argument (doing so double-closes and
  panics).
- The **transform stage** is the sole producer to the sink's channel; it closes
  that channel once `srcCh` is drained, so the sink's `Run` finishes and flushes
  its final partial batch.
- The first stage to error cancels the shared context so the others unwind
  promptly, and `Run` returns that error.

Because records stay in order end-to-end (a single transform goroutine over FIFO
channels), a resumable migration should take its **commit cursor from the sink**
— the id of the last durably-written record — not from the source's read
position, which runs ahead of what has actually been persisted.

```go
err := pipeline.Run[SourceRow, DestRow](ctx, source, transformer, sink, criteria, bufferSize)
```

### Criteria (query bounds)

`Criteria` is a free-form `map[string]any` of query bounds — time range, cursor,
tenant, page size, and so on. Source implementations type-assert only the keys
they understand and ignore the rest, so a new source can define its own bounds
without changing the harness. Publish typed keys as their real Go types
(`time.Time`, `uuid.UUID`, `int`, ...); the source asserts them back.

## Usage

Each migration is invoked through the single `migrations` binary, selected by
an optional leading subcommand (omitting it runs `riskfindings`). The flags,
environment variables, and examples are migration-specific — see the linked doc
for the migration you want to run. The general shape is:

```
go run ./server/cmd/tools/migrations [migration] [flags]
```

`-dry-run` defaults to **true** for every pipeline migration: a plain run reads
and transforms but writes nothing. Pass `-dry-run=false` to write.

## Adding a new migration

1. Implement `pipeline.Source[A]`, `pipeline.Transformer[A,B]`, and
   `pipeline.Sink[B]` for your origin, mapping, and destination. Put them in their
   own package under `server/cmd/tools/migrations/`.
2. In the Source, read query bounds from the `pipeline.Criteria` map, asserting
   only the keys it understands. Track and expose the last cursor so runs resume.
3. In the Sink, accumulate from `Input()` into batches and flush on a size
   threshold and on channel close.
4. Wire the three stages with `pipeline.Run` (add flags / a subcommand as needed).
5. Add a `<NAME>_MIGRATION.md` documenting the source/target schema, flags, and any
   caveats, and link it from the table above.

### Conventions worth copying

- **Keyset pagination** over a time-ordered key (e.g. a UUIDv7 `id`) makes the
  source cheap and resumable; print the last cursor each page.
- **Dry run by default** so a plain invocation is always safe.
- **Batch the sink** and make re-runs as idempotent as the destination allows.
