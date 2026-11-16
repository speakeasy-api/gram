# Remote session organization backfill

This offline application-data backfill sets `remote_sessions.organization_id` on sessions written before every writer set it. Sealing upstream OAuth tokens with an organization's own key needs the organization on the row.

A session's organization is its client's organization: the organization of the client's project, otherwise the client's own `organization_id`. The user session issuer on the row is never consulted, because it only records where the grant was first made. Two kinds of session keep a NULL `organization_id`, and their tokens stay on the platform key:

- **Global client**: the client has no project and no organization.
- **Missing organization**: the client names an organization, directly or through its project, that has no `organization_metadata` row. `projects.organization_id` has no foreign key, but `remote_sessions.organization_id` does.

The run covers soft-deleted sessions, which keep their tokens for upstream revocation. It changes only `organization_id`. It never touches `updated_at`, which is the refresh compare-and-swap version and the keepalive due clock. It never overwrites an `organization_id` that is already set. It does not alter the schema or publish events.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database.

## Before running

1. The application change that makes every writer set `organization_id` is deployed. From then on new sessions get the column, and re-consent fills older sessions.
2. No image built before that change is still a rollback target. An older image inserts sessions without `organization_id`. If one was rolled back to, run the backfill again: it is idempotent.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations remote-sessions-organization \
  -environment=staging
```

Counts sessions by category. Writes nothing.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations remote-sessions-organization \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=remote-sessions-organization
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL`. The URL must name a single host and require TLS (`sslmode=require`, `verify-ca` or `verify-full`). The run first finds, in one scan, the organizations that own sessions without an `organization_id`. It then fills each organization's sessions in batches of `-batch-size` (default 500, at most 10000). Each batch commits on its own until none remain, so an interrupted run continues where it stopped when run again.

After applying, run the preview again and confirm `pending=0`. If it is not 0, apply again. Two things can leave sessions behind without any data being wrong:

- Concurrent sign-ins fill sessions themselves. If one fills every row a batch picked, the batch updates nothing, and the run moves on to the next organization early.
- An organization deleted during the run fails it with a foreign key error. Its sessions are deleted with it, so running again completes.

## Output

`mode=… environment=… filled=N pending=N global_client=N missing_organization=N mismatched=N`

- `filled`: sessions this run updated. Always 0 in a preview.
- `pending`: sessions an apply fills. 0 after a successful apply, unless an older image wrote sessions in the meantime.
- `global_client`: sessions under a global client. They stay NULL.
- `missing_organization`: sessions whose client's organization has no metadata row. They stay NULL. Investigate them before finalizing any organization on its own key.
- `mismatched`: sessions whose `organization_id` differs from their client's organization. No writer produces these, and the backfill leaves them alone. Investigate any nonzero count.

Counts never name organizations. Do not record organization IDs in tickets or pull requests.

On failure the command prints how many sessions were filled before the error and exits non-zero.

## Recovery

Every writer sets the same value this backfill does, so there is normally nothing to undo. Nothing reads `organization_id` until sealing with organization keys ships, so clearing it is safe before then. Clearing cannot tell backfilled rows from rows the writers set, so it clears both; run the backfill again to restore them. Clear in batches, repeating until it updates 0 rows, so no statement locks the whole table:

```sql
UPDATE remote_sessions SET organization_id = NULL
WHERE id IN (
  SELECT id FROM remote_sessions WHERE organization_id IS NOT NULL LIMIT 500
);
```

Once sealing reads the column, do not clear it: rows sealed with an organization's key need it to be opened.
