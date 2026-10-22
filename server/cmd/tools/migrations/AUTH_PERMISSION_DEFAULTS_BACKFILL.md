# Default auth permission backfill

This offline application-data backfill grants every organization the default auth permissions the mandatory auth client check (`mcp:auth`) needs, so enforcing it later removes no access people have today. It runs the grants phase of the auth client backfill (`authclientbackfill.RunGrants`) for each organization in turn; `docs/rbac.md` ("Default auth permissions") describes what it grants. It does not alter the schema.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. Output contains aggregate counts, plus ids for agent attachments that need remediation.

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

`mode=… environment=… organizations=N personal_default=N shared_client=N api_key=N attachment=N written=N keys_waiting=N attachments_to_remediate=N busy=N failed=N`

Before it, one line per agent attachment that was not carried over in an organization that completed. Failed and busy organizations print none until they are rerun:

`remediate organization=… binding=… agent=… reason=…`

- `attachment`: grants that let an attached agent keep using its owner's connection without the attachment: `mcp:auth` on that owner's connection through the attached upstream OAuth client.
- `attachments_to_remediate`: attachments the backfill did not carry over. The owner must reconnect or configure the agent; the backfill never substitutes another user or a broader grant. Reasons: `not_attached_by_user` (no user made the attachment), `owner_changed` (the attacher no longer owns the agent), `foreign_connection` (the connection is not the attacher's), `agent_inactive` (the agent is suspended, revoked or waiting for a new owner, or its owner is no longer an active member), `connection_gone` (the connection was removed or authorized again), `configuration_gone` (the attachment's sign-in, project, upstream client or issuer was removed), `client_unrecorded` (run the upstream OAuth migration for the organization, then rerun).
- `keys_waiting`: organizations whose legacy API key policies were held back because some auth clients are not recorded yet. Run the upstream OAuth migration for them, then rerun.
- `failed`: organizations whose grants phase failed and rolled back. Rerun; a persistent failure needs investigation.
- `busy`: organizations another run held, which were skipped. Rerun.

The command exits non-zero when any organization failed or was busy.

After applying, a preview should report zero kind counts apart from organizations still waiting or busy.

## Rollback

A rollback deletes the provisioned grants that are still live, then their ledger rows. It leaves:

- personal defaults written when an organization was created (`kind = 'creation_default'`), not by this backfill,
- grants an admin took over by writing them (`taken_over_at` set), and
- every grant of an API key whose provisioned policy an admin took over in part. Removing the rest would leave the key a narrower policy than it had before the backfill.

Run it in a transaction, preview first, and scope it to one organization unless you mean all of them. After a rollback the ledger is gone, so a later run provisions again, including defaults admins had removed.

```sql
BEGIN;

-- Preview: grants that would be deleted.
SELECT count(*)
FROM principal_grants g
JOIN auth_permission_provisions p
  ON g.organization_id = p.organization_id
 AND g.principal_urn = p.principal_urn
 AND g.scope = p.scope
 AND g.selectors = p.selectors
WHERE p.taken_over_at IS NULL
  AND p.kind <> 'creation_default'
  AND p.organization_id = '<ORG_ID>'
  AND NOT EXISTS (
    SELECT 1 FROM auth_permission_provisions t
    WHERE t.organization_id = p.organization_id
      AND t.principal_urn = p.principal_urn
      AND t.principal_urn LIKE 'api_key:%'
      AND t.taken_over_at IS NOT NULL
  );

DELETE FROM principal_grants g
USING auth_permission_provisions p
WHERE g.organization_id = p.organization_id
  AND g.principal_urn = p.principal_urn
  AND g.scope = p.scope
  AND g.selectors = p.selectors
  AND p.taken_over_at IS NULL
  AND p.kind <> 'creation_default'
  AND p.organization_id = '<ORG_ID>'
  AND NOT EXISTS (
    SELECT 1 FROM auth_permission_provisions t
    WHERE t.organization_id = p.organization_id
      AND t.principal_urn = p.principal_urn
      AND t.principal_urn LIKE 'api_key:%'
      AND t.taken_over_at IS NOT NULL
  );

DELETE FROM auth_permission_provisions
WHERE organization_id = '<ORG_ID>'
  AND kind <> 'creation_default';

COMMIT;
```

For every organization, drop the three `organization_id = '<ORG_ID>'` conditions together; never scope one statement without the others. One statement over every organization can deadlock with concurrent grant writes; if Postgres aborts it, roll back and run it again, or run it one organization at a time.

## Deploy and revert order

The migration adding `auth_permission_provisions` must be deployed before the server code that writes it: every explicit grant write now marks provisioned grants taken over. Revert that code before ever reverting the migration.

## Known gap

Nothing edits a key's policy after creation today. If that changes, an edit landing while the backfill tops the same policy up can interleave with it; a later run then leaves the policy alone.
