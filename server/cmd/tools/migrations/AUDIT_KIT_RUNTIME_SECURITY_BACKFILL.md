# Kit runtime security audit backfill

This offline application-data backfill moves audit entries about the Kit runtime's own workload admissions from `audit_logs` to `security_audit_logs`. Each Kit runtime admits its workload identity when it starts and withdraws it on teardown. Those entries record machine access, not configuration decisions, so they belong in the security log rather than the configuration history customers read on the audit log page. They stay stored, visible to staff, and readable under Security activity. Entries written after the application change that records them in `security_audit_logs` is deployed are already there; this run moves the ones written before it.

It moves `workload-admission:admit` and `workload-admission:withdraw` rows whose admission the Kit runtime admitted for itself, meaning the same organization has a `workload-admission:admit` row for that subject written by the `system:kit-runtime` actor in `audit_logs`, or any `workload-admission:admit` row for it in `security_audit_logs`. That includes withdrawals attributed to the person who withdrew the issuer. Tags are not used, because customers can set them. Moved rows keep their `id`, `created_at` and every other column, and take new `seq` values in their original order. The run does not alter the schema or publish webhook events.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. The migration adding `security_audit_logs` must already be applied, and the application change should be deployed first so no new entries land in `audit_logs` while the run is in progress.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations audit-kit-runtime-security \
  -environment=staging
```

Counts the rows an apply would move. Writes nothing.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations audit-kit-runtime-security \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=audit-kit-runtime-security
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL`. The URL must name a single host and require TLS (`sslmode=require`, `verify-ca` or `verify-full`). Rows move in batches of `-batch-size` (default 1000): each batch deletes its rows from `audit_logs` and inserts them into `security_audit_logs` in one statement, so a row is never in both tables or neither, and each batch commits on its own until none remain. The run is idempotent: an interrupted run can be restarted and continues with the rows still in `audit_logs`.

## Output

`mode=… environment=… rows=N`

In a dry run `rows` is the number of entries that would move; in an apply it is the number moved. On failure the command prints how many rows were moved before the error and exits non-zero.

## Recovery

Moved rows keep their `id`, so they can be moved back. Entries the application wrote to `security_audit_logs` after deploy match the same filter, so limit the move to rows created before that deploy:

```sql
WITH moved AS (
  DELETE FROM security_audit_logs
  WHERE subject_type = 'workload_admission'
    AND action IN ('workload-admission:admit', 'workload-admission:withdraw')
    AND created_at < '<deploy time of the application change, UTC>'
  RETURNING *
)
INSERT INTO audit_logs (
  id, organization_id, project_id, actor_id, actor_type, actor_display_name,
  actor_slug, action, subject_id, subject_type, subject_display_name,
  subject_slug, before_snapshot, after_snapshot, metadata, acting_surface,
  acting_client_id, created_at
)
SELECT
  id, organization_id, project_id, actor_id, actor_type, actor_display_name,
  actor_slug, action, subject_id, subject_type, subject_display_name,
  subject_slug, before_snapshot, after_snapshot, metadata, acting_surface,
  acting_client_id, created_at
FROM moved
ORDER BY seq;
```

No webhook events are published either way.
