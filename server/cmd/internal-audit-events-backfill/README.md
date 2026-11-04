# Internal audit events backfill

Manual operator command for Speakeasy historical audit classification. It updates
only `is_internal` in `audit_logs` and `security_audit_logs`; it does not call an
audit logger, emit outbox events, change timestamps, or modify delivery history.
Already-sent events and webhooks are **not retracted**. Historical rows remain
customer-visible until classified.

## Deployment order

1. Deploy the schema-only migration adding `is_internal` to both tables.
2. Deploy the application with internal writers and visibility/publication guards.
3. Confirm **all old writer instances have drained** (including workers).
4. Choose a fixed RFC3339 cutoff **after the writer rollout/drain** to cover all
   legacy rows. Rows exactly at or after the cutoff are excluded. Do not use a
   cutoff before the rollout unless intentionally leaving some legacy rows.
5. Run the default dry-run and review both tables' eligible counts.
6. Obtain operational approval and rerun with `--apply`.
7. Verify both `remaining` counts, rerunning with the same cutoff until zero.
   Preserve cutoff and counts as operator evidence in internal systems, not git.

From `server/`, with `GRAM_DATABASE_URL` supplied through your approved secret
mechanism (never commit it or paste it in logs):

```sh
mise exec -- go run ./cmd/internal-audit-events-backfill \
  --before '<RFC3339_CUTOFF>' --batch-size 500 --max-batches 20
# After reviewing deployment state and counts, explicitly add --apply.
```

No database URL is printed, including connection failure diagnostics. The command
accepts no table names or SQL. Eligibility is exactly:

```sql
NOT is_internal AND created_at < $1
AND (subject_type = 'catalog_promotion'
     OR action = 'mcp-server:promote-catalogue')
```

Other `mcp_server` actions are not classified. Already-classified rows are skipped,
so reruns are idempotent. Dry-run only counts; it performs no updates.

## Bounds and recovery

- `--before`: required RFC3339 timestamp with timezone, exclusive.
- `--batch-size`: default 500; accepted range 1–5000.
- `--max-batches`: default 20; positive; limit applies **per table**.
- `--apply`: false by default; commits one short transaction per batch.
- Each batch: 20-second context, 15-second local statement timeout and 2-second
  local lock timeout. Counts have a 30-second context; connection check 10 seconds.
- Each table reports eligible-before, per-batch committed rows, updated rows,
  committed batch count and remaining eligibility. Counts are observations, not a
  cross-table snapshot; avoid concurrent backfills and finish draining old writers.

A successful bounded run may leave rows. **Exit zero does not mean completion**:
check `remaining` for both tables and rerun as needed. Database or output errors
exit nonzero and report the number of previously committed rows. Earlier batches
are not rolled back. A failed commit may have an unknown outcome: inspect counts
and safely rerun rather than assuming the last batch rolled back. Lock/statement
failures require reviewing contention before retrying. Do not increase timeouts
or remove bounds to force a blocked run through.

## Tests

Tests create and destroy a disposable PostgreSQL container and cloned database;
they never use `GRAM_DATABASE_URL`. Run from repository root:

```sh
mise run test:server ./cmd/internal-audit-events-backfill -count=1 -timeout=2m
```

On local OrbStack installations without `/var/run/docker.sock`, prefix the command
with `DOCKER_HOST=$(docker context inspect orbstack --format '{{.Endpoints.docker.Host}}')`.
Never run the command with `--apply` against a shared database for implementation
verification.
