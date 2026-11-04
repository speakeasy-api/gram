# Internal Audit Events Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist explicit internal-event classification, retain staff visibility, and suppress customer reads and all outbox publication.

**Architecture:** Add the same default-false boolean to both PostgreSQL audit tables and generated query contracts. Classify five existing writers; enforce publication at the shared publication boundary and visibility in SQL. Reuse existing staff API, MCP tools, UI, and standalone operator-command conventions.

**Tech Stack:** PostgreSQL/Atlas/SQLc, Go/Goa, Admin MCP, React/TypeScript, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-04-internal-audit-events-design.md`

## Global constraints

- `is_internal BOOLEAN NOT NULL DEFAULT false`; no actor-, staff-, or subject-based inference in live writes/reads.
- Internal rows remain durable in the mutation transaction; no outbox consumer receives them, including growth signals.
- `WithoutOutbox` remains independent: non-internal backfills can suppress publication.
- No customer opt-in, staff toggle, classification registry, event-catalog deletion, webhook retraction, or transitional subject exclusion.
- Migrations are generated DDL only and ship in a separate schema-only PR before application deployment. Backfill is manual after deployment, never a migration/startup job.
- No customer identifiers or credentials in committed examples. Preserve existing authorization, assistant filtering, ordering, pagination, and result-safety behavior.

## Skills and execution prerequisites

Load `ponytail`, `postgresql`, `.agents/skills/gram-audit-logging/SKILL.md`, `maintaining-admin-mcp`, `maintaining-platform-mcp`, `writing-skills`, `vercel-react-best-practices`, `gram-demo-seed`; use `test-driven-development` and `verification-before-completion` during execution. Read repository UI conventions before the badge change; no new navigation/resource discovery is introduced. Use `pull-request` for the separate PRs and `pull-request-demo` for the UI PR evidence.

SQLc requires the development PostgreSQL instance; Atlas diff uses its configured Docker dev database. Establish prerequisites only with authorization. Task checkboxes below are the original execution checklist, not a deployment completion record.

## Findings and file boundaries

- `server/internal/audit/securityevent/queries.sql` mirrors configuration query types; `securityevent/securityevent.go` directly converts insert params/rows and calls exported `audit.PublishWebhook`. Guarding only `Logger.log` would leave this bypass open.
- `server/internal/audit/queries.sql` has the PR's literal exclusion in the list plus actor/action/surface facets. Security list/actor queries mirror it. Replace every corresponding exclusion, not unrelated workload inventory queries.
- `server/internal/admin/impl.go` already passes `IncludeInternalEvents: true`. Keep this server-only switch; customer mapping in `server/internal/auditapi/impl.go` must never set it.
- PR-only logger construction is in `server/internal/catalogpromotion/curation.go` and `retry.go`.
- Existing operator command precedent: `server/cmd/directory-id-backfill/{main.go,main_test.go,README.md}` (stdlib flags, pgx, explicit apply). Do not reuse its WorkOS-specific logic.
- Admin page implementation/tests: `client/admin/src/pages/organization/Activity{,.test}.tsx`; route is only a wrapper. SDK fields are camelCase.

### Task 1: Expand storage, separately deploy schema

**Files:** modify `server/database/schema.sql`; generate `server/migrations/<Atlas-generated timestamp>_internal_audit_events.sql` and `server/migrations/atlas.sum`. No application/backfill changes in this PR.

**Produces:** identical `is_internal` columns on `audit_logs` and `security_audit_logs`.

- [ ] Establish schema expectations before editing: a transaction inserting an existing valid fixture without the new field must return false; explicit true must round-trip; null must fail for each table. Reuse fixtures in `server/internal/audit/list_test.go` and `server/internal/audit/securityevent/securityevent_test.go` for the later application regression, not as business code in the schema PR.
- [ ] Add this column inside both table declarations:

```sql
is_internal boolean NOT NULL DEFAULT false,
```

- [ ] Run `mise run db:diff internal_audit_events`, then `mise run lint:migrations`. Review generated DDL contains only two additive columns, no DML or unrelated changes. Never hand-edit generated migration/hash files.
- [ ] Commit/review the schema-only change; deploy it before any new application binary. Existing rows default false; no historical visibility change yet.

### Task 2: Persist classification and close publication bypass

**Files:** modify `server/internal/audit/{queries.sql,logger.go,outbox.go,catalogpromotions.go,mcpservers.go,logger_test.go}`, `server/internal/audit/securityevent/{queries.sql,securityevent.go,securityevent_test.go}`, `server/internal/catalogpromotion/{curation.go,retry.go}`; generate both audit `repo/` packages.

**Consumes/produces:** both SQLc insert params expose `IsInternal bool`; five writer methods set it explicitly; `PublishWebhook` keeps its existing signature.

- [ ] Add `TestLogger_InternalEvents` covering `LogCatalogPromotionArchive`, `Reject`, `Restore`, `RetryDocs`, and `LogMcpServerPromoteCatalogue`, each using ordinary `audit.NewLogger()`. Reuse existing actor/organization fixtures. Assert persisted true and zero outbox records after each action. Add normal-event false/publication and `WithoutOutbox` false/no-publication assertions to existing logger tests. Add a direct-publication regression so an internal security insert also cannot publish.

```go
require.True(t, row.IsInternal)
require.Zero(t, outboxCount)
// Existing WithoutOutbox test must also assert:
require.False(t, row.IsInternal)
```

- [ ] Run `mise run test:server ./internal/audit/... -run 'TestLogger|Test.*Internal' -count=1`; first expect missing-field compilation failure, then behavioral failures until guards/writers land.
- [ ] Append `is_internal` / `@is_internal` to both insert column/value lists in identical order. Add `is_internal` to both list SELECT projections so response mappings receive the persisted value. In `MoveKitRuntimeAuditLogs`, add `is_internal` to the target INSERT columns and moved-row SELECT, preserving classification instead of resetting it to false. Extend `TestMoveKitRuntimeAuditLogs` with true and false source rows and assert each moved row retains its value. Keep both generated type shapes convertible. Run `mise run gen:sqlc-server`.
- [ ] Set `IsInternal: true` in the five writers' `repo.InsertAuditLogParams` literals only. Preserve ordinary MCP-server writers. In `Logger.log`, retain insert-before-suppression and use:

```go
if l.skipOutbox || entry.Params.IsInternal {
    return nil
}
```

At the start of exported `PublishWebhook`, independently protect its direct security caller:

```go
// Internal entries are persisted but never reach any outbox consumer.
if input.IsInternal {
    return nil
}
```

No new logger interface or registry. The extra shared-boundary guard is intentional defense against the actual direct caller, not a new abstraction. Security events currently default false; their future true entries must remain safe.

- [ ] Replace the two catalog-promotion `audit.NewLogger(audit.WithoutOutbox())` special cases with `audit.NewLogger()`. Update logger/security publication comments; retain operator-backfill usage elsewhere.
- [ ] Re-run the focused tests plus `mise run test:server ./internal/catalogpromotion -count=1`; commit application changes after green tests.

### Task 3: Flag-based customer exclusion and required API response

**Files:** modify `server/internal/audit/{queries.sql,list_test.go}`, `server/internal/audit/securityevent/{queries.sql,securityevent_test.go}`, `server/internal/auditapi/impl.go` and its existing tests, `server/internal/admin/{impl.go,listorganizationactivity_test.go}`, `server/design/auditlogs/design.go`; regenerate SQLc, `server/gen/`, SDK/OpenAPI outputs via tasks.

**Produces:** required `AuditLog.IsInternal bool`, serialized `is_internal`, SDK `isInternal`; staff-only query inclusion stays private.

- [ ] Add customer list/facet regressions with internal and non-internal rows sharing a subject type, including non-internal `catalog_promotion` and internal `mcp_server`. Assert exclusion under action, subject type, subject ID, and subject-ID-list filters; check actor, action, and surface facets. Repeat security mirror coverage. Staff mixed-row pagination must include both without changing cursor order. Assert customer serialized false and staff true/false.
- [ ] Run `mise run test:server ./internal/audit/... ./internal/auditapi ./internal/admin -run 'Test.*(List|Facet|Internal)' -count=1`; expect old subject predicates to fail the mixed-classification cases.
- [ ] Replace list predicate with the existing private opt-in:

```sql
AND (@include_internal_events::boolean OR NOT a.is_internal)
```

Replace literal subject exclusions in customer facets with `AND NOT is_internal` (use table alias where required). Preserve assistant predicates and all tenant constraints. Do the equivalent for security mirror list/actor queries.

- [ ] Add required Goa attribute in `AuditLog` and map `IsInternal: row.IsInternal` in customer and staff response construction:

```go
Attribute("is_internal", Boolean, "Internal staff-only audit activity; persisted but never published to the outbox.")
Required("is_internal")
```

Keep the existing Required fields too. Customer inputs gain no flag. Platform MCP assessment: keep its existing customer API boundary; no tool allowing internal opt-in. Add a regression on the existing Platform MCP customer audit route if it exposes this service; otherwise record that audit retrieval is not an exposed tool rather than inventing one.

- [ ] Run `mise run gen:sqlc-server`, `mise run gen:goa-server`, `mise run gen:sdk`; inspect generated changes, do not edit generated code. Re-run focused tests and `mise run test:server ./internal/platformmcp -count=1`. Commit green read/API changes.

### Task 4: Explicit MCP booleans and staff badge

**Files:** modify `server/internal/adminmcp/{tool_activity.go,tool_activity_test.go,tool_audit_logs.go,tool_audit_logs_test.go}` and output-schema assertions in `tool_list_schema_test.go` where applicable; modify `client/admin/src/pages/organization/Activity{,.test}.tsx`. Use generated SDK from Task 3.

**Consumes:** `gen.AuditLog.IsInternal` / SDK `AuditLog.isInternal`.

- [ ] Extend both tool fixtures with true and false rows; assert raw JSON contains the field even for false, output schema requires a boolean, and existing staff auth/safe-summary assertions remain unchanged:

```go
require.Contains(t, body, `"is_internal":false`)
require.Contains(t, body, `"is_internal":true`)
```

Add page tests with one internal row and one ordinary row, expecting one visible `Internal` label, and none when only ordinary rows are rendered.

- [ ] Run `mise run test:server ./internal/adminmcp -run 'Test.*(Activity|AuditLog|Schema)' -count=1` and `aube run -F admin test src/pages/organization/Activity.test.tsx`; expect missing-field/label failures.
- [ ] Add to both `OrganizationActivity` and `AuditLogEntry`, with no `omitempty`, then map from `log.IsInternal`:

```go
IsInternal bool `json:"is_internal" jsonschema:"Internal staff-only activity; persisted but never published to the outbox"`
```

Update both tool descriptions/contracts to explain this classification without widening authorization or returning snapshots in the summary tool.

- [ ] Render an existing UI-styled badge next to the row's action text; no toggle/filter or new component abstraction:

```tsx
<>
  {log.isInternal ? (
    <span className="bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-xs">
      Internal
    </span>
  ) : null}
</>
```

Use the existing row variable and nearby badge conventions. Keep visible label accessible as text. No Command K change: existing route/resource set is unchanged.

- [ ] Re-run both focused suites and `aube run -F admin type-check`; commit green MCP/UI changes.

### Task 5: Manual bounded, idempotent backfill

**Create:** `server/cmd/internal-audit-events-backfill/{main.go,main_test.go,README.md}`. No migration, daemon, startup hook, or new dependency.

**Interface:** standalone Go command; `--before` required RFC3339 cutoff; `--batch-size` default 500 (1–5000); `--max-batches` default 20 (positive); `--apply` default false. Use `GRAM_DATABASE_URL`, never print it. Fixed two-table allowlist; no user-supplied SQL/table names. Dry-run performs counts only, no updates.

- [ ] Add tests for flag rejection; dry-run no writes; both tables; historical `catalog_promotion` plus exact `mcp-server:promote-catalogue`; unrelated `mcp_server` action unchanged; second run updates zero; batch/max-batch/cutoff bounds; unchanged timestamps/data and outbox counts. Use existing test database-clone patterns, not production connections.
- [ ] Run `mise run test:server ./cmd/internal-audit-events-backfill -count=1`; expect failure before implementation.
- [ ] Use stdlib flags and pgx as in the directory command. Count eligible false rows before and after each table; report scanned eligibility, rows updated, batches and remaining. Use a fixed cutoff, one short transaction per batch, bounded context and local lock/statement timeout. Exit nonzero on database/report errors; explicitly report if previous batches committed. A bounded run may leave rows; report this and rerun rather than silently claiming completion.

Core SQL for `audit_logs` (repeat with the fixed `security_audit_logs` identifier, not arbitrary interpolation):

```sql
WITH batch AS (
  SELECT id FROM audit_logs
  WHERE NOT is_internal AND created_at < $1
    AND (subject_type = 'catalog_promotion'
         OR action = 'mcp-server:promote-catalogue')
  ORDER BY created_at, id
  LIMIT $2
  FOR UPDATE
)
UPDATE audit_logs a SET is_internal = true
FROM batch b WHERE a.id = b.id AND NOT a.is_internal;
```

`SET` changes only the flag. Stop at zero affected or max batches per table; no logger/outbox calls and no historical outbox modifications. Use the same eligibility predicate for before/after counts; no `SKIP LOCKED` that could falsely suggest completion. Short lock timeout makes contention an explicit retryable operator error.

- [ ] Document deployment order: schema → application → inspect dry-run → approved manual apply → verify/rerun. Example, from `server/`:

```sh
GRAM_DATABASE_URL='<DATABASE_URL>' mise exec -- go run ./cmd/internal-audit-events-backfill --before '<RFC3339_CUTOFF>' --batch-size 500 --max-batches 20
# Add --apply only after reviewing counts and deployment state.
```

Historical rows remain visible until classified; delivered webhooks/outbox history are not retracted. Preserve counts and cutoff as operator evidence, without recording tenant identifiers in git.

- [ ] Re-run package tests; commit command/runbook. Never run apply as part of implementation verification against a shared environment.

### Task 6: Guidance, demo assessment, final verification

**Files:** modify `.agents/skills/gram-audit-logging/SKILL.md`; synchronize harness links with `.mise-tasks/skills/sync.sh`. Assess `server/internal/demoseed/` fixtures, change only if existing staff fixtures should exhibit the badge.

- [ ] Replace statements that both logs are universally customer-readable/publish by default with explicit exceptions:

> Set `IsInternal: true` at the writer only for staff-only activity. Persist the row in the mutation transaction; exclude it from customer lists and facets and suppress every outbox consumer. Do not infer internal status from staff actors or subject types. Staff changes to customer resources usually remain non-internal. `WithoutOutbox` suppresses publication for operator backfills without changing classification or visibility.

Document all five initial actions, shared `PublishWebhook` guard, mirror-schema requirements, required boolean contracts, and the writer/read/facet/MCP/badge/backfill tests above. Keep existing outbox event definitions intact.

- [ ] Demo assessment: ordinary customer seed rows retain false defaults; do not invent customer catalog activity. If existing staff catalog fixtures are present, explicitly classify them in their existing insert paths. Otherwise record exclusion: no appropriate staff activity fixtures, covered by focused admin tests. No broad demo-seed rewrite.
- [ ] Run `mise run skills:sync`; inspect only intended links/files. Run `git diff --check`, focused suites from Tasks 2–5, and `aube run -F admin type-check`. Run `mise run lint:brand` for new prose. Record unavailable infrastructure rather than claiming success.
- [ ] Review generated contracts for required booleans and no customer opt-in. Search modified audit queries for obsolete literal exclusions; ensure `WithoutOutbox` remains for actual operator use. Commit docs after checks; prepare separate schema and dependent application PRs following safeguards.

## Self-review against spec

Storage/mirror and default → Task 1/2; five writers, transaction persistence, all-consumer suppression and shared bypass → Task 2; all customer filters/facets, mixed subjects, staff pagination, required API bool → Task 3; both MCP tools explicit false/true and UI badge → Task 4; bounded manual historical classification including MCP promotion, timestamp preservation and no publication → Task 5; skill synchronization/demo assessment → Task 6. No transitional exclusion, automatic backfill, inferred classification, or event-bus abstraction. No product requirements omitted.

## Unresolved questions / blockers

- No product-design questions. Schema/application must be separate PRs and deployed in order under repository rules.
- Execution needs existing local database/Docker/codegen prerequisites; planning did not start or validate these services.
