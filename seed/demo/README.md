# Demo org seed

Go seed for the shared demo organization — and, retargeted, for your local
development organization. It lives in `server/internal/demoseed/`, compiles
into the server binary, and runs through the `gram demo-seed` subcommand — the
SAME code path locally and in prod, versioned atomically with each deploy.
This directory holds the authoring docs:

- Postgres: `seed_NN_*.go` phases (`seeder.runPostgres` lists them in order),
  with their sqlc queries in `queries/NN_*.sql` (generated into `repo/` by
  `mise run gen:sqlc-server`). The phases run in ONE transaction, so a failed
  run leaves yesterday's data in place, and every statement gets its own
  `statement_timeout`. The first phase asserts the demo constants collide with
  nothing else and deletes the demo-scoped data; the last (postflight) phases
  fail the run on missing or leaked rows. Deliberately NOT a migration:
  migrations are append-only and the seed churns; the daily rerun is the
  upgrade path.
- ClickHouse: `ch_NN_*.go` phases (`seeder.runClickHouse`). Scoped deletes of
  raw/source tables and every incremental MV target, then fresh inserts built
  in Go and sent with `PrepareBatch`. The inserts repopulate the MV targets;
  postflight checks fail the run on missing or leaked rows.
- Shared helpers: `names.go` (deterministic ids, the chat schedule, risk
  draws, `hash64` for variety) and `roster.go` (the demo users and fixed ids).
  Both stores derive chat ids, owners, and timestamps from these, which is
  what keeps them joined.
- `PAGES.md` — the acceptance contract: which dashboard page each piece of
  data feeds and its verification status.
- `verify.md` — the agent-driven page verification playbook.

## Exact upstream-session attachment fixture

The shared seed writes two active release agents with one fictional human owner.
Both reference the same exact upstream session through
`principal_remote_session_bindings`; the requesting Per-User OAuth Server session's issuer is
linked to its upstream client. This is intentionally inert: `.invalid` issuer,
invalid ciphertext, no refresh token, and auto-refresh off. No real credentials
or external account IDs are included. Deterministic `gram-demo-attachment-*`
IDs come from `seeder.det`, which namespaces them by `Spec.NameSeed`. Cleanup runs before
any referenced parent deletes; postflight checks enforce two reachable
bindings and one underlying session. See `PAGES.md` and `verify.md` for UI QA.

## Fixed constants

| Thing      | Value                                                                                                        |
| ---------- | ------------------------------------------------------------------------------------------------------------ |
| Org id     | `org_gram_demo_workspace` (slug `acme-demo`, account type `enterprise` for gated pages)                      |
| Project    | `dec0de00-0000-4000-a000-000000000001` (`default` — the single demo project)                                 |
| Chat ids   | `seeder.chatID(n)` = `detUUID("gram-demo-chat-" + n)`: md5, version nibble 5, variant 8; same in both stores |
| Demo users | `user_demo_*` / `*@demo.getgram.ai`                                                                          |

Timestamps are always `now()`-relative (trailing ~12 days): the daily prod
rerun regenerates a fresh window and data never goes stale. Incremental MVs,
including billing usage's UTC-day summaries, populate their targets on INSERT.
Physical duplicate meter deliveries intentionally count independently.

## Tenants

Every id, user, and email the seed writes is built from a `demoseed.Spec`
through the `seeder` helpers (`orgID`, `projectID`, `fixed`, `det`, `name`,
`userID`, `email`), one Spec per tenant:

| Spec              | Org id                                     | Used by                                      |
| ----------------- | ------------------------------------------ | -------------------------------------------- |
| `DefaultSpec()`   | `org_gram_demo_workspace`                  | production's daily run, `mise run seed:demo` |
| `LocalSpec()`     | derived from WorkOS `org_devidp_speakeasy` | `mise run seed` — your local dev org         |
| `otherTenantSpec` | `org_gram_othr_workspace`                  | `TestDemoSeedSafety`'s adversarial fixture   |

Adding a new identifier family means adding a field to `Spec` and a helper
that builds it — a literal written straight into the seed would put the local
and test tenants' rows in the demo org's scope.
`TestSeedSourceHasNoDemoIdentifiers` catches that without needing a database.

`LocalSpec` identifies the dev-idp's default org, so logging in locally lands
you inside the seeded data. Unlike the demo org it is an ordinary org whose
grants come from real membership: none of the demo carve-outs in
`authz.Engine` key off it.

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

### Billing usage summaries

The ClickHouse seed deletes the demo tenant's
`billing_meter_daily_summaries` rows before deleting its raw meter rows. The
runner waits until both deletions are visible, then the fresh
`billing_meter_readings_by_time` inserts synchronously trigger the incremental
materialized view. This makes each seed run scoped and repeatable without a
full-history reporting scan or a separate rebuild.

Summaries have UTC-day precision. Every physical delivery contributes to the
daily `SummingMergeTree`; producer-side deduplication is responsible for
preventing unwanted duplicate accounting.

## Local-only fixtures

`gram demo-seed --local` runs `RunLocalFixtures`
(`server/internal/demoseed/local.go`) after the seed, adding what a developer
needs and the shared demo org must never have:

- **You.** A `users` row derived from `git config user.email` exactly as the
  dev-idp derives it, a membership, the Admin role, platform super-admin, and
  a direct `chat:read` grant (Admin deliberately omits it, and without it the
  Agent Sessions list would hide every seeded chat because they belong to the
  fictional teammates).
- **Fixed credentials.** A `seed-key` API key, a well-known constant rather
  than a generated value — so `mise.toml` ships `GRAM_API_KEY` as a checked-in
  default and nothing is written back into `mise.local.toml` after a seed run.
  `server/.golangci.yaml` carries a narrow, commented `G101` exemption for it:
  it is a genuinely hardcoded credential, deliberately, scoped to one
  developer's database.
- **A default environment**, the global `Gram Recommended` MCP registry row
  (not tenant-scoped, so it cannot live in the seed proper), and the
  Playground's MCP App: a Speakeasy Function zipped in-memory from
  `server/internal/demoseed/mcpapp/` and hung off the seeded deployment, so the
  demo org never gets a functions deployment production would have to run.
- **Working upstream servers.** The seed has ten MCP servers (keep it at ten
  or fewer). Each is named for the configuration it tests, and in the local
  org each one works right after `mise run seed`. The stack starts a throwaway MCP fixture server
  (`mcp-fixture/`, pitchfork daemon `mcp-fixture`, `GRAM_MCP_FIXTURE_URL`): an
  OAuth 2.1 server (sign in by picking an email; dynamic registration, client
  ID metadata documents, PKCE, refresh rotation, tokens bound to the requested
  resource), MCP endpoints `/mcp/oauth`, `/mcp/auto`, `/mcp/shared` and
  `/mcp/open` and `/mcp/ema`, a personal token page at `/tokens`, a stub of
  the demo API, and a tunnel agent on the local tunnel gateway. Its credentials live in
  `fixtureauth` and are checked in on purpose. `local_mcp_fixture.go` points
  the local org at it:

  | Server                             | Tests                                                                                                | Locally                                                                                 |
  | ---------------------------------- | ---------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
  | OpenAPI Server (API Key)           | A server generated from an OpenAPI document, reached with a Speakeasy API key                        | Calls the fixture's API stub (`ACME_SERVER_URL` system environment)                     |
  | Read-Only Tool Subset              | A second toolset over the same API with only read-only tools; gateway membership, killswitch history | Same API stub                                                                           |
  | Server-Specific Sign-In            | A hosted server with its own Speakeasy sign-in                                                       | Same API stub                                                                           |
  | External OAuth Server              | Callers bring a token from the customer's own OAuth server                                           | Its authorization server metadata is the fixture's                                      |
  | Per-User OAuth Server              | Each person signs in upstream: expiry, refresh, scope pins, agent connections                        | `/mcp/oauth`; seeded sign-ins hold real fixture tokens                                  |
  | Service Account Server             | One static shared credential for every caller                                                        | `/mcp/shared` with the checked-in shared token                                          |
  | Pass-Through Token Server          | Each caller's own token, forwarded upstream                                                          | `/mcp/shared`; the caller sends `X-Upstream-Token` (create one at `/tokens`)            |
  | Auto-Configured OAuth Server       | An admin sets up sign-in through discovery and client registration                                   | `/mcp/auto`, with no client until an admin saves Auto-Configure                         |
  | Tunnelled Private Server           | A server on a private network, reached through a tunnel                                              | The fixture's tunnel agent connects with `fixtureauth.TunnelKey` and serves `/mcp/open` |
  | Enterprise Sign-In (ID-JAG) Server | Sign-in through the company identity provider, exchanged for an ID-JAG                               | `/mcp/ema`, through the dev-idp (below)                                                 |

  The Enterprise Sign-In (ID-JAG) Server is wired by `local_ema.go`. The
  dev-idp plays the company identity provider: the seed creates its EMA app,
  resource, trust rule and the developer's assignment over the dev-idp's
  control RPC, and provisions the developer in the org's directory (federated
  sign-in admits only directory-provisioned people). The server's own
  organization sign-in federates through the dev-idp; Speakeasy then exchanges
  the developer's ID token for an ID-JAG at the dev-idp and redeems it at the
  dev-idp's `/resource-as/<slug>` authorization server, whose access token the
  fixture's `/mcp/ema` checks by introspection. One client id
  (`GRAM_IDP_CLIENT_ID`) serves all three legs, with a checked-in local
  secret. A dev-idp that cannot be reached leaves the server display-only.

  The shared seed uses the placeholder host `mcp-fixture.example.com`; the
  public demo org keeps those display-only values because prod cannot reach
  localhost. A worktree created before the fixture existed picks up its port
  and URLs with `mise run git:worksync`.

The fixtures are idempotent, and everything they write is either upserted by a
fixed id or cascades from the seeded deployment/project, so a reseed rolls
forward cleanly. There is no completion marker: the seed is fast and
idempotent, so `mise run seed` simply always runs.

Not covered, deliberately: real OpenAPI/functions **deployments** through the
API. The seed fabricates the tool stack in SQL, which is what the dashboard
reads, but it never exercises upload → parse → tool generation. Testing that
pipeline is a test's job, not the seed's.

## Iteration loop

1. Edit the seed phase (and its sqlc queries) in `server/internal/demoseed/`;
   run `mise run gen:sqlc-server` after changing a query.
2. `mise run seed:demo`, verify per `verify.md`.
3. Run the safety test (below), fix until green, tick the page in `PAGES.md`,
   commit. Prod picks the change up on the next release.

## Safety test (required CI check)

`TestDemoSeedSafety` (`server/internal/demoseed/safety_test.go`) is the
merge-blocking guard for every seed change, run by the standalone
`demo-seed-safety` job in `pr.yaml` (wired into `ci-gate`). Locally:

    mise run test:server -tags=demoseed_safety ./internal/demoseed/...

The safety test is build-tagged out of the sharded server suite, while the
package's untagged `TestSeedCELCompiles` test still runs with a plain
`mise run test:server ./internal/demoseed/`. Use the tagged command above when
you need to run the safety test.

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
   published plain MergeTree tables and the merged
   `billing_meter_daily_summaries` target. Other Summing/Aggregating MV targets
   collapse rows on `now()`-bucketed keys, so they are checked for isolation
   only.

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
2. DONE: `authz.Engine.PrepareContext` gives any demo session
   `authz.DemoScopeGrants()` — every user-visible scope, unrestricted,
   `org:admin` and `environment:read` included — for everyone, including
   admins with the override cookie. It is the same set `access.listGrants`
   reports to the dashboard, so no page is offered that its handler then
   refuses. Visitors can mutate demo data; the daily reseed reverts it.
3. DONE (commit ae256351c1): transcript block lifted for the demo org in
   `chat.LoadChat` via `constants.DemoOrganizationID`.
4. DONE: `authz.Engine.ShouldEnforce` forces enforcement for the demo org
   regardless of its RBAC product feature. Mutations by demo sessions are
   allowed to land: the daily `gram demo-seed` run deletes and reinserts the
   org's data wholesale, so the seed staying idempotent and scoped — not an
   access control — is what keeps the demo presentable.
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

### Global registry (manual administration)

An empty global registry is expected in production. Once the staff admin UI is
available, staff manually create the initial servers there; production has no
JSON import command or starter-data prerequisite.

No starter inventory is shipped, and there is no automatic local import
workflow. Tenant reseeds and `RunLocalFixtures` never populate or
reset the global catalog, including staff edits and publication state.

Existing Pulse-backed demo installations remain unchanged; this is not a Pulse
cutover. Global staff-only administration is not exposed to demo customers, and
tenant seeds create no demo-prefixed catalog copies.
