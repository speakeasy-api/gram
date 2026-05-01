---
name: gram-audit-logging
description: Concepts, external interfaces, and conventions for Gram's audit logging subsystem — the internal Go API for recording actor/action/subject events and the `/rpc/auditlogs.*` management API that exposes them. Activate whenever the task involves recording or exposing audit events (adding or changing audit coverage on a service, introducing a new audited subject or action, writing tests that assert an event was recorded, changing how entries are displayed or filtered).
metadata:
  relevant_files:
    - "server/internal/audit/**/*.go"
    - "server/internal/audit/**/*.sql"
    - "server/design/auditlogs/**"
---

Audit logging is how Gram records _who did what to which resource_. Every meaningful mutation on a project- or org-scoped resource is expected to produce one audit entry per affected row, written inside the same database transaction as the mutation so events can't drift from the state they describe. Entries are exposed to Gram users through the `auditlogs` management API.

## Concepts and terminology

**Actor.** The principal that caused the event — a `urn.Principal` carrying a type (user, role, service account) and an id, with optional display name and slug for human rendering.

**Subject.** The resource the event is about — identified by a subject type (e.g. `remote_mcp_server`, `access_role`) and a subject id.

**Action.** What happened to the subject. Each subject declares its own set of actions, typically one per mutating verb on the subject's life cycle.

**Before/after snapshot.** Optional opaque JSON payloads describing the subject's state. Populated on updates, left empty on creates and deletes unless the snapshot is independently useful.

**Metadata.** Optional JSON bag for contextual fields that are not part of the subject's state.

**Scoping.** Every entry belongs to exactly one organization and zero or one projects. Org-scoped subjects (roles, members) carry no project id; project-scoped subjects carry the project UUID.

**Atomicity.** Audit entries are written inside the same database transaction as the mutation they describe, so the state and the record of the state commit together or not at all.

## Server

Audit logging lives in `server/internal/audit/` with its management-API surface defined in `server/design/auditlogs/`. Callers are other services whose handlers emit events; the `auditlogs` Goa service reads them back.

### Conventions

**Where types live.** The package-private `subjectType` string type and every `subjectType*` constant live in `server/internal/audit/events.go`. The public `Action` string type and the `marshalAuditPayload` snapshot helper live in the same file. The subject-type const block is kept alphabetised.

**Subject type naming.** Subject type values are short snake_case strings (e.g. `remote_mcp_server`, `access_role`). Constants follow the `subjectType<Name>` pattern.

**Action naming.** Values follow `<subject-slug>:<verb>` (e.g. `remote-mcp:create`, `access_role:update`). Verbs are typically `create` / `update` / `delete`; subjects may add feature-specific verbs (`toolset:attach_oauth_proxy`).

**Subject files.** One Go file per subject under `server/internal/audit/`, named after the subject in plural form (e.g. `remotemcpservers.go`, `toolsets.go`). Each file owns that subject's `Action*` constants, its `Log*Event` payload structs, and its `Log*` functions. Do not merge subjects into a shared file.

**`Log*Event` and `Log*` naming.** One `Log<Verb>Event` struct plus one `Log<Verb>` function per action, declared in the subject file. `Log*` functions take `(ctx, dbtx repo.DBTX, event Log*Event) error` so the audit insert is atomic with the caller's mutation.

**Subject identifier fields.** Event structs carry the subject's identifier as a URN type, not a raw `uuid.UUID`. Field name is `<Subject>URN` (e.g. `KeyURN urn.APIKey`, `McpServerURN urn.McpServer`), and the `Log*` function populates `SubjectID` from `event.<Subject>URN.ID.String()`. If no URN type exists yet, add one under `server/internal/urn/` before introducing the event struct — see `server/internal/urn/api_key.go` for the template.

**Snapshot fields.** Update event structs declare snapshot fields as `<Subject>SnapshotBefore` / `<Subject>SnapshotAfter` with concrete pointer types (e.g. `*types.Toolset`, `*types.McpServer`). Do not use `any` or bare `SnapshotBefore` / `SnapshotAfter` — the typed form keeps `marshalAuditPayload` callers honest about the shape being persisted. Pass the view through directly unless a specific field on the type needs stripping for size or sensitivity reasons (see `toolsets.go` for the one clone-and-strip case).

**Per-row events for bulk mutations.** A single bulk SQL statement that touches N rows of an audited subject produces N audit entries — one per row — not one entry that covers the batch. This is what makes the audit log a faithful reconstruction of each subject's life cycle and what lets `auditlogs.list` filter to a specific subject id. The most common place this gets missed is cascading soft-deletes that fan out from a parent delete; see "How to audit a cascading delete of child resources" under "Jobs to be done".

### Non-generated files

| File                                                                 | Purpose                                                                                                                                                                                                                |
| -------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `server/design/auditlogs/design.go`                                  | Goa design for the `auditlogs` service. Regenerates `server/gen/auditlogs/` and `server/gen/http/auditlogs/` via `mise run gen:goa-server`.                                                                            |
| `server/internal/audit/<subject>.go`                                 | One file per subject (e.g. `access.go`, `remotemcpservers.go`, `toolsets.go`).                                                                                                                                         |
| `server/internal/audit/audittest/helpers.go`                         | Test helpers other packages use to assert audit events.                                                                                                                                                                |
| `server/internal/audit/audittest/queries.sql`                        | SQLc queries backing the test helpers. Regenerates `server/internal/audit/audittest/repo/` via `mise run gen:sqlc-server`.                                                                                             |
| `server/internal/audit/events.go`                                    | Top-level declarations shared across every subject.                                                                                                                                                                    |
| `server/internal/auditapi/impl.go`                                   | Implementation of the `/rpc/auditlogs.*` Goa service (reads). Lives in its own package to keep the `audit` writer surface free of `auth/sessions` and `mv` so any service can call `audit.Log*` without import cycles. |
| `server/internal/audit/queries.sql`                                  | SQLc queries for the audit log table. Regenerates `server/internal/audit/repo/` via `mise run gen:sqlc-server`.                                                                                                        |
| `server/internal/auditapi/{setup_test,list_test,listfacets_test}.go` | Tests for the `auditlogs` management API.                                                                                                                                                                              |

### Generated files

Files under `server/gen/**` and any `repo/` subdirectory carry a `DO NOT EDIT` header.

| Path                                                  | Generator                                                                                                                       |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `server/gen/auditlogs/`, `server/gen/http/auditlogs/` | `mise run gen:goa-server` from `server/design/auditlogs/design.go`.                                                             |
| `server/internal/audit/audittest/repo/`               | `mise run gen:sqlc-server` from `server/internal/audit/audittest/queries.sql` (separate stanza in `server/database/sqlc.yaml`). |
| `server/internal/audit/repo/`                         | `mise run gen:sqlc-server` from `server/internal/audit/queries.sql` (via the `audit` stanza in `server/database/sqlc.yaml`).    |

## Server-client contract

Audit entries are surfaced to Gram users through a small, fixed set of endpoints. New actions and subject types appear automatically — facets are computed from the rows that exist, so there is no registration step outside the Go code.

**HTTP routes** (design: `server/design/auditlogs/design.go`):

- `GET /rpc/auditlogs.list` — paginated list; supports cursor, `project_slug`, `actor_id`, and `action` filters.
- `GET /rpc/auditlogs.listFacets` — returns the set of actors and actions that actually appear, for UI facet pickers.

**Generated client surfaces** — regenerated by `mise run gen:goa-server` then `mise run gen:sdk`:

- TypeScript SDK: `client/sdk/src/funcs/auditlogs*.ts`, `client/sdk/src/react-query/auditlogs*.ts`, plus models under `client/sdk/src/models/`.
- CLI bindings: `server/gen/http/cli/gram/cli.go`.

## Jobs to be done

### How to audit a handler in a service

When a handler mutates a resource whose subject already has `Action*`/`Log*` definitions, the handler opts in by calling the existing `Log*` function. The call has to happen inside the same `dbtx` as the mutation so the audit row and the state it describes commit together.

1. Open the service's `impl.go` and locate the handler. Confirm the handler already uses a transaction; if not, wrap the repo calls in `s.db.Begin(ctx)` → `defer o11y.NoLogDefer(rollback)` → `dbtx.Commit(ctx)`.
2. After the primary repo writes succeed and before `Commit`, call the subject's `audit.Log<Verb>` with a populated `Log<Verb>Event`. Pass the same `dbtx` the repo writes used so the audit row commits atomically with them.
3. Build the actor from `contextvalues.GetAuthContext(ctx)` — typically `urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)`, plus `authCtx.Email` for `ActorDisplayName`. Principal types live in `server/internal/urn`. Fill in the subject-specific identifier fields (subject id, display name, slug) from the repo row you just wrote.
4. For updates, populate the typed snapshot fields (`<Subject>SnapshotBefore` / `<Subject>SnapshotAfter`) with the pre- and post-mutation state. For creates and deletes, leave them nil unless the snapshot is independently useful.
5. Treat audit-log failures as `oops.CodeUnexpected`. Audit logging is not optional — if it fails, fail the request.
6. Add a test that asserts the event was recorded (see "How to assert audit events in tests" below).

### How to audit a cascading delete of child resources

Use this when deleting a parent resource also soft-deletes child rows of an independently audited subject (e.g. deleting an `mcp_server` cascades to its `mcp_endpoints`). The parent's `Log<Verb>` is not enough — every affected child row must produce its own audit entry under the child subject's action.

1. Make the cascade query return the affected rows. SQLc queries scoped by parent id should be `:many` with `RETURNING *` so the caller can iterate the deleted children. If the existing query is `:exec`, change it and regenerate (`mise run gen:sqlc-server`).
2. In the parent handler, after the cascade query succeeds and inside the same `dbtx`, loop over the returned rows and call the child subject's `audit.Log<Verb>` once per row. Populate the child's URN, display name, and slug from the returned row — not from the parent.
3. Emit the parent's `audit.Log<Verb>` after the per-child loop so cause precedes effect in the timeline. Both still commit atomically with the cascade.
4. In tests, capture baseline counts for both the parent and the child action, exercise the handler, and assert the child count grew by exactly the number of cascaded rows. A single +1 assertion on the parent action will not catch a regression where the per-child events stop being emitted.

### How to add a new action to an existing subject

Use this when the subject already has a file but you're introducing a new verb.

1. In the subject's file, add an `Action<Subject><Verb>` constant alongside the existing ones.
2. Add a `Log<Subject><Verb>Event` struct with the fields the caller needs to supply. At minimum: `OrganizationID`, `ProjectID` (zero value for org-scoped subjects), `Actor`, `ActorDisplayName`, `ActorSlug`, plus the subject URN (`<Subject>URN urn.<Subject>`) and any additional display name / slug fields the subject needs. Updates additionally carry typed snapshot fields (`<Subject>SnapshotBefore` / `<Subject>SnapshotAfter` with concrete pointer types — e.g. `*types.<Subject>`).
3. Add a `Log<Subject><Verb>` function that translates the event into `repo.InsertAuditLogParams`, passes any snapshots through `marshalAuditPayload` (which handles nil internally), calls `repo.New(dbtx).InsertAuditLog`, and wraps errors with `fmt.Errorf("log %s: %w", action, err)`.
4. Call the new function from the handler as described under "How to audit a handler in a service".

No schema change, no codegen step — facet queries pick up the new action automatically.

### How to add a new audited subject

Use this when introducing an entirely new kind of resource that doesn't map onto any existing subject file.

1. Add a `subjectType<Name>` constant to `events.go`.
2. Create `server/internal/audit/<subject>.go` following the subject-file convention, and populate it with the `Action*` constants, `Log*Event` structs, and `Log*` functions.
3. Call the new `Log*` functions from the owning service's handlers.

### How to update an existing action or subject

1. **Renaming an `Action` value is a breaking change** for consumers of `auditlogs.list` that filter on action strings. Avoid it; add a new action and dual-write if a behaviour rename is needed.
2. Adding a new field to a `Log*Event` struct is safe — update every call site (`exhaustruct` will flag missed ones).
3. Changing the shape of the snapshot payload (the concrete type referenced by `<Subject>SnapshotBefore` / `<Subject>SnapshotAfter`) is safe for new rows only; old rows retain the old shape. If consumers parse snapshots, version the payload inside the JSON.
4. Do not edit the string values of `subjectType*` constants; the same breakage argument as action renames applies.

### How to assert audit events in tests

Use `audittest` helpers in the service's test package — do not query the audit tables directly.

1. Capture a baseline count with `audittest.AuditLogCountByAction(ctx, conn, audit.Action<Foo>)`.
2. Exercise the handler.
3. Assert `after == before + 1` (or the expected delta).
4. For snapshot correctness, fetch the row with `audittest.LatestAuditLogByAction` and decode `Metadata`, `BeforeSnapshot`, or `AfterSnapshot` with `audittest.DecodeAuditData`.

## Relevant mise tasks

| Task                       | Purpose                                                                                                                                                                                                             |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `mise run gen:goa-server`  | Regenerate `server/gen/auditlogs/**` whenever you edit `server/design/auditlogs/design.go`.                                                                                                                         |
| `mise run gen:sdk`         | Regenerate the TypeScript SDK and CLI bindings after a Goa design change.                                                                                                                                           |
| `mise run gen:sqlc-server` | Regenerate `server/internal/audit/repo/` and `audittest/repo/`. Run whenever you change `queries.sql` in either place. Requires `mise run infra:start` (sqlc connects to the local Postgres to type-check queries). |
| `mise run lint:server`     | `golangci-lint` including `exhaustruct` — keep struct literals complete when adding fields to `Log*Event`.                                                                                                          |
| `mise run test:server`     | Runs the full server test suite. Takes the same arguments as `go test` (e.g. `./internal/audit/... ./internal/remotemcp/...`).                                                                                      |

## Maintaining this skill

This file documents conventions that evolve over time. Adding a new action, subject, or filter is already covered by "Jobs to be done" — those don't require skill edits. Structural changes do. Update this skill in the same commit when you make any of the following kinds of changes:

- Reorganising per-subject files (moving away from one-file-per-subject, merging subjects, renaming the plural convention).
- Renaming or reshaping `Log*Event` fields, or changing the `Log*` function signature.
- Replacing `marshalAuditPayload` or changing how snapshots are encoded.
- Moving audit code out of `server/internal/audit/`.
- Changing how facets are computed — today they derive from row data with no registration; if that changes, the "no registration step" claim stops being true.
- Adding a new audit-relevant mise task that belongs on the cheat sheet.
- Introducing a new top-level concept (a new principal type for actors, a new kind of subject scoping beyond org/project, etc.).

## Cross-references

- `gram-management-api` — the `auditlogs` service is itself a management API; adding a new endpoint or filter follows that skill's flow.
- `gram-rbac` — `access_role`, `access_member`, and other RBAC mutations are audited via `server/internal/audit/access.go`.
- `golang` — `oops` error wrapping, `slog` logging, transaction patterns, and the black-box `setup_test.go` convention used by audit tests and service tests.
- `postgresql` — when adding or changing SQLc queries in `audit/queries.sql` or `audittest/queries.sql`.
- `mise-tasks` — when modifying the generator scripts under `.mise-tasks/gen/`.
