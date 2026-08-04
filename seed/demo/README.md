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
3. Fix until green, tick the page in `PAGES.md`, commit. Prod picks the change
   up on the next release.

## Server changes required for user access (not yet implemented)

Access is by IMPERSONATION only — demo org never gets membership rows.

1. Demo-impersonation path parallel to the admin override
   (`server/internal/middleware/admin.go`,
   `server/internal/auth/impl.go` Callback priority 1): any authenticated user
   may set active org to `org_gram_demo_workspace` — and ONLY that org.
2. `authz.Engine.PrepareContext` (`server/internal/authz/engine.go:118`): demo
   impersonation gets a read-only grant set (`org:read`, `project:read`,
   `mcp:read`, `skill:read`, `chat:read`; NO `environment:read`, no writes) —
   not `allScopeGrants()`.
3. DONE (commit ae256351c1): transcript block lifted for the demo org in
   `chat.LoadChat` via `constants.DemoOrganizationID`.
4. Force scope enforcement for demo impersonation regardless of the org's RBAC
   product feature, PLUS a defense-in-depth guard rejecting mutating RPCs when
   `ActiveOrganizationID == demo org` (scope coverage across handlers is not
   complete).
5. Frontend: reuse the `ImpersonationBanner` machinery for a "Demo org —
   read only" banner + exit; add the entry point ("Explore demo org").
6. DONE for impersonation (commits 0f8d13113e + ae256351c1):
   `access.listGrants` returns the admin scope set before the WorkOS check,
   and `listMembers`/`listRoles` fall through to the pure-Postgres role
   manager reads for impersonated WorkOS-less orgs. The dedicated
   demo-impersonation path (change 1) should reuse the same carve-outs.
7. RESOLVED by data: the demo org is seeded with
   `gram_account_type='enterprise'`, so `EnterpriseGate` pages (Logs, …) are
   unlocked. Demo identity is carried by the org id constant, never by
   account type.
8. The auth callback's org-metadata upsert overwrites `gram_account_type`
   (observed: 'demo' → 'pro' after one impersonation login). The daily seed
   run restores it, but the server should preserve the demo account type so
   the flag is trustworthy between runs.

Until these land, verify locally with a platform-admin user (local `mise run
seed` makes you one) via the existing admin override — expect the chat
transcript sheet to be blocked (change 3) and writes to be allowed (change 4).

## Customer-data rule

Everything in this directory is committed to a public repo: fictional
companies, `*@demo.getgram.ai` emails, `EXAMPLE`/`DEMO` marked fake secrets
only. Never paste real org/project/user ids here.
