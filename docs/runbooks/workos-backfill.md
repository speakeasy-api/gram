# WorkOS Backfill

Use this runbook to run the local WorkOS backfill script against local, dev, or
prod databases. The script syncs WorkOS snapshot state into Gram for:

- global roles
- organization metadata
- organization roles
- users
- organization memberships
- organization role assignments

The entrypoint is `server/cmd/workos-backfill`.

## Prerequisites

Set a WorkOS API key before running the script:

```sh
export WORKOS_API_KEY='sk_test_...'
```

For prod, use the live WorkOS key and add `--environment=prod`. For local and
dev, the real WorkOS endpoint requires a test key that starts with `sk_test_`.

For local database access, set `GRAM_DATABASE_URL` or pass `--database-url`.
For dev and prod Cloud SQL access, run the script with `--cloudsql-proxy`;
that mode ignores `GRAM_DATABASE_URL` and builds a local proxy URL.

If you need to point at a non-standard WorkOS API endpoint, set
`WORKOS_API_URL` or pass `--workos-endpoint`.

Before using Cloud SQL, grant your IAM database user the right permissions from
the `gram-infra` repo:

```sh
cd ~/github.com/speakeasy-api/gram-infra
mise gcp:db:master dev
```

Choose `READ` for preflight/validate and `ALL` before writes. The backfill
command starts the Cloud SQL proxy and connects as your active `gcloud` account;
it does not create the IAM database user or grant privileges.

## Commands

Run preflight first. This is read-only and is the default phase:

```sh
mise backfill:workos
```

Equivalent explicit command:

```sh
mise backfill:workos --phase=preflight --environment=dev --cloudsql-proxy
```

Limit the scope while testing:

```sh
mise backfill:workos --phase=preflight --environment=dev --cloudsql-proxy --limit=5
```

Run a specific WorkOS organization:

```sh
mise backfill:workos --phase=preflight --environment=dev --cloudsql-proxy --workos-org-id=org_...
```

Multiple organizations can be repeated or comma-separated:

```sh
mise backfill:workos --phase=preflight --environment=dev --cloudsql-proxy --workos-org-id=org_1,org_2
```

Run global roles only:

```sh
mise backfill:workos --phase=global-roles --environment=dev --cloudsql-proxy --dry-run=false
```

Run organizations, users, memberships, and assignments:

```sh
mise backfill:workos --phase=organizations --environment=dev --cloudsql-proxy --dry-run=false
```

Run everything:

```sh
mise backfill:workos --phase=all --environment=dev --cloudsql-proxy --dry-run=false
```

Validate after writes:

```sh
mise backfill:workos --phase=validate --environment=dev --cloudsql-proxy
```

For prod, the command requires explicit prod confirmation:

```sh
mise backfill:workos --phase=all --environment=prod --cloudsql-proxy --dry-run=false --confirm-prod=prod
```

Non-prod writes prompt for `backfill` unless `--auto-approve` is passed. Prod
writes never skip the prod confirmation.

## Useful Flags

- `--phase`: `preflight`, `global-roles`, `organizations`, `validate`, or `all`.
- `--environment`: `local`, `dev`, or `prod`.
- `--cloudsql-proxy`: start a local Cloud SQL proxy for dev/prod DB access.
- `--cloudsql-port`: local proxy port; defaults to a free port.
- `--cloudsql-db-name`: database name; defaults to `gram`.
- `--dry-run`: defaults to `true`; set `--dry-run=false` to write.
- `--workos-org-id`: process selected WorkOS organizations only.
- `--limit`: cap the number of WorkOS organizations inspected.
- `--breakpoint-before-write`: pause after preflight and before DB writes.
- `--pause-after-each`: pause after each organization backfill.
- `--auto-approve`: skip the non-prod `backfill` prompt.
- `--confirm-prod=prod`: required for non-interactive prod access.

## Interpreting Output

The script prints a preflight plan before it writes.

```text
Global role preflight:
  workos_global_roles: 3
  role_rows: affected=0 create=0 update=0 delete=0 noop=3 stale_skip=0
```

`affected` means rows that would mutate the database. It is
`create + update + delete`.

```text
Organization preflight:
  workos_orgs: 119
  expected_organization_roles: 39
  expected_users: 101
  expected_memberships: 101
  skipped_unlinked_without_external_id: 4
  organization_rows: affected=115 create=115 update=0 delete=0 noop=0 stale_skip=4
  role_rows: affected=37 create=37 update=0 delete=0 noop=0 stale_skip=0
  user_rows: affected=87 create=87 update=0 delete=0 noop=0 stale_skip=10
  membership_rows: affected=87 create=87 update=0 delete=0 noop=0 stale_skip=10
  assignment_rows: affected=87 create=87 update=0 delete=0 noop=0 stale_skip=10
```

Row states:

- `create`: the local row does not exist and will be inserted.
- `update`: the local row exists and at least one synced field will change.
- `delete`: the local row is absent from the WorkOS snapshot and will be soft-deleted.
- `noop`: the local row already matches the WorkOS snapshot.
- `stale_skip`: the local row has newer synced WorkOS state, or the script cannot safely resolve the local row.

The sample section shows representative organizations and the dominant row
state for each entity type:

```text
sample:
  org_... -> gram_org_id org=create:1 roles=noop:0 users=create:2 memberships=create:2 assignments=create:2 name="Example"
```

When updates or deletes are planned, the script prints a capped
`planned_change_details` section with field-level changes:

```text
planned_change_details: showing=1 total=1
  update user user_123
    email: "old@example.com" -> "new@example.com"
```

After writes, the completion report includes both organization-level progress
and row outcomes for the successfully written and validated organizations:

```text
Organization backfill complete.
  scanned: 119
  written: 115
  validated: 115
  skipped: 4
  skipped_noop: 0
  failed: 0
  validation_failures: 0
  organization_rows: affected=115 create=115 update=0 delete=0 noop=0 stale_skip=0
  role_rows: affected=37 create=37 update=0 delete=0 noop=0 stale_skip=0
  user_rows: affected=87 create=87 update=0 delete=0 noop=0 stale_skip=0
  membership_rows: affected=87 create=87 update=0 delete=0 noop=0 stale_skip=0
  assignment_rows: affected=87 create=87 update=0 delete=0 noop=0 stale_skip=0
```

`written` counts organizations whose write transaction completed and whose
validation passed. It is not a row count.

## How It Works

Preflight loads the WorkOS snapshot first. For each selected organization, it
fetches the WorkOS organization, roles, users, and memberships. It then compares
that snapshot to the current database and classifies expected row changes.

Writes are split by phase:

- `global-roles` syncs global WorkOS roles.
- `organizations` syncs organization metadata, organization roles, users,
  memberships, and organization role assignments.
- `all` runs both phases.

Each organization write runs inside a database transaction. The write path:

1. Resolves the local organization by `workos_id`, or by WorkOS `external_id`
   when no local `workos_id` row exists.
2. Skips unlinked WorkOS organizations with no `external_id`.
3. Upserts organization metadata and preserves an existing slug. New slugs use
   Gram's normal unique organization slug generation.
4. Upserts organization roles and soft-deletes roles missing from the WorkOS
   snapshot.
5. Resolves each WorkOS user by existing local `workos_id`, then by WorkOS
   `external_id`.
6. Skips users that cannot be resolved to a local Gram user ID.
7. Upserts users, memberships, and role assignments for resolved users.
8. Commits the transaction and validates the expected rows.

When `--cloudsql-proxy` is set, the script derives the Cloud SQL instance from
`--environment`, starts `cloud-sql-proxy` with `--auto-iam-authn`, picks a free
local port unless `--cloudsql-port` is provided, and connects to `127.0.0.1` as
the active `gcloud` account.

The script sets `lock_timeout=5s` and `statement_timeout=5min` for DB sessions.
Preflight, validate, and dry-run sessions are read-only.

## Debugging

Use `--breakpoint-before-write` to pause after preflight and before writes:

```sh
mise backfill:workos --phase=organizations --environment=dev --cloudsql-proxy --dry-run=false --breakpoint-before-write
```

Use `--pause-after-each` when stepping through a small batch:

```sh
mise backfill:workos --phase=organizations --environment=dev --cloudsql-proxy --dry-run=false --limit=1 --pause-after-each
```

For VSCode, launch `server/cmd/workos-backfill` as a Go program and pass the
same args. Useful breakpoints:

- `runOrganizationBackfill` in `server/cmd/workos-backfill/main.go`, on the
  `backfill.Do(...)` call, before an organization transaction starts.
- `BackfillWorkOSOrganization.Do` in `server/cmd/workos-backfill/backfill.go`,
  where the WorkOS snapshot is fetched and the transaction begins.
- `backfillWorkOSUser` in `server/cmd/workos-backfill/backfill_user.go`, when
  debugging skipped users or user ID resolution.

When stopped before an organization write, inspect:

- `org.workosOrgID`
- `org.gramOrgID`
- `org.orgChanges`
- `org.roleChanges`
- `org.userChanges`
- `org.membershipChanges`
- `org.assignmentChanges`
- `org.changeDetails`

## Safety Checklist

1. Run `preflight` first.
2. Use `--workos-org-id` or `--limit` while debugging.
3. Confirm `affected`, `create`, `update`, `delete`, `noop`, and `stale_skip`
   counts match expectations.
4. Review `planned_change_details` for updates and deletes.
5. Run writes in phases when possible: `global-roles`, then `organizations`.
6. Run `validate` after writes.
7. For prod, capture the preflight output before writing.

## Troubleshooting

If the command fails with `SQLSTATE 42501` or `permission denied for table ...`,
the Cloud SQL proxy connected successfully, but your IAM database user does not
have enough table privileges. Go to `~/github.com/speakeasy-api/gram-infra`,
run `mise gcp:db:master dev` or `mise gcp:db:master prod`, and choose `READ`
for preflight/validate or `ALL` before write phases.
