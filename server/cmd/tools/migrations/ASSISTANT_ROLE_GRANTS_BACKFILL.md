# Built-in role assistant grant backfill

Organizations whose built-in Admin and Member grants were seeded before the `assistant:read` / `assistant:write` scopes existed have no stored assistant rows for those roles. The server used to fill them in at load time, which also restored assistant access after an administrator deliberately removed it. `authz.LoadGrants` now returns exactly the stored grants, so this one-time application-data backfill writes the canonical rows instead (Admin: `assistant:read` and `assistant:write`; Member: `assistant:read`; all unrestricted). It does not alter the schema or Atlas migrations.

A built-in role principal (`role:global:<id>`) in an organization is backfilled only when all of the following hold:

- the role has at least one stored grant in the organization (roles with none are seeded in full by `authz.SeedSystemRoleGrants`);
- the role has no stored assistant-kind grant, including `assistant:blocked_*` exceptions;
- no `access_role:update` audit entry for the role has an assistant grant in its before or after snapshot. A role that once held a stored assistant grant and now holds none had it removed deliberately, and stays revoked.

Rows go through `authz.PatchRoleGrantsTx`, one transaction per role principal. Each principal is rechecked under an advisory lock before writing, so rerunning the command (preview or apply) never restores a later revocation.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. Output contains aggregate counts only.

## Rollout

Run the apply against each environment right before or right after deploying the server change that removes the load-time fallback. Until it runs, legacy organizations whose built-in roles have no stored assistant grants have no assistant access through those roles.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations assistant-role-grants \
  -environment=staging
```

Every transaction is rolled back. `roles` and `grants_added` are the prospective counts. Pass `-org=<ORG_ID>` to limit the run to one organization.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations assistant-role-grants \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=assistant-role-grants
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL` by pgx. Stop on any error, investigate, then rerun the same command. A subsequent preview should report `roles=0 grants_added=0`.
