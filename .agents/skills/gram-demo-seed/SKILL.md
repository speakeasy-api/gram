---
name: gram-demo-seed
description: Use when editing or extending the demo org seed — server/internal/demoseed/{postgres,clickhouse}.sql, demo.ensure_demo_org(), gram demo-seed, mise run seed:demo, seed/demo/ docs, TestDemoSeedSafety, the demo-seed-safety CI job, org_gram_demo_workspace, dec0de00 uuids — or when that CI check fails with "touched another tenant's rows", "reseed is not cleaning up or not idempotent", "update otherTenantReplacements", or a demo seed pre/postflight exception.
---

# Extending the demo org seed

The demo seed provisions the shared read-only demo organization. Two SQL
scripts in `server/internal/demoseed/` are `go:embed`ded into the server
binary and applied by `gram demo-seed` — the same code path locally and in
prod, versioned atomically with each deploy. Prod reruns it daily; every run
**deletes all demo-scoped data and reinserts it fresh**, so the seed must stay
idempotent and perfectly scoped. `TestDemoSeedSafety` (its own required CI
check, `demo-seed-safety`) enforces that mechanically — this skill explains
the system and what the test will reject.

## Quick reference

| Task                | How                                                                  |
| ------------------- | -------------------------------------------------------------------- |
| Edit the seed       | `server/internal/demoseed/postgres.sql` / `clickhouse.sql`           |
| Apply locally       | `mise run seed:demo`                                                 |
| Verify pages        | `seed/demo/verify.md` playbook; contract in `seed/demo/PAGES.md`     |
| Run the safety test | `mise run test:server -tags=demoseed_safety ./internal/demoseed/...` |
| Authoring docs      | `seed/demo/README.md` (constants, prod wiring, iteration loop)       |

The test package is `server/internal/demoseed`; invoke `mise run
test:server` from anywhere — the task chdirs to `server/`, hence the
`./internal/...` package path. The safety test is
build-tagged (`demoseed_safety`): running the package without the tag reports
"no test files" — that is expected, not a broken package.

## The two scripts

- **postgres.sql** installs and executes `demo.ensure_demo_org()`, a single
  idempotent plpgsql function. Deliberately NOT a migration: migrations are
  append-only and the seed churns; `CREATE OR REPLACE` + daily rerun is the
  upgrade path. It deletes demo data first (projects delete cascades most of
  it; org-scoped tables are deleted explicitly), then reinserts. Preflight
  asserts abort if the demo constants collide with anything that is not the
  demo org; postflight asserts fail the run on missing or leaked rows.
- **clickhouse.sql** runs scoped `ALTER TABLE … DELETE` on the telemetry
  source, **every MV target** (enumerate them by grepping `MATERIALIZED
VIEW` in `server/clickhouse/schema.sql`), and the org-keyed tables, then
  inserts fresh rows; the MVs repopulate summaries on INSERT. Postflight
  `throwIf` statements at the bottom of the script fail the run — the
  Postgres postflight `RAISE` asserts likewise sit at the end of
  `ensure_demo_org()`. The Go runner splits statements on `;` — keep
  semicolons out of string literals.

Fixed identity: org `org_gram_demo_workspace` (slug `acme-demo`), single
project `dec0de00-0000-4000-a000-000000000001`, users `user_demo_*` /
`*@demo.getgram.ai`. Deterministic uuids come from
`demo.det_uuid('gram-demo-<thing>-' || n)` (md5 with version nibble `5`,
variant `8`) — ClickHouse reproduces the same formula so rows join across
stores. det_uuid inputs only need globally unique strings: pick a fresh
`gram-demo-<thing>-` prefix per row family and never reuse one across
families. Timestamps are always `now()`-relative (trailing ~12 days).

## Rules the safety test enforces

`TestDemoSeedSafety` provisions a fake "customer" tenant by running a
transformed copy of the seed itself (every demo identifier string-rewritten),
snapshots both databases, runs the real seed twice with tampering in between,
and fails on:

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
   add a postflight assert on its expected demo row count (Postgres `RAISE`
   / ClickHouse `throwIf`), following the existing ones.
3. **Telemetry rows missing the `demo-seed` marker.** Every
   `telemetry_logs` insert must carry
   `'{"gram.deployment.id":"demo-seed"}'` in `resource_attributes`; the
   seed's own postflights fail otherwise.
4. **New identifier families the test cannot rewrite.** If you introduce a
   new class of globally unique identifier (a new uuid prefix, a new
   `something_demo_*` id written to a globally-unique column, a new email
   domain), add a rewrite pair to `otherTenantReplacements` in
   `server/internal/demoseed/safety_test.go`. Miss it and the test fails as a
   unique-constraint violation during seeding, or with
   `postgres.sql lost literal %q; update otherTenantReplacements` after a
   rename.
5. **New ClickHouse tables with unrecognized scoping columns.** The test
   infers demo scope from `organization_id` / `gram_project_id` columns. A
   demo-written table using a different column name fails the isolation check
   (fail-safe false positive) — name the scoping column one of those two.

## Iteration loop

1. Edit the SQL; keep every statement scoped and every insert cleaned up.
2. `mise run seed:demo` against the local stack; verify affected dashboard
   pages per `seed/demo/verify.md`; update `seed/demo/PAGES.md`.
3. `mise run test:server -tags=demoseed_safety ./internal/demoseed/...`
   (~1 min with containers). Fix anything it names — the failure message
   includes the exact table.
4. Commit. Prod picks the change up on the next release promotion; the daily
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
