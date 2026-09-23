# Signals intelligence

Status: implemented and verified locally. Production deployment remains separate.

## Context

Customers define data labels (signals), grouped into sensors. The eventual runtime classifies user and assistant messages separately, using Jev. **This implementation is limited to CRUD APIs for custom signals and sensors**, not message processing or UI.

Examples: satisfaction → happy/unhappy; marketing campaign → named campaigns; service → api/database/worker/auth (manually defined initially).

## Findings

- User confirmed `sigint_target` and `sigint_classifier` both mean **sensor**. Use sensor throughout the schema/API; neither is a separate entity.
- The user explicitly marked the uncommitted schema draft as **throwaway exploration**, potentially incomplete or incorrect. It is not a contract or a schema to repair incrementally. Discard its sigint additions during implementation and design from the agreed requirements and repository conventions; preserve unrelated schema/worktree changes. No compatibility, naming, field, or migration obligations attach to the draft.
- [Jev API](https://docs.typesafe.ai/api#question-types): `noul` returns independent yes probability; `choice` returns an exclusive distribution over up to 255 options; `score` returns an ordinal expected value over 2–10 levels. Per-signal probability differs from question-level `confidence`. These inform configuration, not an inference implementation in this change.
- Existing management patterns cover this scope: Goa HTTP-RPC, project authorization, SQLc, model views, ordered many-to-many membership, and atomic mutation/audit transactions.
- PostgreSQL migrations are Atlas-generated and ship separately, before the application change.

## Approach

### Scope and model

Build one `sigint` management service. Custom signals form a reusable **project-scoped catalog**; sensors attach signals from that same project.

| Table                   | Domain fields                                                            |
| ----------------------- | ------------------------------------------------------------------------ |
| `sigint_custom_signals` | `name`, optional `description`, optional `classifier_criteria`           |
| `sigint_sensors`        | `name`, optional `description`, optional `instructions`, required `mode` |
| `sigint_sensor_signals` | `sensor_id`, `signal_id`, `sort_order`                                   |

All three: UUIDv7 `id`, non-null `project_id` referencing `projects`, `created_at`, `updated_at`, `deleted_at`, generated `deleted`. Omit integration lineage and generic attributes: neither has a requirement in the agreed CRUD scope. IDs are authoritative; display names are not identifiers and need not be unique. Trim names; require 1–200 characters.

Membership rules:

- Composite `(project_id, sensor_id)` and `(project_id, signal_id)` foreign keys pin both references to the same tenant; add non-partial unique `(project_id, id)` indexes on the parent tables.
- Partial unique index prevents duplicate active `(project_id, sensor_id, signal_id)` memberships. Index active sensor ordering and reverse signal lookup; index parent project/ID lists.
- Follow PostgreSQL skill: named constraints, plural tables, explicit `ON DELETE SET NULL`, enums validated in application code. With non-null ownership/reference columns, physical deletion requires explicit child-first cleanup; API deletion is always soft.

### Three modes

| API `mode`      | Meaning                                              | Future Jev mapping                      |
| --------------- | ---------------------------------------------------- | --------------------------------------- |
| `multi_label`   | Zero, one, or several labels can independently apply | One `noul` question per attached signal |
| `exclusive`     | Signals compete; probabilities sum to one            | One `choice` question                   |
| `ordered_score` | Ordered levels define a low-to-high numeric scale    | One `score` question                    |

- Store order **on membership**, not the shared signal. Preserve it in every mode; in ordered-score mode array index defines level 0, 1, ….
- Sensor instructions frame the question; signal criteria define each label/level. Both may be absent while authoring.
- Empty and one-signal sensors are legal drafts. No `enabled`, activation, readiness endpoint, provider call, or inference-validity gate.
- Reject unknown modes, repeated IDs, and missing/deleted/cross-project references. Validate the final sensor configuration atomically: exclusive max 255 members, ordered-score max 10. Switching modes obeys the same maxima; runnable minimum counts are deferred.
- No implicit neutral/none signal. Customers can explicitly attach one for exclusive classification.

### API contract

Session and producer API-key authentication with the existing project header. Never accept ownership from a body-supplied project ID.

| Resource | Endpoints under `/rpc/sigint.`                                             |
| -------- | -------------------------------------------------------------------------- |
| Signal   | `createSignal`, `getSignal`, `listSignals`, `updateSignal`, `deleteSignal` |
| Sensor   | `createSensor`, `getSensor`, `listSensors`, `updateSensor`, `deleteSensor` |

- GET for get/list; POST for create/update; DELETE with `id` query parameter for deletes. Use standard Gram errors and Goa operation/SDK/hook metadata.
- Signal model returns ID, project ID, domain fields, timestamps. Sensor model adds required ordered `signal_ids` (empty array when unattached); no expanded duplicate signal definitions or public membership resource.
- Sensor create/update accepts ordered `signal_ids`. This one operation handles attach, detach, replace, and reorder transactionally; no additional membership endpoints.
- Create requires signal `name`, or sensor `name` and `mode`; omitted membership means empty.
- Partial updates: omitted fields remain unchanged; supplied text `""` clears optional text; supplied `signal_ids` replaces the ordered set (`[]` clears). JSON null has omission semantics, not a second clearing mechanism. Ensure generated Go/SDK handling preserves omitted versus empty collections.
- Lists: `limit` default 50, range 1–200, UUID cursor, deterministic ID ordering, `next_cursor` only when more rows exist. Return active resources only. Avoid per-sensor membership queries; load ordered IDs in the same SQL statement/snapshot.
- Reads require existing `project:read`; mutations require `project:write`, checked before DB mutation. Foreign-project IDs resolve as unavailable/not-found, never disclose ownership. No new RBAC scopes.

### Lifecycle, concurrency, and audit

- Signals are live shared definitions, not copies or pinned versions. Editing a signal is reflected wherever its ID is attached.
- Sensor updates retain unchanged membership rows, reorder retained rows, soft-delete removed rows, and insert new links. Normalize order to consecutive zero-based positions.
- Deleting a sensor soft-deletes its links but **never deletes shared signals**.
- Deleting a signal soft-deletes it and every active membership in the project, updates each affected sensor's `updated_at`, and compacts surviving order. Sensors may become incomplete; do not reject the deletion.
- Serialize configuration mutations using one transaction-scoped advisory lock per project, namespaced to sigint. Acquire before reading mutation state. This low-volume CRUD path avoids attach-versus-delete races and inconsistent audit snapshots without a complex cross-resource lock order. No lock on read paths.
- Use one transaction for state, membership changes, audit rows, and audit outbox events. Add typed signal/sensor URNs and audit subjects; create/update/delete events follow existing conventions.
- On signal deletion, record one sensor update with before/after ordered memberships per affected sensor plus the signal delete event. Membership rows are not independently audited resources.
- Missing/already-deleted IDs return not-found; no restore endpoint. Audit failure rolls back the mutation.

### Seed and scope exclusions

Seed all three modes in `server/internal/demoseed/postgres.sql`, including a shared signal and an incomplete sensor. Use `demo.det_uuid('gram-demo-sigint-…')`; existing `Spec.NameSeed` rewriting already covers this namespace. Add project-scoped child-first deletes and postflight checks.

**Not included:** system-signal tables/integrations, Jev client or credentials, hook changes, queues, classification jobs, ClickHouse results, thresholds, OTel metering, historical scoring, activation, dashboard, or new CLI UX. The eventual runtime will classify user and assistant messages separately. Versioned inference snapshots and historical interpretation must be designed when results are introduced, not simulated here.

## Files to modify

- **Schema-only delivery:** `server/database/schema.sql`; Atlas-generated `server/migrations/<timestamp>_sigint_configuration.sql` and `atlas.sum`. Discard the throwaway sigint additions and author the three-table model above afresh; validate its constraints/indexes independently of the draft.
- **Design:** new `server/design/sigint/design.go` and resource model files as needed; registration in `server/design/gram.go`.
- **Service/persistence:** new `server/internal/sigint/{impl.go,queries.sql,setup_test.go,*_test.go}`; `server/database/sqlc.yaml`; attach in `server/cmd/gram/start.go`.
- **Views/identity:** new `server/internal/mv/sigint.go`, `server/internal/urn/sigint_signal.go`, `server/internal/urn/sigint_sensor.go`.
- **Audit:** new `server/internal/audit/sigintsignals.go` and `sigintsensors.go`; `server/internal/audit/events.go`; `server/internal/outbox/events/audit_log.go`.
- **Fixtures:** `server/internal/demoseed/postgres.sql`. No `Spec` change needed if IDs use its existing `NameSeed` namespace.
- **Generated:** SQLc repo, Goa service/HTTP/CLI/OpenAPI, webhook catalogs, internal/public OpenAPI outputs and TypeScript SDK. Use generators; never edit artifacts by hand.
- **Release note:** a server-minor changeset describing the new CRUD API. No dashboard/RBAC-page docs change: no new page or scope.

## Reuse

- `server/design/mcpservers/design.go`: session/API-key/project security composition, payload/header helpers, HTTP error declarations, SDK metadata.
- `server/internal/mcpservers/impl.go`: `Attach`, `APIKeyAuth`, create/get error mapping and transaction conventions. Use direct `db` injection and `repo.New(dbtx)` for the new service.
- `server/internal/toolsets/impl.go:UpdateToolset`: partial updates and collection-presence semantics; do not copy its unrelated runtime side effects.
- `server/database/schema.sql:meta_mcp_server_members`: ordered, soft-deleted many-to-many shape and tenant-pinning indexes. Apply current FK conventions rather than copying its cascading deletes.
- `server/internal/mcpservers/queries.sql:LockMCPServerToolMetadataWrite`: transaction-scoped advisory-lock pattern for collection mutations/audit consistency.
- `server/internal/deployments/impl.go:ListDeployments`, `server/design/shared/pagination.go:CursorPagination`: UUID cursor and SDK pagination conventions.
- `server/internal/environments/impl.go`, `server/internal/authz/scopes.go`: project-read enforcement and existing project-read/write vocabulary.
- `server/internal/audit/mcpservers.go`, `server/internal/urn/mcp_server.go`, `server/internal/outbox/events/audit_log.go`: typed audit snapshots/URNs/versioned event definitions.
- `conv`, `o11y`, `oops`, `testenv`, `authztest`, `audittest`: conversions, rollback/error handling, isolated fixtures, grant setup, audit assertions.
- `server/internal/demoseed/{postgres.sql,spec.go}`: deterministic retargetable fixtures, cleanup, and postflight assertions.

## Steps

- [x] **Schema first:** discarded the throwaway sigint draft, preserving unrelated work; authored the agreed three-table model; verified it locally and regenerated `20260923152815_sigint_configuration.sql` with Atlas after updating to current main. Deliver migration separately and roll it out before app code. No DML in migrations.
- [x] **API/persistence:** added Goa models and ten methods, SQLc registration/queries, ordered membership writes, project advisory lock, pagination, and model views.
- [x] **Service integration:** implemented all operations with project authorization and atomic audit events; added URNs/outbox definitions; wired the HTTP service.
- [x] **Fixtures and generated contract:** added three-mode demo/local fixtures; generated SQLc, Goa, webhook catalogs, and SDKs with spec upload and versioning disabled; exercised SDK omitted-versus-empty collection serialization.
- [x] **Prove behavior:** exercised real HTTP CRUD, authentication, pagination, mode boundaries, and concurrent attach/delete; passed focused server tests, seed safety, build, and lint; added `.changeset/signals-intelligence-crud.md`.

## Verification

Planning evidence: read-only schema diff, source inspection, and Jev API documentation. No implementation, services, migrations, inference calls, or tests run during planning.

During implementation:

1. Apply the generated migration **locally**. Check migration lint; run generators against the migrated local schema.
2. Smoke through actual HTTP with the local API:
   - Create reusable custom signals; create a sensor in each mode and one empty sensor.
   - Get/list/update resources; rename a shared signal and resolve it through two sensors.
   - Reorder ordered-score membership; verify exact round-trip order.
   - Omit membership on a metadata update, then send `[]`; prove preserve versus clear through the generated transport.
   - Delete a shared signal; verify removal from every sensor, preserved relative order, compaction, and correct audit snapshots. Delete a sensor; verify remaining signals survive.
   - Verify cursor pagination, session/API-key authentication, denied grants, and cross-project reads/attachments.
3. Keep focused regressions for plausible bugs: tenant isolation, invalid-member all-or-nothing writes, collection omission/clear semantics, mode-size transitions, shared-delete ordering/audits, and concurrent attach/delete with no surviving active link to a tombstone. Use black-box service tests and existing `testenv`/SQLc fixtures; include a transport-level check for JSON collection presence.
4. `mise run test:server ./internal/sigint/... ./internal/audit/... ./internal/urn/...`; `mise run build:server`; `mise run lint:server`; `hk fix` then rerun any checks affected by formatting.
5. Run local `mise run seed` twice; verify seed resources through CRUD reads. Run `mise run test:server -tags=demoseed_safety ./internal/demoseed/...`. Confirm existing identifier-rewrite checks still pass.

### Completed evidence

- Atlas migration applied locally; isolated migration lint passed with 10 schema changes and no diagnostics. Migration ordering and transaction-safety checks passed.
- Focused sigint/audit/URN suite: 664 tests passed after the final fixes. Seed-safety suite: 40 tests passed.
- Real HTTP exercised all ten methods, producer API keys and OIDC sessions, unauthenticated denial, Unicode name bounds, shared edits/deletion, membership omission/null/empty/reordering, both cursor lists, exclusive 255/256 and ordered-score 10/11 boundaries, and eight concurrent attachments racing deletion.
- Service regressions cover denied grants, cross-project reads/attachments, atomic invalid-member rejection, audit-failure rollback, deletion snapshots, and concurrent membership integrity.
- Local/demo seed ran twice. HTTP confirmed 9 signals, 4 sensors, all modes, shared signals, and an empty draft. Smoke resources were removed; the same active fixture counts remained.
- Generated SDK serialization preserves omitted membership versus `[]`. Webhook catalog check, server build, `hk fix`, and full server lint passed. Final build/lint ran sequentially with bounded Go resources after the first lint process was killed.
- Normal local startup has an existing incomplete Stripe catalog. HTTP verification used the supported `--stripe-api-key unset` local stub without changing credentials or billing code; the temporary server was stopped.

No browser verification or classifier evaluation is claimed or required: neither surface changes in this scope.

## Relevant skills

Implementation: `postgresql`, `golang`, `gram-management-api`, `gram-rbac`, `gram-audit-logging`, `gram-demo-seed`. Use `pitchfork` when starting local services for HTTP proof. Pub/Sub/Python skills were consulted for context only; those runtimes remain unchanged.

## Unresolved questions

None. Apply the schema migration separately before the application rollout.
