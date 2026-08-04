# Demo workspace seed

SQL-first seed for the shared, read-only demo organization. The same two SQL
files run in every environment:

- `postgres.sql` — installs and executes `demo.ensure_demo_org()`
  (idempotent; all writes scoped to the demo constants; pre/postflight
  isolation asserts abort the transaction on any violation).
- `clickhouse.sql` — scoped deletes (source table + every MV target) followed
  by fresh inserts; MVs repopulate summaries on INSERT. Postflight
  `throwIf` asserts fail the run on missing or leaked rows.
- `PAGES.md` — the acceptance contract: which dashboard page each piece of
  data feeds and its verification status.
- `verify.spec.ts` — Playwright assertions per page (run via
  `mise run seed:demo --verify`).

## Fixed constants

| Thing      | Value                                                                    |
| ---------- | ------------------------------------------------------------------------ |
| Org id     | `org_gram_demo_workspace` (slug `acme-demo`, `gram_account_type='demo'`) |
| Project A  | `dec0de00-0000-4000-a000-000000000001` (`acme-support`)                  |
| Project B  | `dec0de00-0000-4000-a000-000000000002` (`acme-platform`)                 |
| Chat ids   | `md5('gram-demo-chat-' concatenated with n)::uuid` — same in both stores |
| Demo users | `user_demo_*` / `*@demo.getgram.ai`                                      |

Timestamps are always `now()`-relative (trailing ~12 days): the daily prod
rerun regenerates a fresh window, data never goes stale, and no MV backfill is
ever needed (fresh rows are past every MV date cutoff).

## Running

- Local, whole stack: `mise run seed:demo`, then verify pages with the
  `verify.md` playbook (playwright agent).
- Prod (target wiring):
  1. Postgres: install `demo.ensure_demo_org()` and schedule
     `SELECT demo.ensure_demo_org();` daily via pg_cron (infra repo).
  2. ClickHouse: run `clickhouse.sql` via `clickhouse-client --multiquery`
     from the infra cron, AFTER the Postgres run (CH has no procedural
     functions). Both are idempotent.

## Iteration loop

1. Edit the SQL.
2. `mise run seed:demo --verify`.
3. Fix until green, tick the page in `PAGES.md`, commit. Prod picks the change
   up on the next scheduled run.

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
3. Lift the impersonation transcript block in
   `server/internal/chat/impl.go:679` when the impersonated org is the demo
   org (its transcripts are fake).
4. Force scope enforcement for demo impersonation regardless of the org's RBAC
   product feature, PLUS a defense-in-depth guard rejecting mutating RPCs when
   `ActiveOrganizationID == demo org` (scope coverage across handlers is not
   complete).
5. Frontend: reuse the `ImpersonationBanner` machinery for a "Demo workspace —
   read only" banner + exit; add the entry point ("Explore demo workspace").
6. `access.listMembers` / `access.listRoles` / `access.listGrants` return 400
   ("organization is not linked to WorkOS") for the demo org (`workos_id` is
   NULL by design). Consequences observed: the insights dock's `useMembers()`
   throws to the page error boundary (project pages crash), and the frontend
   retry-loops `listGrants` (~1,500 requests) while `RequireScope
scope="org:admin"` gates the page into "Access restricted". Those endpoints
   must tolerate WorkOS-less orgs (return empty lists) or the frontend must
   catch the error — impersonating a non-WorkOS org currently breaks the whole
   RBAC-gated frontend.
7. The Logs page sits behind `EnterpriseGate` (`useProductTier` wants
   `gram_account_type === 'enterprise'`); the demo org is `'demo'` by design,
   so either the gate learns the demo account type or the demo skips that
   page.
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
