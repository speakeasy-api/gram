# Demo org seed

SQL-first seed for the shared, read-only demo organization — and, retargeted,
for your local development organization. The SQL lives in
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

## Tenants

The scripts are written against the demo org's literals. Every other tenant is
produced by rewriting those literals — one `demoseed.Spec` per tenant, applied
by `Spec.Rewrite` before the SQL is executed:

| Spec              | Org id                                     | Used by                                      |
| ----------------- | ------------------------------------------ | -------------------------------------------- |
| `DefaultSpec()`   | `org_gram_demo_workspace`                  | production's daily run, `mise run seed:demo` |
| `LocalSpec()`     | derived from WorkOS `org_devidp_speakeasy` | `mise run seed` — your local dev org         |
| `otherTenantSpec` | `org_gram_othr_workspace`                  | `TestDemoSeedSafety`'s adversarial fixture   |

Rewriting with `DefaultSpec()` is a no-op, so production executes the scripts
exactly as written (asserted by `TestDefaultSpecRewriteIsIdentity`). Adding a
new identifier family to the SQL means adding a field to `Spec` — otherwise it
is NOT rewritten, and the local and test tenants write it into the demo org's
scope. `TestLocalSpecRewritesEveryDefaultIdentifier` catches that without
needing a database.

`LocalSpec` identifies the dev-idp's default org, so logging in locally lands
you inside the seeded data. Unlike the demo org it is a perfectly ordinary
writable org: none of the demo carve-outs in `authz.Engine` or
`middleware.DemoOrgWriteGuard` key off it.

Its `OrgID` is **derived**, not equal to the WorkOS org id:
`organization_metadata.id` for any organization that came from WorkOS is
`orgid.FromWorkOSID(workos_id)` — a UUIDv5 — and the auth callback recomputes
it on every login. The demo org is the exception that makes this easy to get
wrong: its id is hand-written and never came from WorkOS. Seed under the raw
WorkOS id and the callback derives a different id, inserts a SECOND
organization, and drops you into an empty org next to the seeded one, with a
suffixed slug because the seeded row already holds `speakeasy`.

## Running

- Local development: `mise run seed`. Applies `LocalSpec`, then
  `RunLocalFixtures` — see below.
- The demo org itself: `mise run seed:demo` (wraps `gram demo-seed`), then
  verify pages with the `verify.md` playbook (playwright agent).
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

## Local-only fixtures

`gram demo-seed --local` runs `RunLocalFixtures`
(`server/internal/demoseed/local.go`) after the seed, adding what a developer
needs and the shared demo org must never have:

- **You.** A `users` row derived from `git config user.email` exactly as the
  dev-idp derives it, a membership, the Admin role, platform super-admin, and
  a direct `chat:read` grant (Admin deliberately omits it, and without it the
  Agent Sessions list would hide every seeded chat because they belong to the
  fictional teammates).
- **Fixed credentials.** A `seed-key` API key and the Postgres-MCP tunnel key,
  both well-known constants rather than generated values — so `mise.toml` ships
  `GRAM_API_KEY` and `TUNNEL_LOCAL_*` as checked-in defaults and nothing is
  written back into `mise.local.toml` after a seed run. `server/.golangci.yaml`
  carries a narrow, commented `G101` exemption for them: they are genuinely
  hardcoded credentials, deliberately, scoped to one developer's database.
- **A default environment**, the global `Gram Recommended` MCP registry row
  (not tenant-scoped, so it cannot live in the seed proper), and the
  Playground's MCP App: a Gram Function zipped in-memory from
  `server/internal/demoseed/mcpapp/` and hung off the seeded deployment, so the
  demo org never gets a functions deployment production would have to run.

The fixtures are idempotent, and everything they write is either upserted by a
fixed id or cascades from the seeded deployment/project, so a reseed rolls
forward cleanly. There is no completion marker: the seed is fast and
idempotent, so `mise run seed` simply always runs.

Not covered, deliberately: real OpenAPI/functions **deployments** through the
API. The seed fabricates the tool stack in SQL, which is what the dashboard
reads, but it never exercises upload → parse → tool generation. Testing that
pipeline is a test's job, not the seed's.

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

How it works: a fake "customer" tenant is provisioned by running the seed
retargeted at `otherTenantSpec`, so the customer has rows in exactly the
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
globally-unique identifier family as a `Spec` field. The `gram-demo-seed`
agent skill covers these rules in detail.

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
