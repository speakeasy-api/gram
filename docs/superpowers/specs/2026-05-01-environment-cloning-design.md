# Environment cloning — design

**Date:** 2026-05-01
**Status:** Approved
**Author:** Sagar (with Claude)

## Goal

Let users clone an existing environment from the dashboard's Environments page. When cloning, they can choose to copy only the variable names or also the encrypted secret values. The feature must not expose masked secrets, must not modify the source environment, and must not break the existing encryption boundary.

## Security envelope (non-negotiable)

1. **Plaintext never leaves the database.** The "copy values" path uses a single SQL `INSERT INTO environment_entries (...) SELECT ... FROM environment_entries WHERE environment_id = $src` — ciphertext bytes flow row-to-row server-side and are never decrypted in Go. The masked `"*"` shown in the UI is irrelevant to the clone path.
2. **Source is read-only.** Clone issues exactly one `SELECT` against the source env and `INSERT`s into a brand-new `environment_id`. No `UPDATE`/`DELETE` against the source.
3. **Project boundary at SQL.** Source fetch uses `WHERE project_id = $caller_project_id AND slug = $src_slug AND deleted IS FALSE` — same pattern as existing `GetEnvironmentBySlug`. Cross-project clone is impossible even with a guessed slug.
4. **Authz gates (two layers, updated 2026-05-03 in response to PR #2561 review).**
   - Project-level: `authz.ScopeEnvironmentWrite` at the project resource — authority to add a new environment to this project. Backward-compatible with existing `project:write` grants via `scopeExpansions[ScopeEnvironmentWrite] = {ScopeProjectWrite}`.
   - Source-level: `authz.RequireAny` of `{ScopeEnvironmentRead at sourceEnv.ID}` OR `{ScopeProjectRead at projectID}` — authority to read this specific environment. `RequireAny` is used (rather than expansion-based satisfaction) because `Check.expand()` preserves the `ResourceID` across scope variants, so an `env:read` check at an environment's UUID cannot expand into a `project:read` variant that would match a project-pinned grant — the IDs are for different resource types and would never align.
5. **No client-supplied ciphertext.** Clone API accepts `{ slug, new_name, copy_values }` only. Clients cannot smuggle attacker-controlled ciphertext into the encrypted column.
6. **Audit log.** `audit.LogEnvironmentCreate` is emitted on the new env (same as a normal create).

## Backend

### Goa method

`cloneEnvironment` — POST `/rpc/environments.clone?slug=<src>`

Payload:

```
slug:        string  (query, source env slug)
new_name:    string  (body, name for the clone)
copy_values: bool    (body, optional, default false)
```

Returns `shared.Environment`.

### Service flow (single transaction)

1. `authz.Require(ScopeEnvironmentWrite at projectID)` — authority to add envs to this project.
2. `GetEnvironmentBySlug` with caller's `project_id`. 404 on miss.
3. `authz.RequireAny({env:read at sourceEnv.ID}, {project:read at projectID})` — authority to read the source.
4. Generate slug from `new_name` via `conv.ToSlug`.
5. `CreateEnvironment` with `(name=new_name, slug=new_slug, description=source.description)`. On unique-violation → `CodeConflict` with a clean message; the user picks a different name.
6. If `copy_values = true`: `CloneEnvironmentEntriesWithValues(new_id, source_id)` — pure SQL `INSERT … SELECT`, no decrypt path.
7. If `copy_values = false`: encrypt `""` once in Go, then `CloneEnvironmentEntryNames(new_id, source_id, placeholder)` — `INSERT … SELECT name, $placeholder` so names are preserved with empty placeholder values.
8. `ListEnvironmentEntries(redacted=true)` to build the response view.
9. `audit.LogEnvironmentCreate(newEnv)`.
10. Commit.

We **do not** copy `source_environments` or `toolset_environments` links. The clone is a fresh, unattached environment.

### New SQLc queries (server/internal/environments/queries.sql)

```sql
-- name: CloneEnvironmentEntriesWithValues :exec
INSERT INTO environment_entries (environment_id, name, value)
SELECT @new_environment_id::uuid, name, value
FROM environment_entries
WHERE environment_id = @source_environment_id::uuid;

-- name: CloneEnvironmentEntryNames :exec
INSERT INTO environment_entries (environment_id, name, value)
SELECT @new_environment_id::uuid, name, @placeholder_value::text
FROM environment_entries
WHERE environment_id = @source_environment_id::uuid;
```

## Frontend

### EnvironmentCard kebab

Wrap the card with the existing `MoreActions` dropdown (matches `ToolsetCard`). One action: `Clone`. Delete stays on the detail page for now.

### CloneEnvironmentDialog

New component using the existing Radix `Dialog` primitive in `client/dashboard/src/components/ui/dialog.tsx`.

Form fields (plain `useState`, matches existing patterns):

- **Name** — `Input`, default `${source.name} (copy)`. Submit-disabled if empty.
- **Copy stored values** — `Switch`, default OFF. Helper text: _"Off: copies only variable names. On: duplicates the encrypted secret values."_

Buttons: Cancel · Clone.

### Mutation hook

`useCloneEnvironmentMutation` (auto-generated by `mise gen:sdk`). Wrap in `useCloneEnvironment` helper that:

- captures telemetry (`environment_event` / `environment_cloned`)
- toasts success
- refetches the environments list
- navigates to the new env detail page

## Out of scope (YAGNI)

- Deep clone of source/toolset environment links.
- Bulk multi-clone.
- Per-entry selection in the modal — toggle-all is enough for v1.
- New RBAC scope.

## Open question — resolved

When `copy_values = false`, new entry rows are inserted with **empty encrypted string `""`**. Existing UI's empty-value affordances apply.

## Files touched

| Path                                                                 | Change                                                                 |
| -------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `server/design/environments/design.go`                               | Add `cloneEnvironment` method + `CloneEnvironmentForm`                 |
| `server/internal/environments/queries.sql`                           | Add `CloneEnvironmentEntriesWithValues` + `CloneEnvironmentEntryNames` |
| `server/internal/environments/impl.go`                               | Add `CloneEnvironment` service method                                  |
| `server/gen/...`                                                     | Regenerated by `mise gen:goa-server`                                   |
| `server/internal/environments/repo/...`                              | Regenerated by `mise gen:sqlc-server`                                  |
| `client/sdk/...`                                                     | Regenerated by `mise gen:sdk`                                          |
| `client/dashboard/src/pages/environments/Environments.tsx`           | Add MoreActions kebab to `EnvironmentCard`                             |
| `client/dashboard/src/pages/environments/CloneEnvironmentDialog.tsx` | New dialog component                                                   |
| `client/dashboard/src/pages/environments/useEnvironmentActions.ts`   | New `useCloneEnvironment` hook                                         |
