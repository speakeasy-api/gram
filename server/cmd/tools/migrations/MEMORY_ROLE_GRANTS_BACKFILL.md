# Built-in Admin role memory grant backfill

New organizations get `memory:read` and `memory:write` on the built-in Admin role when `authz.SeedSystemRoleGrants` provisions them. That seed runs once per organization and skips roles that already hold grants, so organizations provisioned before the memory scopes existed have no stored memory rows for their Admin role, and their administrators cannot read or curate Organizational Memory. This one-time application-data backfill writes the canonical Admin rows (`memory:read` and `memory:write`, unrestricted). The built-in Member role holds no memory grant by default and is never touched. It does not alter the schema or Atlas migrations.

A built-in Admin role principal (`role:global:<id>`) in an organization is backfilled only when all of the following hold:

- the role has at least one stored permanent grant in the organization (roles with none are seeded in full by `authz.SeedSystemRoleGrants`);
- the role has no stored permanent `memory:*` grant (temporary grants are ignored);
- no `access_role:update` audit entry for the role has a memory grant in its before or after snapshot. A role that once held a stored memory grant and now holds none had it removed deliberately, and stays revoked.

Rows go through `authz.PatchRoleGrantsTx`, one transaction per role principal. Each principal is rechecked before writing under the same per-role lock that role edits take, so neither a concurrent revocation nor rerunning the command (preview or apply) can restore revoked access.

Granting the scopes does not turn Organizational Memory on. It stays gated per organization by the staff admin toggle, so organizations with it off see no change.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. Output contains aggregate counts only.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations memory-role-grants \
  -environment=staging
```

Every transaction is rolled back. `roles` and `grants_added` are the prospective counts. Pass `-org=<ORG_ID>` to limit the run to one organization.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations memory-role-grants \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=memory-role-grants
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL` by pgx. Stop on any error, investigate, then rerun the same command. A subsequent preview should report `roles=0 grants_added=0`.
