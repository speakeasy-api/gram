# risk_results → risk_findings migration

Back-fills historical **Postgres `risk_results`** rows into the **ClickHouse
`risk_findings`** event log, so old findings sit alongside ones the live writer
ingests going forward.

For the generic pipeline concepts (Source / Transform / Sink, `Criteria`,
lifecycle), see [README.md](./README.md).

## Stages

Implemented in the `riskfindings` package:

| Stage       | Type                       | What it does                                                                                                                                                                                                                 |
| ----------- | -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Source      | `riskfindings.Source`      | Keyset-paginates `risk_results` by `id` (UUIDv7, so id order is time order), joining chat/message/assistant attribution and reading the recorded spans. Optional org / project / policy / time / cursor bounds. Resumable.   |
| Transformer | `riskfindings.Transformer` | Emits one ClickHouse row per recorded span (one row for span-less findings) with per-span HMAC-SHA256 fingerprints, the partial-mask display string (`internal/risk/maskdisplay`), and reveal metadata (surface/field/path). |
| Sink        | `riskfindings.Sink`        | Native `PrepareBatch` + `AppendStruct` into `risk_findings`.                                                                                                                                                                 |

### What gets transformed

**Only clean true positives are migrated.** The source filters `found IS TRUE AND
rule_id IS NOT NULL`, mirroring the live outbox emission
(`findingCreatedPayloads` in `risk_result_writer.go`), so the "nothing found"
`SourceNone` sentinel rows and dead-letter rows — which never reach ClickHouse
through the live path — are excluded, and the backfill cannot inflate risk-event
counts with non-findings.

**False positives are migrated with their mark.** Rows the Presidio
false-positive sweep has classified as noise (reserved IPs, placeholder emails,
…) are written to ClickHouse with their `false_positive_at` timestamp preserved
in the matching Nullable column. Every read query filters
`false_positive_at IS NULL`, so these rows never inflate counts — but the
pre-cutover marks survive the migration, which matters while the live
false-positive mutation path (PR 3) is deferred. When validating row counts
after a run, compare against Postgres **without** a `false_positive_at IS NULL`
filter (or apply it on both sides).

**Findings with recorded spans explode into one ClickHouse row per span.**
Since the CEL custom-rule work (June 2026) every real finding carries a
`risk_results.spans` JSON array (`{match, field, path, start_pos, end_pos}`);
custom-rule findings can carry several spans grouped under one Postgres row.
The transform emits ONE ClickHouse row PER SPAN, each with its own
match-derived columns (offsets, `match_len`, mask, fingerprints) and reveal
metadata:

- Span index 0 keeps the Postgres row id, so webhooks and false-positive marks
  keyed on it keep resolving.
- Span index i ≥ 1 gets the deterministic id
  `uuid.NewSHA1(uuid.NameSpaceURL, "gram:risk:finding:pgspan:<pg_row_id>:<i>")`,
  so a re-run re-emits the same ids instead of minting new ones.
- Row-level state (attribution, exclusion, false-positive mark, category) fans
  out to every span row.

**Validation note:** because of the explode, the ClickHouse row count after a
run EXCEEDS the Postgres row count whenever multi-span custom findings are in
the window. Count Postgres rows as
`SELECT SUM(GREATEST(COALESCE(jsonb_array_length(spans), 1), 1))` over the
migrated window (with the usual `found IS TRUE AND rule_id IS NOT NULL`
filter), not `COUNT(*)`.

The Postgres and ClickHouse shapes differ — this is **not** a column-for-column
copy:

- **The full raw match is never written to ClickHouse.** Only its byte length
  (`match_len`), the partial-mask display string (`match_redacted`), and
  one-way fingerprints are stored; the plaintext stays in Postgres for the
  audited unmask path. The mask comes from the shared
  `internal/risk/maskdisplay` package (replacing the historical
  `<redacted len=N sha=XXXXXXXX>` form, so backfill and live writer can no
  longer diverge): emails show `***@` plus the real domain, financial matches
  show `****` plus the last 4 characters, everything else shows boundary
  characters by length tier (first 4 + last 2 for length ≥ 8, first 2 + last 1
  for 5–7, first 1 + last 1 for 3–4, fully starred below 3). `shadow_mcp`
  matches pass through verbatim (a non-secret server identifier, documented
  carve-out); `prompt_injection`, `llm_judge`, `destructive_tool`, and
  `cli_destructive` store an empty mask since their rationale carries the
  signal.
- **Fingerprints** are computed with the risk pepper keyring: a global
  HMAC-SHA256 (stable across tenants) and a tenant-qualified one under a per-org
  HKDF key, per span. Dead-letter sentinels and empty matches are left
  un-fingerprinted.
- **Reveal metadata.** `surface` records which text `start_pos`/`end_pos`
  index. Spans with field attribution map `path != '' → json_path`,
  `tool.args → tool_args`, `content`/`prompt`/`assistant`/`tool_result →
content`, `tool.server`/`tool.function → derived`; `field` and `path` copy
  over verbatim. Span-less rows (and single spans without field attribution)
  map by source: `gitleaks → scan_surface` (offsets index the composed
  content+args scan surface), `presidio → legacy_presidio` (offsets index a
  YAML-reformatted transform; reveal refuses these fast),
  `prompt_injection`/`llm_judge → none` (matches are rendered JSON artifacts),
  `shadow_mcp`/`account_identity`/`destructive_tool`/`cli_destructive →
derived`, anything else `''`. `tool_call_id` is always left empty: Postgres
  spans only carried the tool NAME, not a recorded call id, and faking one
  would poison reveal lookups.
- **Derived / dropped columns.** `request_id` is not recorded in `risk_results`
  and is left empty; `inserted_at` is stamped by ClickHouse's `DEFAULT now64(9)`.
  `found` has no ClickHouse home and is dropped (only `found IS TRUE` rows
  migrate anyway). Postgres `excluded_exclusion_id` maps to ClickHouse
  `exclusion_id`; `false_positive_at` carries over as-is.
- **Denormalized attribution, event time, and category.** The source LEFT JOINs
  `chat_messages`/`chats` to stamp `chat_id`, `user_id`, and `external_user_id`
  (message-level user ids win over chat-level, everything collapsing to `''`
  when the message or chat is gone), plus `message_created_at`
  (`chat_messages.created_at`, falling back to the finding's own `created_at` —
  the ClickHouse column DEFAULT — when there is no message) and `assistant_id`
  (the chat's most recent live `assistant_threads` link), mirroring the live
  writer's `GetChatMessageAttribution` lookup. Content-part-anchored rows stamp
  `content_part_id` instead of `chat_message_id` and resolve attribution
  through the part, mirroring the live `GetChatContentPartAttribution` lookup:
  chat id from the part's chat, user ids parent-message-first with the same
  guards (part live, part project = finding project, the part's chat in the
  part's own project, the parent message in the part's own chat — any failed
  guard leaves attribution fully empty rather than borrowing a foreign chat's
  ids). Beyond the live part lookup, the backfill also stamps the parent
  message's `created_at` (falling back to the finding's own) and the part
  chat's assistant link, since it has the joins at hand. `category` is
  computed from `(source, rule_id)` via `internal/risk/categories`, same as
  the live writer.

## Flags

Secrets are **never** flag values — they would leak through `argv` / `ps`. The
Postgres URL, ClickHouse password, and fingerprint pepper come from the
environment only (the pepper may also come from a file):

| Secret (env var)                       | Alt                           | Meaning                                           |
| -------------------------------------- | ----------------------------- | ------------------------------------------------- |
| `GRAM_DATABASE_URL`                    | —                             | Postgres connection string (required)             |
| `CLICKHOUSE_PASSWORD`                  | —                             | ClickHouse password                               |
| `GRAM_RISK_FINGERPRINT_PEPPER_KEYRING` | `-pepper-keyring-file <path>` | JSON pepper keyring for fingerprinting (required) |

Non-secret flags:

| Flag                   | Env fallback             | Default                | Meaning                                                                                                                                                                            |
| ---------------------- | ------------------------ | ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `-pepper-keyring-file` | —                        | —                      | Path to a file holding the pepper keyring (alternative to the env var)                                                                                                             |
| `-ch-host`             | `CLICKHOUSE_HOST`        | `localhost`            | ClickHouse host (IPv4, IPv6, or DNS)                                                                                                                                               |
| `-ch-database`         | `CLICKHOUSE_DATABASE`    | `default`              | ClickHouse database                                                                                                                                                                |
| `-ch-username`         | `CLICKHOUSE_USERNAME`    | `gram`                 | ClickHouse username                                                                                                                                                                |
| `-ch-native-port`      | `CLICKHOUSE_NATIVE_PORT` | `9440`                 | ClickHouse native protocol port                                                                                                                                                    |
| `-ch-insecure`         | `CLICKHOUSE_INSECURE`    | `false`                | Skip ClickHouse TLS verification                                                                                                                                                   |
| `-org`                 | —                        | (all)                  | Scope to one `organization_id`                                                                                                                                                     |
| `-project`             | —                        | (all)                  | Scope to one `project_id` (uuid)                                                                                                                                                   |
| `-policy`              | —                        | (all)                  | Scope to one `risk_policy_id` (uuid)                                                                                                                                               |
| `-from`                | —                        | `2026-05-01T00:00:00Z` | Lower time bound, RFC3339 (`created_at >= from`); applies with or without `-cursor`. Defaults to the reveal-metadata re-backfill start; pass `-from ""` to scan from the beginning |
| `-to`                  | —                        | (end)                  | Upper time bound, RFC3339 (`created_at < to`)                                                                                                                                      |
| `-cursor`              | —                        | —                      | Resume after this `risk_results` id (exclusive); keyset resume position only                                                                                                       |
| `-batch-size`          | —                        | `5000`                 | Rows per source page and sink batch                                                                                                                                                |
| `-buffer`              | —                        | `5000`                 | Channel buffer between pipeline stages                                                                                                                                             |
| `-dry-run`             | —                        | `true`                 | When true, read + transform but do not write (and do not connect to ClickHouse)                                                                                                    |

An interrupted run (Ctrl-C / SIGTERM) exits with a **nonzero** status and logs
the `-cursor` to resume from, so shell automation never mistakes a partial
backfill for a completed one.

The pepper keyring JSON has the shape (base64-encoded keys):

```json
{ "current": "v1", "keys": { "v1": "<base64-key>" } }
```

## Examples

Dry run over everything (counts only, no ClickHouse connection):

```bash
GRAM_DATABASE_URL=postgres://gram:gram@127.0.0.1:5439/gram?sslmode=disable \
GRAM_RISK_FINGERPRINT_PEPPER_KEYRING='{"current":"v1","keys":{"v1":"<base64>"}}' \
  go run ./server/cmd/tools/migrations -dry-run=true
```

Apply, scoped to one org and time window:

```bash
go run ./server/cmd/tools/migrations \
  -org org_123 -from 2024-01-01T00:00:00Z -to 2024-06-01T00:00:00Z \
  -dry-run=false
```

Resume an interrupted run from the last printed cursor:

```bash
go run ./server/cmd/tools/migrations -cursor 019f65f6-ed75-7186-84a5-7ed095aab7b3 -dry-run=false
```

### Running against production

Reach production Postgres and ClickHouse through their respective auth
proxies / tunnels, then point the flags at `127.0.0.1`:

```bash
cloud-sql-proxy --port 5432 <instance-connection-name> &
# open a ClickHouse tunnel on 9440

GRAM_DATABASE_URL=postgres://USER:PASS@127.0.0.1:5432/gram \
CLICKHOUSE_PASSWORD="$CH_PASS" \
  go run ./server/cmd/tools/migrations \
  -pepper-keyring-file ./pepper.json \
  -ch-host 127.0.0.1 -ch-database gram -ch-username gram \
  -from 2024-01-01T00:00:00Z -to 2025-01-01T00:00:00Z \
  -dry-run=false
```

Use the **same pepper keyring as production** so back-filled fingerprints match
the ones the live writer produces; a different keyring yields fingerprints that
will not join.

## Re-backfill runbook (reveal metadata)

The reveal-metadata rework changed what every row carries (`surface`, `field`,
`path`, `tool_call_id`, `content_part_id`, `message_created_at`,
`assistant_id`, the partial-mask `match_redacted`, and the span explode), so
the operator run that adopts it REPLACES the table contents instead of
appending:

1. Pause/quiesce the live ClickHouse finding writer (drain the outbox) so no
   insert races the reload.
2. **MANUAL STEP — run by hand, the tool never executes this:**

   ```sql
   TRUNCATE TABLE risk_findings;
   ```

   > **Warning:** from this moment until the backfill completes, every
   > ClickHouse-served read (Risk Events listing, overview rollups) is EMPTY or
   > partial. Schedule accordingly and treat the truncate + reload as one
   > maintenance window.

3. Run the backfill with `-dry-run=false`. The default `-from`
   (`2026-05-01T00:00:00Z`) is the agreed re-backfill start; rows older than
   that are outside the table's 90-day TTL horizon anyway.
4. Validate row counts using the span-aware Postgres count from the validation
   note above (plain `COUNT(*)` undercounts multi-span custom findings).
5. Resume the live writer.

## Safety and caveats

- **Dry run by default.** Nothing is written unless you pass `-dry-run=false`.
  A dry run reports no resume cursor — nothing was durably written, so there is
  no checkpoint to resume the real migration from.
- **Resumable, from the committed cursor.** On an applied run the final report's
  `last cursor` is the **sink's** last durably-written id, not the source's read
  position (which runs ahead). Resume an interrupted run by passing that value to
  `-cursor`, and **repeat the original `-org`, `-project`, `-policy`, `-from`, and
  `-to` filters** — the cursor does not encode query scope. The cursor is only the
  keyset resume position (`id > cursor`); it does not relax the time window, so
  `-from`/`-to` still bound the resumed run and keep it from importing
  out-of-window rows. Rows that were read but not yet flushed on interruption are
  re-read, never skipped.
- **Exact time bounds.** `-from`/`-to` filter on `created_at` directly, to full
  precision. The `id` keyset is used only for pagination and resume, never to
  prune the time window: a row's uuidv7 `id` and its `created_at` are minted at
  slightly different instants, so the id timestamp is not a sound bound for a
  `created_at` filter.
- **No full plaintext in ClickHouse.** Only length, the partial-mask display
  string (boundary characters per the `maskdisplay` tiers — a deliberate,
  signed-off relaxation of the earlier no-plaintext rule; `shadow_mcp` server
  identifiers verbatim as a documented carve-out), and one-way fingerprints are
  written. The full raw match stays in Postgres for the audited unmask path.
- **Idempotency depends on the engine.** Each batch carries a deterministic
  `insert_deduplication_token` hashed over the full ordered set of its row ids
  (not just the endpoints, which could collide between batches with the same
  first/last id but different interiors). On a _Replicated_ MergeTree this dedups
  a genuinely identical re-inserted batch; on a plain `MergeTree` the token is
  ignored, so a re-run inserts duplicates. When re-running, resume from `-cursor`
  so any overlap is bounded to the single in-flight batch.
- **Do not overlap the live writer.** Production `risk_findings` is a plain
  `MergeTree`, so the dedup token is inert there: any row inserted by both the
  backfill and the live writer is duplicated. A `-to` cutoff is **necessary but
  not sufficient** — the live path emits through the async outbox, so a finding
  created before `-to` can still be delivered (or retried) into ClickHouse _after_
  the backfill runs, and the live event and the backfill row carry independent
  inserts of the same `id`. Before relying on the boundary you must also make the
  handoff atomic on the live side: **quiesce/drain the live outbox** (or pause the
  live ClickHouse writer) so no pre-`-to` finding is still in flight, then run the
  backfill up to that drained watermark, then resume live ingestion. Equivalently,
  agree a shared cutover checkpoint (e.g. a `created_at`/`id` watermark) that the
  backfill's `-to` and the live writer's start both key off. Do not treat wall-clock
  `-to` alone as the boundary.
- **Partition limit.** `risk_findings` is `PARTITION BY toYYYYMMDD(created_at)`.
  A backfill batch can span more than the default 100 partitions when historical
  data is sparse, so the sink sets `max_partitions_per_insert_block = 0` for its
  inserts.
- **90-day TTL.** `risk_findings` has `TTL created_at + INTERVAL 90 DAY`. Rows
  older than that are evicted on merge, so the post-backfill row count can be
  lower than the number inserted — that is the table's retention, not data loss
  in the tool.
