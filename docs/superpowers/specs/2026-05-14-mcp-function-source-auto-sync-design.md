# MCP Function-Source Auto-Sync

Linear: [AGE-1377 — feat: Add flag to functions uploads that allows updating a MCP server directly](https://linear.app/speakeasy/issue/AGE-1377)

Status: design — pending implementation plan.

## Problem

When a customer pushes a new function deployment, **updates to existing tools** flow into their MCP servers automatically — tool URNs are deployment-agnostic (`tools:function:<source>:<name>`) and the MCP resolves the latest deployment's row for any URN already in the toolset. **New tools do not.** They land in `function_tool_definitions` but never reach an MCP until a human opens the dashboard and adds them to the relevant toolset.

Customers experience this as a "stage area" they have to clear after every push. The actual implementation has no staging status — the gap is the toolset's frozen `tool_urns` array.

## Goal

Allow a toolset to declare that it follows one or more function sources. Once declared, every subsequent deployment that contains new tool URNs for those sources extends the toolset automatically, producing a new `toolset_version` row. The customer never opens the dashboard for the routine case.

## Non-goals

- Auto-sync of OpenAPI sources. The `kind:source` encoding reserves space for it, but lifecycle semantics (spec revisions, operationId renames, larger blast radius on public MCPs) need their own safety policy. Tracked as a follow-on ticket.
- Auto-removal when a deployment drops a tool. Removals are higher-risk and have no clean revert — they continue to flow through the existing orphan-URN UI.
- A project-wide "auto-sync everything" toggle. Scoping must be explicit per source.

## Design overview

A new column on `toolsets` names the function sources the toolset follows. A new step in the deployment-completed workflow diffs the deployment's new tool URNs against each subscribing toolset and appends additions as a new `toolset_version`. A CLI flag and a `gram.deploy.json` field set the subscription as a side-effect of `gram push`, matching the ticket's literal ask. A dashboard card lets users manage the subscription directly.

```
gram push --auto-attach my-mcp
        │
        ▼
deployment processing (Temporal)
        │
        ├── function_tool_definitions rows written
        │
        ▼
deployment-completed hook
        │
        ├── group new URNs by urn.Source
        │
        ▼
for each toolset with source ∈ auto_sync_sources:
        ├── diff URNs vs latest toolset_version.tool_urns
        ├── if additions: write new toolset_version (additions only)
        └── emit audit-log entry
```

## Data model

```sql
ALTER TABLE toolsets
  ADD COLUMN auto_sync_sources TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];
```

- Empty array = current manual behavior. Backwards compatible.
- Each entry is a kind-prefixed source identifier: `"<kind>:<source>"`, where `<kind>` matches `urn.ToolKind` (`function`, `http`, etc.) and `<source>` matches the `Source` segment of `tools:<kind>:<source>:<name>`. For functions, `<source>` is the value from `manifest.ManifestV0`'s top-level source identifier.
- **Only `function:` entries are accepted today.** The server-side validator (PR 2) rejects any other prefix with a stable 400 error. The kind discriminator is reserved in the encoding so OpenAPI auto-sync can land as a purely additive PR later — no column rename, no expand-contract migration on a contract table.
- Lives on `toolsets`, not `toolset_versions`, because it is _intent_, not a snapshot. Each auto-extend still produces a new immutable `toolset_version`, preserving the existing pattern (mirrors `default_environment_slug`).

A subsequent migration (separate PR, per the CLAUDE.md migration rules) adds the column. No backfill needed — the default handles every existing row.

## Server-side hook

Lives in the deployment-completed pathway in `server/internal/deployments/impl.go`, alongside the existing trigger for `FunctionsReaperWorkflow`. Implemented as a new Temporal activity (so a slow toolset write doesn't block deployment completion and so retries are bounded) or, if the work is reliably fast, an inline DB transaction at completion. Decision deferred to the implementation plan; prefer the activity if the toolset count per project is unbounded.

Steps:

1. Load all rows in `function_tool_definitions` for `deployment_id = D`. Build `addedBySource map[string][]urn.Tool` keyed by `urn.Source`.
2. Load every toolset in the project where `auto_sync_sources && ARRAY[<"function:" || source for each source in addedBySource>]` (Postgres array-overlap operator on the prefixed entries).
3. For each candidate toolset:
   - Read the latest `toolset_version.tool_urns`.
   - Compute additions: URNs from `addedBySource[source]` for each subscribed source, minus URNs already present. **Never compute deletions.**
   - If additions is non-empty, insert a new `toolset_version` row with `tool_urns = existing ∪ additions`, `predecessor_id = latest.id`, and `version = latest.version + 1`.
   - Emit one audit-log event: `action = "toolset.tools_auto_added"`, `subject = <toolset URN>`, `actor = system`, payload `{deployment_id, added_urns, sources}`.

Idempotency: a replay of the activity for the same `deployment_id` is a no-op because the second pass finds no new URNs to add. This matters because Temporal activities can re-execute.

Failure semantics: a failure to extend one toolset must not abort extension of others. Each toolset write is its own transaction.

## CLI affordance

### Flag

```
gram push --auto-attach <toolset-slug>[,<slug>...]
```

Effect: the push payload includes the slugs. On the server, the column update happens **inside the same transaction that records the deployment**, so the first push with `--auto-attach` already triggers the deployment-completed hook against the updated subscription. The server resolves each slug and **adds** every function source named in this deployment's `gram.deploy.json` to that toolset's `auto_sync_sources`, encoded as `"function:<source>"` entries (set union — additive, never removes).

The CLI flag is coarse-grained on purpose: it subscribes the toolset to _every_ function source in the push. Fine-grained subscription (per-source toggles) is the dashboard's job; this keeps the CLI ergonomic for the common "I just want this MCP to track everything I'm pushing" workflow without overloading flag syntax.

Idempotent: re-running with the same flag is a no-op on the column.

### Persistent config

`gram.deploy.json` gains an `auto_attach` field at the deployment level:

```json
{
  "type": "deployment",
  "auto_attach": ["my-mcp", "internal-tools"],
  "sources": [ ... ]
}
```

CLI behavior: if both the flag and the file specify `auto_attach`, the **union** is sent. This keeps CI workflows that already write the config working, and lets local debug pushes layer additional slugs without editing the file.

## Dashboard affordance

On the MCP detail page (`MCPDetails.tsx`):

- New card **"Auto-sync sources"** between the existing OAuth and Tools cards. Lists every function source present in the project. Each row is a toggle bound to the `auto_sync_sources` array on the toolset.
- Header badge: when the array is non-empty, show a pill "Auto-syncing N sources" so consequences are visible at a glance.
- Tools list: tools added by auto-sync show an "auto-added <date>" badge. Reuses the same row-affordance pattern as the orphan-URN treatment already in `MCPDetails.tsx`.

The toggle calls the existing `updateToolset` RPC with a new optional field. No new endpoint.

## RBAC and audit

- Setting `auto_sync_sources` = `toolset:write`. Same scope as any other toolset edit. No new scope needed.
- Each auto-extend emits an audit-log event via the existing internal audit-logging API (`gram-audit-logging` skill). Actor is the system principal; the deployment ID is in the payload so users can trace why a URN appeared. The standard `/rpc/auditlogs.*` surface exposes these without any extra work.

## Conflict and edge cases

| Case                                                                                | Behavior                                                                                                                                                                                                                                                                                                                          |
| ----------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Deployment adds a function tool whose URN already exists as `http` or `externalmcp` | Auto-sync only operates on URNs whose `Kind = function`. Other kinds are untouched, so no collision risk.                                                                                                                                                                                                                         |
| Toolset's `auto_sync_sources` references a source the project no longer has         | Silent no-op for that entry. Column value preserved so a re-upload of the source picks up automatically.                                                                                                                                                                                                                          |
| Toolset has `mcp_is_public = TRUE`                                                  | No additional guard. Audit log + dashboard badge are the safety net. We can layer a per-toolset confirmation later if customer feedback demands it.                                                                                                                                                                               |
| Toolset uses `tool_selection_mode = 'dynamic'`                                      | No interaction. Dynamic mode reads from the same `tool_urns` set, so it benefits transparently.                                                                                                                                                                                                                                   |
| A future deployment removes a tool that was auto-added                              | URN remains in the toolset. Surfaces via the existing orphan-URN UI, where the user prunes deliberately.                                                                                                                                                                                                                          |
| Two deployments complete concurrently                                               | Per-toolset writes serialize via `SELECT … FOR UPDATE` on the toolset row before reading the latest `toolset_version`. The unique constraint on `(version, toolset_id)` is the second line of defense — on the unlikely race past the row lock, the losing INSERT fails and the activity retries, finding its URNs already added. |
| User pushes with `--auto-attach my-mcp` but `my-mcp` doesn't exist                  | Push fails before deployment write. Standard 404 surfaced through the CLI.                                                                                                                                                                                                                                                        |

## Audit and rollback story

- Every auto-extend is its own audit event linked to a deployment. The dashboard's audit-log view (per `gram-audit-logging`) lets users walk back through every system-driven toolset change.
- Rollback path: toggling a source off in the dashboard or removing it via `gram.deploy.json` immediately stops future auto-adds. Previously-added URNs stay; the user can remove them through the normal toolset-edit flow if desired. This matches the spirit of the existing immutable `toolset_versions` history — auto-sync extends history, it never rewrites it.

## Out of scope (revisit later)

- OpenAPI auto-sync. The data model is already polymorphic via the `kind:source` encoding; what's deferred is the UI surface (operation-group pickers, breaking-change visualization) and the safety policy (per-tag allow-lists, operationId-rename detection). A future PR adds `http:` validator support and the UI; no schema work required.
- Wildcard "follow all sources" subscription. Forcing explicit scoping is the right default for now.
- Auto-removal. Tracked in the design above as deliberately deferred.

## Validation plan

- Unit tests on the diff-and-extend logic: empty subscription, single source, multi-source, all-already-present, partial overlap.
- Integration test: stage two functions, push with `--auto-attach`, assert toolset version increments and new URNs land. Push again with a new tool added to the same source, assert the second auto-extend.
- Integration test: subscribe two toolsets to the same source, push, assert both extend independently and emit independent audit events.
- Integration test: subscribe a toolset to source A; push a deployment that only adds tools to source B; assert no toolset extension occurred.
- Frontend: snapshot test of the new MCP detail card; existing `MCPDetails.tsx` orphan-URN tests untouched.

## Open implementation questions for the plan stage

1. Activity vs inline transaction in the deployment-completed pathway. Decision: pick activity if toolset count per project can grow beyond a small constant.
2. Where the `kind:` prefix gets attached. Two options: (a) CLI sends bare slugs and the server prefixes with `function:` during the column write, or (b) CLI sends pre-prefixed entries. Lean toward (a) — keeps the CLI surface minimal and concentrates the prefix policy on the server, which is also where the validator that rejects unknown kinds lives.
3. Whether to expose `auto_sync_sources` on the `Toolset` read view returned by `gettoolset.go`. Probable yes — needed by the dashboard.
