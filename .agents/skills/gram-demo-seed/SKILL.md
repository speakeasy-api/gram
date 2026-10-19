---
name: gram-demo-seed
description: Use when adding a new feature or changing an existing one that surfaces data in the dashboard — the demo org (ai.speakeasy.com/explore-demo) must show it — and when editing or extending the demo org seed or the local dev seed — server/internal/demoseed/ (seed_*.go, ch_*.go, queries/*.sql), demoseed.Spec, RunLocalFixtures, gram demo-seed, mise run seed, mise run seed:demo, seed/demo/ docs, TestDemoSeedSafety, the demo-seed-safety CI job, org_gram_demo_workspace, dec0de00 uuids — or when that CI check fails with "touched another tenant's rows", "reseed is not cleaning up or not idempotent", "hardcodes the demo identifier", or a demo seed pre/postflight exception.
---

# Extending the demo org seed

The demo seed provisions the shared read-only demo organization AND, retargeted,
your local development organization. It is Go code in
`server/internal/demoseed/` (sqlc for Postgres, hand-written batches for
ClickHouse), compiled into the server binary and run by `gram demo-seed` — the
same code path locally and in prod, versioned atomically with each deploy. Prod reruns it daily; every run **deletes all
demo-scoped data and reinserts it fresh**, so the seed must stay idempotent and
perfectly scoped. `TestDemoSeedSafety` (its own required CI check,
`demo-seed-safety`) enforces that mechanically — this skill explains the system
and what the test will reject.

## When the seed must change

The demo org (`ai.speakeasy.com/explore-demo`) is how customers explore Speakeasy before
their own org is set up, and the same seed provisions every developer's local
org. So a feature that ships without seed data shows an empty page to both.

**Any new feature, or change to an existing one, that introduces or reshapes
data a page renders must land with the matching seed change in the same
PR.** Rule of thumb: if the change adds a table, a column a page reads, a new
entity kind, or a new telemetry dimension, the seed needs a row for it. Then
verify the affected pages per `seed/demo/verify.md` and update
`seed/demo/PAGES.md`.

## Quick reference

| Task                | How                                                                  |
| ------------------- | -------------------------------------------------------------------- |
| Edit the seed       | `server/internal/demoseed/seed_*.go` + `queries/*.sql`, `ch_*.go`    |
| Regenerate queries  | `mise run gen:sqlc-server`                                           |
| Seed local dev      | `mise run seed` (= `gram demo-seed --local`)                         |
| Apply the demo org  | `mise run seed:demo`                                                 |
| Verify pages        | `seed/demo/verify.md` playbook; contract in `seed/demo/PAGES.md`     |
| Run the safety test | `mise run test:server -tags=demoseed_safety ./internal/demoseed/...` |
| Authoring docs      | `seed/demo/README.md` (specs, local fixtures, prod wiring)           |

## One seed, three tenants

Every id, user, and email the seed writes is built from a `demoseed.Spec`
(`spec.go`) through the `seeder` helpers in `seeder.go` / `roster.go`:
`s.orgID()`, `s.projectID()`, `s.fixed("0000-4000-a000-…")`, `s.det("thing-1")`
(deterministic uuid), `s.name("thing-1")` (hash and trace-id inputs),
`s.userID("amara")`, `s.email("amara")`, `s.spec.OrgSlug` / `OrgName` /
`Marker`. Never write one of the demo org's values as a literal:

- `DefaultSpec()` — the demo org; what prod runs daily.
- `LocalSpec()` — the dev-idp's org (`dev-idp/pkg/devidentity`), writable, what
  `mise run seed` provisions.
- `otherTenantSpec` — the safety test's adversarial fixture tenant.

**A new kind of globally unique identifier means a new `Spec` field and a
helper that builds it.** A literal leaks the local and test tenants' rows into
the demo org's scope. `TestSeedSourceHasNoDemoIdentifiers` fails with
`hardcodes the demo identifier %q` — no database needed.

Local-only rows (your user, the API key, the MCP App) live in
`local.go` / `mcpapp.go` as `RunLocalFixtures`, NOT in the seed phases — the
shared demo org must never have them.

The test package is `server/internal/demoseed`; invoke `mise run
test:server` from anywhere — the task chdirs to `server/`, hence the
`./internal/...` package path. The safety test is
build-tagged (`demoseed_safety`): running the package without the tag reports
"no test files" — that is expected, not a broken package.

## How the seed is laid out

- **Postgres** — `seed_NN_*.go` phases, listed in order in
  `seeder.runPostgres`, each with its sqlc queries in `queries/NN_*.sql`
  (prefix every query name with the phase's prefix). All phases run in ONE
  transaction, so a failed run leaves yesterday's data, and every statement
  gets its own `statement_timeout` — keep each statement small; loop in Go.
  The first phase asserts the demo constants collide with nothing that is
  not the demo org, then deletes demo data (the projects delete cascades most
  of it; org-scoped tables are deleted explicitly) before the reinserts. The
  postflight phase (implemented in `seed_08_postflight.go` and
  `seed_09_postflight_identity.go`) fails the run on missing or leaked rows. Deliberately NOT a migration: the seed churns, and the daily rerun is
  the upgrade path.
- **ClickHouse** — `ch_NN_*.go` phases (`seeder.runClickHouse`).
  `ch_01_purge_telemetry.go` runs scoped lightweight deletes on the telemetry
  source, **every MV target** (enumerate them by grepping `MATERIALIZED
  VIEW` in `server/clickhouse/schema.sql`), and the org-keyed tables, waits
  for them to be visible, then the phases insert fresh rows built in Go with
  `PrepareBatch`; the MVs repopulate summaries on INSERT. Postflight checks
  in `ch_03_events.go` fail the run.

Fixed identity: org `org_gram_demo_workspace` (slug `acme-demo`), single
project `dec0de00-0000-4000-a000-000000000001`, users `user_demo_*` /
`*@demo.getgram.ai`. `names.go` holds what both stores share: `detUUID` (md5
with version nibble `5`, variant `8`), the chat schedule (`chatID`, `chatTS`,
`chatOwner`, `chatSurface`), the risk draws, and `hash64` for variety
(well-mixed, so `hash64(...) % n` is safe). Both stores call these, which is
what keeps their rows joined — never re-derive them. `s.det` inputs only need
to be unique: pick a fresh `<thing>-` prefix per row family and never reuse
one across families. Timestamps are always relative to `s.now` (the Postgres
transaction time); chat activity trails ~12 days.

## Rules the safety test enforces

`TestDemoSeedSafety` provisions a fake "customer" tenant by running the seed
retargeted at `otherTenantSpec`, snapshots both databases, runs the real seed
twice with tampering in between, and fails on:

1. **Any write outside the demo scope.** Every Postgres `DELETE`/`UPDATE`
   must be scoped by the demo org id, demo project id, or ids derived from
   them; every ClickHouse delete by `organization_id` or `gram_project_id`.
   Failure reads `table X: … touched another tenant's rows` / `a pre-existing
   row of another tenant was modified or deleted`.
2. **Data that survives a rerun uncleaned, or unstable counts.** New inserts
   need a matching delete (or an upsert) so the run stays idempotent —
   failure reads `reseed is not cleaning up or not idempotent`. Any NEW
   table the seed writes needs its own explicit scoped delete unless you
   have verified it sits inside the demo project's FK delete cascade. Also
   add a postflight check on its expected demo row count, following the
   existing ones in `seed_08_postflight.go` / `ch_03_events.go`.
3. **Telemetry rows missing the `demo-seed` marker.** Every
   `telemetry_logs` insert must carry
   `{"gram.deployment.id":<s.spec.Marker>}` in `resource_attributes`; the
   seed's own postflights fail otherwise.
4. **Identifiers not built from the Spec.** If you introduce a new class of
   globally unique identifier (a new uuid prefix, a new `something_demo_*` id
   written to a globally-unique column, a new email domain), add a field to
   `demoseed.Spec`, set it in every Spec, and build it through a helper. Miss
   it and the test fails as a unique-constraint violation during seeding —
   and the cheaper `TestSeedSourceHasNoDemoIdentifiers` fails first when the
   value is a demo literal.
5. **New ClickHouse tables with unrecognized scoping columns.** The test
   infers demo scope from `organization_id` / `gram_project_id` columns. A
   demo-written table using a different column name fails the isolation check
   (fail-safe false positive) — name the scoping column one of those two.

## Iteration loop

1. Edit the phase and its queries, then `mise run gen:sqlc-server`; keep
   every statement scoped and every insert cleaned up. New identifier family?
   Add a `Spec` field.
2. `mise run seed` against the local stack — this is also how developers get
   their data, so a broken seed breaks everyone's environment. Run it TWICE:
   the second run is what proves the delete/reinsert cycle rolls forward.
3. Verify affected dashboard pages per `seed/demo/verify.md`; update
   `seed/demo/PAGES.md`.
4. `mise run test:server -tags=demoseed_safety ./internal/demoseed/...`
   (~1 min with containers). Fix anything it names — the failure message
   includes the exact table.
5. Commit. Prod picks the change up on the next release promotion; the daily
   CronJob rerun replaces the demo org's data wholesale.

## Common mistakes

- Deleting with an unscoped statement "because the table only holds demo
  data today" — the safety test plants another tenant's rows in every table
  the seed touches, so this always fails.
- Adding a ClickHouse insert without deleting the MV targets it feeds — a
  DELETE on `telemetry_logs` never shrinks MV targets; each target needs its
  own scoped delete.
- Reusing one trace id across row types — `trace_summaries` collapses per
  `trace_id`, merging them into a single unclassifiable trace.
- Editing `users`, `organization_features`, or `organization_metadata`
  expecting deletes: those are upsert-only by design (global/shared tables);
  removals need an explicit cleanup statement.
- Real-looking data: this repo is public — fictional companies,
  `*@demo.getgram.ai` emails, `EXAMPLE`/`DEMO`-marked fake secrets only.
- Putting a developer-only row in a seed phase. Anything that exists so LOCAL
  dev works (your user, keys, deployments the runtime must execute) belongs in
  `RunLocalFixtures`; the phases are shared with the public demo org.
- Seeding a child row of `projects` without checking its FK action. The seed
  deletes and recreates the project every run, so a `RESTRICT`ing child wedges
  the next run — and `environments` is worse: `project_id` is `NOT NULL` with
  `ON DELETE SET NULL`, so it always errors. Such tables need their own scoped
  delete before the projects delete.
