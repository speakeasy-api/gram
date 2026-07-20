# risk_results → risk_findings migration

Back-fills historical **Postgres `risk_results`** rows into the **ClickHouse
`risk_findings`** event log, so old findings sit alongside ones the live writer
ingests going forward.

For the generic pipeline concepts (Source / Transform / Sink, `Criteria`,
lifecycle), see [README.md](./README.md).

## Stages

Implemented in the `riskfindings` package:

| Stage       | Type                       | What it does                                                                                                                                                                |
| ----------- | -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Source      | `riskfindings.Source`      | Keyset-paginates `risk_results` by `id` (UUIDv7, so id order is time order). Optional org / project / policy / time / cursor bounds. Resumable.                             |
| Transformer | `riskfindings.Transformer` | Computes the global + tenant-qualified HMAC-SHA256 fingerprints of the match and a redacted display string, mirroring the live ingest path (`internal/risk/finding_bq.go`). |
| Sink        | `riskfindings.Sink`        | Native `PrepareBatch` + `AppendStruct` into `risk_findings`.                                                                                                                |

### What gets transformed

**Only real findings are migrated.** The source filters `found IS TRUE AND
rule_id IS NOT NULL`, mirroring the live outbox emission
(`findingCreatedPayloads` in `risk_result_writer.go`). The "nothing found"
`SourceNone` sentinel rows and dead-letter rows — which never reach ClickHouse
through the live path — are excluded, so the backfill cannot inflate risk-event
counts with non-findings.

The Postgres and ClickHouse shapes differ — this is **not** a column-for-column
copy:

- **The raw match is never written to ClickHouse.** Only its byte length
  (`match_len`), a redacted display string (`match_redacted`, always
  `<redacted len=N sha=XXXXXXXX>`), and one-way fingerprints are stored. The
  plaintext stays in Postgres for the audited unmask path. Every source is
  redacted, including `shadow_mcp` and `account_identity`.
- **Fingerprints** are computed with the risk pepper keyring: a global
  HMAC-SHA256 (stable across tenants) and a tenant-qualified one under a per-org
  HKDF key. Dead-letter sentinels and empty matches are left un-fingerprinted.
- **Derived / dropped columns.** `request_id` is not recorded in `risk_results`
  and is left empty; `inserted_at` is stamped by ClickHouse's `DEFAULT now64(9)`.
  Postgres-only columns without a ClickHouse home (`found`, `spans`,
  `false_positive_*`) are dropped. Postgres `excluded_exclusion_id` maps to
  ClickHouse `exclusion_id`.

## Flags

| Flag              | Env fallback                           | Default     | Meaning                                                                         |
| ----------------- | -------------------------------------- | ----------- | ------------------------------------------------------------------------------- |
| `-db`             | `GRAM_DATABASE_URL`                    | —           | Postgres connection string (required)                                           |
| `-pepper-keyring` | `GRAM_RISK_FINGERPRINT_PEPPER_KEYRING` | —           | JSON pepper keyring for fingerprinting (required)                               |
| `-ch-host`        | `CLICKHOUSE_HOST`                      | `localhost` | ClickHouse host                                                                 |
| `-ch-database`    | `CLICKHOUSE_DATABASE`                  | `default`   | ClickHouse database                                                             |
| `-ch-username`    | `CLICKHOUSE_USERNAME`                  | `gram`      | ClickHouse username                                                             |
| `-ch-password`    | `CLICKHOUSE_PASSWORD`                  | `gram`      | ClickHouse password                                                             |
| `-ch-native-port` | `CLICKHOUSE_NATIVE_PORT`               | `9440`      | ClickHouse native protocol port                                                 |
| `-ch-insecure`    | `CLICKHOUSE_INSECURE`                  | `false`     | Skip ClickHouse TLS verification                                                |
| `-org`            | —                                      | (all)       | Scope to one `organization_id`                                                  |
| `-project`        | —                                      | (all)       | Scope to one `project_id` (uuid)                                                |
| `-policy`         | —                                      | (all)       | Scope to one `risk_policy_id` (uuid)                                            |
| `-from`           | —                                      | (beginning) | Lower time bound, RFC3339                                                       |
| `-to`             | —                                      | (end)       | Upper time bound, RFC3339                                                       |
| `-cursor`         | —                                      | —           | Resume after this `risk_results` id (exclusive); overrides `-from`              |
| `-batch-size`     | —                                      | `5000`      | Rows per source page and sink batch                                             |
| `-buffer`         | —                                      | `5000`      | Channel buffer between pipeline stages                                          |
| `-dry-run`        | —                                      | `true`      | When true, read + transform but do not write (and do not connect to ClickHouse) |

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
GRAM_RISK_FINGERPRINT_PEPPER_KEYRING="$(< pepper.json)" \
  go run ./server/cmd/tools/migrations \
  -ch-host 127.0.0.1 -ch-database gram -ch-username gram -ch-password "$CH_PASS" \
  -from 2024-01-01T00:00:00Z -to 2025-01-01T00:00:00Z \
  -dry-run=false
```

Use the **same pepper keyring as production** so back-filled fingerprints match
the ones the live writer produces; a different keyring yields fingerprints that
will not join.

## Safety and caveats

- **Dry run by default.** Nothing is written unless you pass `-dry-run=false`.
- **Resumable, from the committed cursor.** The final report's `last cursor` is
  the **sink's** last durably-written id, not the source's read position (which
  runs ahead). Resume an interrupted run by passing that value to `-cursor`; rows
  that were read but not yet flushed on interruption are re-read, never skipped.
- **Exact time bounds.** `-from`/`-to` are honored to full precision: the id
  keyset window is only a coarse (millisecond) prune, and exact `created_at`
  predicates enforce the requested sub-millisecond boundaries.
- **No plaintext in ClickHouse.** Only length, redacted string, and fingerprints
  are written.
- **Idempotency depends on the engine.** Each batch carries a deterministic
  `insert_deduplication_token`. On a _Replicated_ MergeTree this dedups an
  identical re-inserted block; on a plain `MergeTree` the token is ignored, so a
  re-run inserts duplicates. When re-running, resume from `-cursor` so any
  overlap is bounded to the single in-flight batch.
- **Partition limit.** `risk_findings` is `PARTITION BY toYYYYMMDD(created_at)`.
  A backfill batch can span more than the default 100 partitions when historical
  data is sparse, so the sink sets `max_partitions_per_insert_block = 0` for its
  inserts.
- **90-day TTL.** `risk_findings` has `TTL created_at + INTERVAL 90 DAY`. Rows
  older than that are evicted on merge, so the post-backfill row count can be
  lower than the number inserted — that is the table's retention, not data loss
  in the tool.
