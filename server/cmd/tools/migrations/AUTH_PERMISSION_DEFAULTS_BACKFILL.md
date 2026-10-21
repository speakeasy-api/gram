# Default auth permission backfill

This offline application-data backfill grants every organization the default auth permissions the mandatory auth client check (`mcp:auth`) needs, so enforcing it later removes no access people have today. It runs the grants phase of the auth client backfill (`authclientbackfill.RunGrants`) for each organization in turn; `docs/rbac.md` ("Default auth permissions") describes what it grants. It does not alter the schema.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. Output contains aggregate counts only.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations auth-permission-defaults \
  -environment=staging
```

Each organization's grants phase runs in a transaction that is always rolled back, taking no lock. The kind counts are the grants an apply would add. Only auth clients already recorded are counted, so run the upstream OAuth migration first where it applies.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations auth-permission-defaults \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=auth-permission-defaults
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL`. Each organization is applied in its own transaction under the auth client backfill's organization lock; an organization another run holds is skipped and counted as `busy`. The run is idempotent and only adds grants. Every grant it writes is recorded in `auth_permission_provisions`, and a grant recorded there is never written again, so a rerun does not restore one an admin removed.

## Output

`mode=… environment=… organizations=N personal_default=N shared_client=N api_key=N written=N keys_waiting=N busy=N failed=N`

- `keys_waiting`: organizations whose legacy API key policies were held back because some auth clients are not recorded yet. Run the upstream OAuth migration for them, then rerun.
- `failed`: organizations whose grants phase failed and rolled back. Rerun; a persistent failure needs investigation.

After applying, a preview should report zero kind counts apart from organizations still waiting or busy.

## Rollback

Delete the provisioned grants that are still live and that no admin took over, then the ledger rows, per organization or for all:

```sql
DELETE FROM principal_grants g
USING auth_permission_provisions p
WHERE g.organization_id = p.organization_id
  AND g.principal_urn = p.principal_urn
  AND g.scope = p.scope
  AND g.selectors = p.selectors
  AND p.taken_over_at IS NULL;
DELETE FROM auth_permission_provisions;
```

A grant an admin wrote the same way afterwards has `taken_over_at` set and survives.
