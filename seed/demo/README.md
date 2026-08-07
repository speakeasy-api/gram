# Demo org seed

SQL-first seed for the shared, read-only demo organization. The SQL lives in
`server/internal/demoseed/{postgres,clickhouse}.sql`, is go:embedded into the
server binary, and is applied by the `gram demo-seed` subcommand — the SAME
code path locally and in prod, versioned atomically with each deploy. This
directory holds the authoring docs:

- `server/internal/demoseed/postgres.sql` — installs and executes
  `demo.ensure_demo_org()` (idempotent; all writes scoped to the demo
  constants; pre/postflight isolation asserts abort the transaction on any
  violation). Deliberately NOT a migration: migrations are append-only and
  the seed churns; `CREATE OR REPLACE` + daily rerun is the upgrade path.
- `server/internal/demoseed/clickhouse.sql` — scoped deletes (source table +
  every MV target) followed by fresh inserts; MVs repopulate summaries on
  INSERT. Postflight `throwIf` asserts fail the run on missing or leaked
  rows. Keep semicolons out of string literals — the runner splits on ';'.
- `PAGES.md` — the acceptance contract: which dashboard page each piece of
  data feeds and its verification status.
- `verify.md` — the agent-driven page verification playbook.

## Fixed constants

| Thing      | Value                                                                                         |
| ---------- | --------------------------------------------------------------------------------------------- |
| Org id     | `org_gram_demo_workspace` (slug `acme-demo`, account type `enterprise` for gated pages)       |
| Project    | `dec0de00-0000-4000-a000-000000000001` (`default` — the single demo project)                  |
| Chat ids   | `demo.det_uuid('gram-demo-chat-' + n)`: md5, version nibble 5, variant 8; same in both stores |
| Demo users | `user_demo_*` / `*@demo.getgram.ai`                                                           |

Timestamps are always `now()`-relative (trailing ~12 days): the daily prod
rerun regenerates a fresh window, data never goes stale, and no MV backfill is
ever needed (fresh rows are past every MV date cutoff).

## Running

- Local: `mise run seed:demo` (wraps `gram demo-seed`), then verify pages
  with the `verify.md` playbook (playwright agent).
- Prod (target wiring, gram-infra repo — NOT a GitHub Action, NOT pg_cron;
  pg_cron is not provisioned on the Cloud SQL instance despite earlier
  assumptions): a Helm-templated Kubernetes CronJob in
  `infra/helm/gram/templates/` running the server image with
  `args: ["demo-seed"]`, synced by ArgoCD like every other workload. The
  image tag is pinned per env in `values-{env}.yaml`, so a merged seed change
  reaches prod on the next release promotion. Known wiring caveats (see the
  db-sweeper CronJob as the template): the app IAM DB user has no CREATE
  grants — use the atlas/owner Postgres URL secret; and run the Cloud SQL
  proxy as an explicit sidecar with --quitquitquit so the Job completes.

## Iteration loop

1. Edit the SQL in `server/internal/demoseed/`.
2. `mise run seed:demo`, verify per `verify.md`.
3. Run the safety test (below), fix until green, tick the page in `PAGES.md`,
   commit. Prod picks the change up on the next release.

## Safety test (required CI check)

`TestDemoSeedSafety` (`server/internal/demoseed/safety_test.go`) is the
merge-blocking guard for every seed change, run by the standalone
`demo-seed-safety` job in `pr.yaml` (wired into `ci-gate`). Locally:

    mise run test:server -tags=demoseed_safety ./internal/demoseed/...

It is build-tagged out of the sharded server suite, so a plain
`go test ./server/internal/demoseed/` reporting "no test files" is expected.

How it works: a fake "customer" tenant is provisioned by running a
transformed copy of the seed itself (every demo identifier string-rewritten
via `otherTenantReplacements`), so the customer has rows in exactly the
tables the seed touches — automatically including tables future seed versions
add. Then the real seed runs twice, with stray demo rows planted in between,
and the test asserts:

1. **Isolation** — no row outside the demo scope is modified, deleted, or
   added (full row-fingerprint snapshots in Postgres; per-table count + hash
   of non-demo rows in ClickHouse, scoped by `organization_id` /
   `gram_project_id` columns).
2. **Cleanup** — the planted stray demo rows are wiped by the rerun, so seed
   versions can always roll forward.
3. **Idempotence** — per-table row counts are identical after every run for
   plain MergeTree tables; Summing/Aggregating MV targets collapse rows on
   `now()`-bucketed keys, so they are checked for isolation only.

What that means when extending the seed: scope every statement to the demo
constants, pair every insert with a delete (or upsert), keep the
`gram.deployment.id: demo-seed` marker on all telemetry rows, name ClickHouse
scoping columns `organization_id`/`gram_project_id`, and register any new
globally-unique identifier family in `otherTenantReplacements`. The
`gram-demo-seed` agent skill covers these rules in detail.

## Server changes required for user access

Access is by IMPERSONATION only — demo org never gets membership rows.

1. DONE: `auth.enterDemo` (`server/internal/auth/impl.go`) switches any
   authenticated session's active org to `org_gram_demo_workspace` — and ONLY
   that org — without a logout round-trip (unlike the admin override, which
   only takes effect at the login callback). `sessions.Authenticate` accepts
   the membership-less demo session.
2. DONE: `authz.Engine.PrepareContext` gives any demo session the fixed
   read-only grant set `authz.DemoScopeGrants()` (`org:read`, `project:read`,
   `mcp:read`, `skill:read`, `chat:read`; NO `environment:read`, no writes) —
   for everyone, including admins with the override cookie.
3. DONE (commit ae256351c1): transcript block lifted for the demo org in
   `chat.LoadChat` via `constants.DemoOrganizationID`.
4. DONE: `authz.Engine.ShouldEnforce` forces enforcement for the demo org
   regardless of its RBAC product feature, and
   `middleware.DemoOrgWriteGuard` (`server/internal/middleware/demo.go`,
   wired in `start.go`) rejects mutating `/rpc` calls by method-name verb as
   defense-in-depth (POST alone is no signal — telemetry/risk reads POST).
5. DONE: `ImpersonationBanner` shows "Demo org — sample data" for any session
   whose active org slug is `acme-demo` (cookie no longer required); exit
   switches back to the user's own org via `auth.switchScopes`. Entry points:
   the `/explore-demo` route (stable link target) and an "explore a live demo
   org" link on the BookDemo gate. `/explore-demo` is exempt from the
   AuthProvider whitelist gate and slug-redirect logic.
6. DONE for impersonation (commits 0f8d13113e + ae256351c1) and extended to
   demo sessions: `access.listGrants` returns the demo grant set, and
   `listMembers`/`listRoles` fall through to the pure-Postgres role manager
   reads (`isImpersonatingUnlinkedOrg` treats any demo session as
   impersonating).
7. RESOLVED by data: the demo org is seeded with
   `gram_account_type='enterprise'`, so `EnterpriseGate` pages (Logs, …) are
   unlocked. Demo identity is carried by the org id constant, never by
   account type.
8. OPEN: the auth callback's org-metadata upsert overwrites
   `gram_account_type` (observed: 'demo' → 'pro' after one impersonation
   login). The daily seed run restores it, but the server should preserve the
   demo account type so the flag is trustworthy between runs.

## Customer-data rule

Everything in this directory is committed to a public repo: fictional
companies, `*@demo.getgram.ai` emails, `EXAMPLE`/`DEMO` marked fake secrets
only. Never paste real org/project/user ids here.
