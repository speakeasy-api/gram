# Local admin-list fixtures

Opt in with the local PostgreSQL database already running:

```sh
mise run seed:admin
```

If the stack is paused, run `mise run wake` first. This task deliberately does
not wake/restart services, run the regular seed, or contact an identity/billing
provider. Neither `mise run seed` nor `mise run seed:demo` invokes it.

The command requires `GRAM_ENVIRONMENT=local`, database **gram**, user **gram**,
and loopback-only PostgreSQL hosts (including fallback hosts). Unix sockets,
remote hosts and other database/user names are rejected before connecting.
It honors the worktree's configured port. There is no force/bypass flag.
As with any loopback guard, do not point a local port forward at a remote DB.

It creates 120 explicitly **Fictional Admin Lab NN** organizations with stable
`org_local_admin_fixture_NN` IDs, 60 enabled and 60 disabled. Active member counts
cycle through 0, 1, 5, 10, 25 and 100. Creation times refresh on every invocation:
exactly at and one second before/after UTC today's start and inclusive 7/14/30-day
calendar starts (midnight minus N-1 days), plus rolling 7/14/30-day boundaries.
A custom end-date example uses three days ago: its exclusive upper bound is
midnight two days ago, also bracketed by one second on each side. It includes
now, one hour ago, and 60/90/180/365 days ago. Future timestamps are omitted
(e.g. one second after today's start when invoked exactly at midnight).
Both status sets cover the same dates and exceed the admin's 50-row page size.
Existing IDs 01 through 40 remain stable when expanding an earlier seed.

After committing successfully the command prints:

```text
Seeded 120 fictional organizations (60 active, 60 disabled). Search: Fictional Admin Lab
```

Members are synthetic `user_local_admin_fixture_*` users at the reserved
`admin-seed.invalid` domain. Membership IDs use a reserved negative range.
No developer account is added; no projects, API keys, sessions, identity
accounts or billing accounts are created. The normal org picker uses
`ListOrganizationsForUser` in `server/internal/organizations/queries.sql`, via
`server/internal/auth/identity/identity.go`: only the current user's active
memberships are returned. These fixture memberships belong only to synthetic
users, so they do not appear in the developer's picker.

Reruns transactionally upsert the same IDs, revive fixture users/memberships and
refresh dates/disabled state without duplicating rows. Concurrent invocations
are serialized. Existing unrelated orgs/users/memberships are not changed.
The fixture namespace is reserved: do not add real users, external accounts,
or extra memberships to these orgs. This is not a cleanup tool for manually
modified fixture sets. A uniqueness collision aborts the entire transaction.

Focused checks:

```sh
mise run test:server ./internal/demoseed -run TestAdminSeed -count=1
mise run test:server -tags=demoseed_safety ./internal/demoseed -run TestAdminSeed -count=1
```

The tagged checks use isolated test databases, verify repeat/next-day reruns,
preserve unrelated rows, verify soft-deleted user/membership revival and stable
membership IDs, check org-picker invisibility, and test rollback on
collision. No admin filters or backend API behavior are changed by this seed.
