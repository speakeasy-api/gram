---
name: gram-rbac
description: Concepts, external interfaces, and conventions for Gram's role-based access control (RBAC) subsystem — scopes, grants, principals, system roles, and the `authz.Engine.Require` enforcement path used inside handlers. Activate whenever the task involves authorization (adding or modifying a scope or resource type, declaring a new role or grant, gating a handler, changing scope inheritance, exposing RBAC state through the dashboard).
metadata:
  relevant_files:
    - "server/internal/authz/**/*.go"
    - "server/internal/authztest/**/*.go"
    - "server/internal/access/**/*.go"
    - "server/internal/access/**/*.sql"
    - "server/design/access/**"
    - "client/dashboard/src/pages/access/**"
---

Gram's RBAC is a scope-and-selector model. The server ships with a fixed set of **scopes** grouped into **system roles** (admin, member). A **grant** binds a scope to a **selector** (a Kubernetes-style `map[string]string` of `resource_kind`, `resource_id`, plus optional narrowing dimensions like `tool` or `disposition`) for a given **principal** (user or custom role). Handlers enforce scopes by calling `authz.Engine.Require(ctx, authz.Check{...})`; the dashboard renders the same scope vocabulary through a matching TypeScript union that is hand-maintained in lockstep with the server.

## Concepts and terminology

**Scope.** A named permission that authorizes an operation on a particular kind of resource.

**Resource type.** The kind of resource a scope protects — currently `org`, `project`, or `mcp`. Every scope has exactly one resource type.

**Scope expansion.** Higher-privilege scopes satisfy lower-privilege ones. In the read/write/connect family the privilege order is `write > read > connect`: `mcp:write` satisfies a `mcp:read` check, and either `mcp:read` or `mcp:write` satisfies a `mcp:connect` check (`connect` is the broadest, easiest-to-satisfy gate). The mapping lives in `scopeExpansions` in `authz/scopes.go` — key = required scope, value = higher-privilege scopes that also satisfy it.

**Selector.** A `map[string]string` of constraints attached to a grant or check. Always carries `resource_kind` and `resource_id` (both required); MCP scopes additionally allow `tool` and `disposition`. Wildcards are explicit values — `{"resource_kind":"*","resource_id":"*"}`, never empty `{}`. Defined in [server/internal/authz/selector.go](server/internal/authz/selector.go).

**Selector matching.** A grant selector satisfies a check selector when, for every key the grant constrains, either the values are equal or the grant value is `"*"`. Keys present on the grant but absent from the check are skipped — this is what lets a disposition-scoped grant (`{"disposition":"read_only"}`) still satisfy a connection-level check that doesn't constrain disposition.

**Grant.** A tuple of `{Scope, Selector}` held by a principal. The API-visible forms are `RoleGrant` (carrying `Selectors []Selector`) and `ListRoleGrant` (which also carries the transitively-implied `sub_scopes`). Use `authz.NewGrant(scope, resourceID)` to construct one — it derives the selector's `resource_kind` from the scope family.

**Principal.** Who holds a grant — a `urn.Principal` with a type (user, role, service account) and an id.

**Dimensions.** Optional narrowing keys on a `Check` beyond `resource_id`. Today: `tool` and `disposition` for MCP scopes (see [server/internal/authz/checks.go](server/internal/authz/checks.go) and `MCPToolCallCheck`). Allowed keys per scope family are enforced by `ValidateSelector`; new dimensions must be added to `allowedSelectorKeys` in `selector.go`.

**Disposition.** A snake_case bucket derived from MCP tool annotation hints — `read_only`, `destructive`, `idempotent`, `open_world`. Constants live in `authz/selector.go`; `conv.DispositionFromAnnotations(annotations)` is the canonical conversion from `*types.ToolAnnotations`.

**System role.** A built-in role shipped with the server. Gram defines two: **admin** (every scope) and **member** (the read-and-connect subset). Constants `authz.SystemRoleAdmin` and `authz.SystemRoleMember`.

**Enforcement.** Inside a handler, authorization is an explicit one-line check: the handler names the scope (and resource, if project-scoped) it needs, and the RBAC engine either allows the call or returns a forbidden error.

**Auth context invariant.** By the time a handler runs, the auth context's `ActiveOrganizationID` is always populated — RBAC does not defensively check for an empty org id.

## Server

RBAC is split across two Go packages: `server/internal/authz/` holds the enforcement primitives, and `server/internal/access/` implements the Goa management service that exposes them over HTTP. When adding new authorization primitives (scopes, checks, enforcement logic) edit `authz`; when adding or changing the management API (role/member endpoints) edit `access`.

### `authz` package — the enforcer

Scope vocabulary, grant types, and enforcement logic are defined here. `authz`'s imports are deliberately minimal (DB, logger, cache, WorkOS, urn) so any package that gates on RBAC can depend on it without import cycles — never add an import to `authz` from a package that transitively depends on `authz`, since the split exists specifically to prevent the cycles that motivated it.

**Scope declarations.** `Scope` type and every `Scope*` constant live in `server/internal/authz/scopes.go`. Constants follow the `Scope<Name>` pattern; string values follow `<resource>:<verb>` (e.g. `mcp:read`, `org:admin`). `ScopeRoot` is reserved for service-internal superadmin overrides. The file also holds the `scopeExpansions` map and computes the inverse `scopeSubScopes` in `init()`. `CalculateSubScopes(scope)` exposes the inverse to callers.

**System role grants.** `SystemRoleGrants` in `server/internal/authz/grants.go` — admin and member defaults. Adding a scope usually means adding it to admin, and optionally to member if end users should get it by default. `SeedSystemRoleGrants` upserts the full set; `SyncGrants` upserts grants for a single role slug.

**`authz.Engine`.** The central enforcer. Methods: `PrepareContext`, `Require(ctx, checks...)`, `RequireAny(ctx, checks...)`, `Filter(ctx, scope, ids)`, `ShouldEnforce`, `InvalidateRoleCache`, `InvalidateAllRoleCaches`, `GetScopeOverrides`. Constructed in `server/cmd/gram/start.go` via `authz.NewEngine(logger, db, isEnabled, membership, roleCache, opts...)` and injected into every service that gates on RBAC. The `IsRBACEnabled` callback lets the engine short-circuit when the product feature flag is off for the org; the `MembershipFetcher` is the WorkOS client used for role-slug lookups.

**`authz.Check`.** `{Scope, ResourceKind, ResourceID, Dimensions}` — the thing a handler asks `Require` to enforce. For the common single-resource case, leave `ResourceKind: ""` (auto-derived from the scope family) and `Dimensions: nil`; exhaustruct requires every field at every call site. `ResourceID` is typically `authCtx.ProjectID.String()` for project-scoped scopes. Defined in [server/internal/authz/access.go](server/internal/authz/access.go).

**`authz.Filter` for list endpoints.** When a handler lists resources the caller might only partially own, `s.authz.Filter(ctx, scope, candidateIDs) ([]string, error)` returns the subset of IDs the caller holds the scope for. The standard pattern is: gather candidate IDs from the repo, call `Filter`, then rebuild the response from the allowed IDs. Prefer this over a post-hoc per-item `Require` loop. Canonical call sites: `server/internal/projects/impl.go` (projects list) and `server/internal/toolsets/impl.go` (toolsets list).

**Auth context accessor.** `contextvalues.GetAuthContext(ctx)` returns the current `*AuthContext`. RBAC-relevant fields: `ActiveOrganizationID`, `ProjectID`, `UserID`, `Email`, `AccountType`, `IsAdmin`, `APIKeyID`, `SessionID`.

**Scope overrides.** A local-dev/superadmin header can inject a restricted grant set for the request, parsed in `override.go` and surfaced via `Engine.GetScopeOverrides`. `access.ListGrants` returns the override set verbatim when active so the dashboard reflects what the engine will enforce.

**Error model.** `errors.go` defines sentinel errors (`ErrDenied`, `ErrMissingGrants`, `ErrNoChecks`, `ErrInvalidCheck`) and typed errors (`DeniedError`, `InvalidCheckError`). The engine maps these to `oops` codes — `ErrDenied` → `oops.CodeForbidden`, everything else → `oops.CodeUnexpected` with a logged message.

**Grant loading.** `LoadGrants(ctx, db, orgID, principals)` reads the principal URN set and returns the flattened `[]Grant`. Called by both `Engine.PrepareContext` (middleware path) and `access.ListGrants` (user-facing). Each row's `selectors` JSONB is parsed via `SelectorFromRow`.

**Sync semantics.** `SyncGrants` distinguishes nil from empty: `RoleGrant{Selectors: nil}` writes a single wildcard row; `RoleGrant{Selectors: []Selector{}}` writes nothing (no access). Each non-nil selector is validated by `ValidateSelector` before insert.

### `access` package — the management API

`server/internal/access/` implements the Goa `access` service on top of `authz`. Every handler calls `s.authz.Require(...)` with the appropriate scope before doing work. The package also owns `queries.sql` and the generated `server/internal/access/repo/` SQLc package that both `access` and `authz` use to read and write grant rows.

**Scope metadata.** `ListScopes` in `server/internal/access/impl.go` returns one `{Slug, Description, ResourceType}` entry per scope; this is what the dashboard consumes to render the scope picker.

**Full-access grant list.** `ListGrants` returns a hard-coded full-access scope list when RBAC is disabled or the caller has no grants loaded. That inline list in `server/internal/access/impl.go` must grow whenever a new scope is added. The parallel test expectation is `expectedFullAccessScopes` in `server/internal/access/listusergrants_test.go`.

**System role gating.** `isSystemRole(slug)` in `impl.go` checks against `authz.SystemRoleAdmin` and `authz.SystemRoleMember`. System roles cannot be renamed, deleted, or have their grant set edited; only member assignment is allowed.

**Feature flag endpoints.** `enableRBAC` / `disableRBAC` / `getRBACStatus` are superadmin-only. Enabling seeds `SystemRoleGrants` into the org's `principal_grants` table.

### Non-generated files

| File                                   | Purpose                                                                                                                                                   |
| -------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `server/design/access/design.go`       | Goa design for the `access` service. Regenerates `server/gen/access/` and `server/gen/http/access/` via `mise run gen:goa-server`.                        |
| `server/internal/authz/access.go`      | The `Check` type and its expansion logic.                                                                                                                 |
| `server/internal/authz/checks.go`      | Pre-built `Check` builders for multi-dimensional checks (e.g. `MCPToolCallCheck`, `MCPToolCallDimensions`).                                               |
| `server/internal/authz/context.go`     | Request-context helpers for grants (`GrantsToContext`, `GrantsFromContext`).                                                                              |
| `server/internal/authz/engine.go`      | The `Engine` type — central RBAC enforcer, role-slug caching, and override resolution.                                                                    |
| `server/internal/authz/errors.go`      | Package sentinel errors and typed errors.                                                                                                                 |
| `server/internal/authz/grants.go`      | `Grant`/`RoleGrant`/`ScopedGrant` types, `SystemRoleGrants`, `SyncGrants`, `SeedSystemRoleGrants`, `GrantsForRole`, `GrantsToScopedGrants`.               |
| `server/internal/authz/load.go`        | Principal grant loading from the database.                                                                                                                |
| `server/internal/authz/override.go`    | Scope override plumbing (header parsing, override-to-grants conversion).                                                                                  |
| `server/internal/authz/scopes.go`      | Scope type, constants, and expansion rules.                                                                                                               |
| `server/internal/authz/selector.go`    | `Selector` type, matching rules, `NewSelector`/`NewGrant` helpers, `ValidateSelector`, `ResourceKindForScope`, disposition vocabulary, `SelectorFromRow`. |
| `server/internal/authztest/helpers.go` | Test helpers other packages reuse for RBAC setup (`WithExactGrants`, `RBACAlwaysEnabled`, `RBACAlwaysDisabled`).                                          |
| `server/internal/access/impl.go`       | Implementation of the `/rpc/access.*` Goa service.                                                                                                        |
| `server/internal/access/queries.sql`   | SQLc queries for principals, grants, roles, and members. Regenerates `server/internal/access/repo/` via `mise run gen:sqlc-server`.                       |

### Generated files

| Path                                            | Generator                                                                                                                      |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `server/gen/access/`, `server/gen/http/access/` | `mise run gen:goa-server` from `server/design/access/design.go`.                                                               |
| `server/internal/access/repo/`                  | `mise run gen:sqlc-server` from `server/internal/access/queries.sql` (via the `access` stanza in `server/database/sqlc.yaml`). |

## Server-client contract

Scope and resource-type changes on the server must be accompanied by a matching edit to a hand-maintained client file — adding a scope is not purely a server change.

**HTTP routes** (design: `server/design/access/design.go`):

- `/rpc/access.listScopes` — every scope the server knows about, with `resource_type` and description.
- `/rpc/access.listRoles`, `getRole`, `createRole`, `updateRole`, `deleteRole` — custom role CRUD.
- `/rpc/access.listMembers`, `updateMemberRole` — org membership and role assignment.
- `/rpc/access.listUserGrants` — the caller's effective grants.
- `/rpc/access.getRBACStatus`, `enableRBAC`, `disableRBAC` — feature-flag hooks (superadmin-only).

**Three-place enum lockstep.** `server/design/access/design.go` repeats the scope slug enum in three places — `RoleGrantModel.scope`, `ListRoleGrantModel.scope`, and its `sub_scopes` element — plus `ScopeModel.slug` for the listing endpoint. All three must stay synchronized with `authz/scopes.go`, and `ScopeModel.resource_type` must contain every resource type in use. Adding a new resource type also means adding it to `SelectorModel.resource_kind`'s enum (`project`, `mcp`, `org`, `*`) — the model that backs `RoleGrant.selectors` and `ListRoleGrant.selectors`.

**Generated SDK types.** `client/sdk/src/models/components/scopedefinition.ts`, `rolegrant.ts`, `listrolegrant.ts`, `selector.ts`, etc. Regenerated by `mise run gen:sdk` after every design change.

**Hand-maintained client mirror.** `client/dashboard/src/pages/access/types.ts` re-exports `Scope`, `Selector`, `Disposition`, `ResourceKind` (the latter three from the SDK), defines a `ResourceType` string-literal union, and the `RoleGrant` interface (`selectors: Selector[] | null`). It also owns `ANNOTATION_TO_DISPOSITION` / `DISPOSITION_TO_ANNOTATION` maps that mirror the disposition vocabulary in `authz/selector.go` — keep these in lockstep when adding or renaming dispositions.

## Client

The dashboard pages under `client/dashboard/src/pages/access/` render membership and role management on top of the generated SDK and the `listScopes` response. RBAC-aware UI across the rest of the dashboard gates itself through a shared hook and component.

### Conventions

**`useRBAC` hook.** `client/dashboard/src/hooks/useRBAC.ts` wraps the generated `useGrants` React Query hook and exposes `hasScope(scope, resourceId?)`, `hasAllScopes(scopes, resourceId?)`, `hasAnyScope(scopes, resourceId?)`, plus `isRbacEnabled`, `isLoading`, `grants`, and `error`. Returns `false` from the `has*` checks while loading and `true` when RBAC is disabled. The module also exports `selectorMatches(grant, check)` and `resourceKindForScope(scope)` — direct mirrors of the server-side helpers in `authz/selector.go` — for code that needs parity with backend matching outside the standard `hasScope` flow.

**`RequireScope` component.** `client/dashboard/src/components/require-scope.tsx` is the primary rendering gate. Props: `scope: Scope | Scope[]`, `all?: boolean` (AND vs OR when multiple scopes), `resourceId?: string`, `level: "page" | "section" | "component"`, `children`, and level-specific extras (`fallback` for page/section, `reason`/`className` for component).

- `level="page"` — renders a full Unauthorized fallback page when the scope is missing.
- `level="section"` — hides the children entirely.
- `level="component"` — renders disabled with a tooltip explaining why (good for buttons and inputs).

**Scope vocabulary import.** `useRBAC` and `RequireScope` both consume the `Scope` union from `client/dashboard/src/pages/access/types.ts` (covered under "Server-client contract"). Keeping that file in lockstep with the server is what makes the client gates work.

### Non-generated files

| File                                                                                                     | Purpose                                                                                         |
| -------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `client/dashboard/src/components/require-scope.tsx`                                                      | `RequireScope` gating component — page, section, and component-level rendering gates.           |
| `client/dashboard/src/hooks/useRBAC.ts`                                                                  | `useRBAC` hook — scope checks and raw grants for the dashboard.                                 |
| `client/dashboard/src/pages/access/Access.tsx`                                                           | Top-level access page shell.                                                                    |
| `client/dashboard/src/pages/access/ChangeRoleDialog.tsx`, `CreateRoleDialog.tsx`, `DeleteRoleDialog.tsx` | Role and member-role mutation dialogs.                                                          |
| `client/dashboard/src/pages/access/MembersTab.tsx`, `RolesTab.tsx`                                       | The two tabs of the access page.                                                                |
| `client/dashboard/src/pages/access/ScopePickerPopover.tsx`                                               | Scope selection UI.                                                                             |
| `client/dashboard/src/pages/access/types.ts`                                                             | Hand-maintained client mirror of the server's scope vocabulary. (See "Server-client contract".) |

## Jobs to be done

### How to gate a handler with an existing scope

1. Inject `*authz.Engine` into the service struct (if it isn't already) and keep it on `s.authz`.
2. At the top of the handler — before any database work — call `s.authz.Require(ctx, authz.Check{Scope: authz.Scope<Name>, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil})` and return the error as-is. The exhaustruct linter requires every `Check` field — leave `ResourceKind` empty to auto-derive from the scope family and `Dimensions` nil unless you're narrowing by tool/disposition.
3. Choose the narrowest scope for the operation: `*:read` for GET/list, `*:write` for mutations, `*:connect` for runtime usage. Scope expansions mean write callers are still permitted to read.
4. Use `RequireAny` instead of `Require` when a single handler legitimately satisfies multiple equivalent scopes.
5. In the handler's test, add one case that builds the context without the scope and asserts an `oops.CodeForbidden` response, and one case that builds the context with the scope via `authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.Scope<Name>, resourceID))`. Construct grants with `authz.NewGrant` (or `authz.NewGrantWithSelector` for non-trivial selectors) — never set `Grant.Selector` by hand.

### How to add a new scope to an existing resource type

Use this when the resource type is already represented (e.g. adding a new verb on `mcp`).

1. Add the `Scope<Name>` constant in `server/internal/authz/scopes.go`.
2. Add the new scope to `scopeExpansions` in the same file. Usually: the new scope is the upper or lower end of an existing read/write/connect triple.
3. Extend `SystemRoleGrants` in `server/internal/authz/grants.go`: admin always receives the new scope. Member receives it if and only if end users should have it by default (read and connect, yes; write, no).
4. Add a `{Slug, Description, ResourceType}` entry to `ListScopes` in `server/internal/access/impl.go`.
5. Extend the hard-coded full-access scope list in `ListGrants` (same `impl.go`) so callers without grants loaded still see the complete catalogue.
6. Update the three enums in `server/design/access/design.go` that have to stay in lockstep.
7. Add the new slug to the `Scope` union in `client/dashboard/src/pages/access/types.ts`.
8. Bump `expectedFullAccessScopes` in `server/internal/access/listusergrants_test.go` and the `require.Len(t, result.Scopes, N)` assertion in `server/internal/access/listscopes_test.go`.
9. Run `mise run gen:goa-server`, then `mise run gen:sdk`.
10. Run `mise run lint:server` and `mise run test:server`.

### How to add a new resource type

Use this when introducing a resource type that doesn't exist yet (e.g. the first `foo:*` scopes).

1. Follow every step under "How to add a new scope to an existing resource type" for each scope on the new type.
2. Additionally, add the new resource type string to `ScopeModel.resource_type` in `server/design/access/design.go`.
3. Additionally, add the new resource type to the `ResourceType` union in `client/dashboard/src/pages/access/types.ts`.

### How to change system role defaults

Use this when adjusting what `admin` or `member` gets out of the box. Prefer additive changes — removing a grant from a shipped role is an observable permissions change for existing users.

1. Edit `SystemRoleGrants` in `server/internal/authz/grants.go`.
2. Update `expectedFullAccessScopes` in `server/internal/access/listusergrants_test.go` if the admin set changed.
3. Consider whether existing orgs' grant tables need a migration to reflect the new defaults; new orgs pick up defaults automatically via `SeedSystemRoleGrants` when RBAC is enabled.
4. Run `mise run lint:server` and `mise run test:server`.

### How to narrow an MCP check by tool or disposition

Use this when a single handler should authorize per-tool — e.g. private MCP tool calls where a grant might allow only `read_only` tools. The canonical call site is [server/internal/mcp/rpc_tools_call.go](server/internal/mcp/rpc_tools_call.go).

1. Build dimensions with the typed struct in `authz/checks.go` rather than a raw map: `authz.MCPToolCallDimensions{Tool: params.Name, Disposition: disposition}`. Zero-value fields are dropped automatically.
2. For tool dispositions, derive the value from `*types.ToolAnnotations` via `conv.DispositionFromAnnotations(annotations)` — priority order is read_only > destructive > idempotent > open_world; missing or nil annotations yield an empty string (which gets dropped).
3. Build the check with the matching helper: `authz.MCPToolCallCheck(toolsetID, dims)`. For new dimension shapes, add a fresh helper to `authz/checks.go` rather than scattering raw `Check{Dimensions: …}` literals across services.
4. If you're introducing a brand-new dimension key, allowlist it in `allowedSelectorKeys` in `authz/selector.go`, otherwise `ValidateSelector` will reject any role grant that uses it. New disposition values must also be added to `validDispositions` and to the `disposition` enum on `SelectorModel` in `server/design/access/design.go`.
5. Selector-matching skips dimensions that the grant doesn't constrain — a grant of `mcp:connect` with no `tool` key still satisfies a check that names a specific tool. This is intentional; it lets less-narrow grants cover more checks.

### How to filter a list handler to the caller's accessible resources

Use this whenever a `list*` handler would otherwise return resources the caller has no grant for. `projects.List` and `toolsets.List` are the canonical examples.

1. Query the repo for the full candidate set the org/project contains.
2. Collect the candidate IDs into `[]string`.
3. Call `allowedIDs, err := s.authz.Filter(ctx, authz.Scope<Name>, candidateIDs)`. Return the error as-is.
4. Build a set from `allowedIDs` and rebuild the response by walking the original rows, keeping only the ones whose ID is in the set. Preserves repo ordering without a second query.
5. Do not fall back to a per-item `Require` loop — `Filter` exists specifically to avoid N authorization round-trips.

### How to gate dashboard UI with RBAC

Dashboard code should never hand-roll scope checks — use the shared primitives so a change to `useRBAC` or `<RequireScope>` flows through the whole app.

1. **Rendering gates — use `<RequireScope>`.** Pick the level that matches what you want the un-entitled user to see:
   - `level="page"` around a full route component renders an Unauthorized fallback page.
   - `level="section"` around a block hides it entirely.
   - `level="component"` around a button or input renders disabled with a tooltip reason.

   ```tsx
   <RequireScope scope="org:admin" level="component" reason="Admin only">
     <Button onClick={() => setDialogOpen(true)}>New API key</Button>
   </RequireScope>
   ```

2. **Multi-scope gates.** Pass an array and set `all` to switch between OR (default) and AND logic: `<RequireScope scope={["org:read", "org:admin"]} level="page">`.

3. **Resource-specific gates.** Pass `resourceId` when the scope only applies to a specific resource: `<RequireScope scope="mcp:write" resourceId={toolsetId} level="component">`.

4. **Imperative checks — use `useRBAC`.** When you need the scope result as a value (to compute a class name, skip an effect, pick a label), pull from the hook instead of wrapping markup:

   ```tsx
   const { hasScope, isLoading } = useRBAC();
   const canEdit = hasScope("mcp:write", toolsetId);
   ```

5. **The `Scope` string you pass must match the server.** Import from `client/dashboard/src/pages/access/types.ts` (re-exported via `useRBAC` for convenience). If TypeScript complains about an unknown scope, you're missing the union update from the server scope add (see "How to add a new scope to an existing resource type").

### How to inspect the caller's grants

- **In the dashboard**: `const { grants } = useRBAC();` returns the raw `RoleGrant[]`. Prefer `hasScope` for gating; reach for `grants` only when you need to render them (the access page itself, diagnostics, dev overlays).
- **In Go handlers**: `authz.GrantsFromContext(ctx)` returns the grants on the request context after the engine's `PrepareContext` middleware has run.
- **Over the API**: `GET /rpc/access.listUserGrants` returns the caller's effective grants.

## Role hierarchy at a glance

- `admin` — every scope. Write implies read via `scopeExpansions`, so admins can exercise every read operation transitively.
- `member` — the read-and-connect subset.
- Resource scoping — a grant's selector either names a specific resource (`{"resource_kind":"project","resource_id":"proj_123"}`) or wildcards it (`{"resource_kind":"*","resource_id":"*"}` via `authz.WildcardResource`). A grant value of `*` matches anything for that selector key.
- `root` (`authz.ScopeRoot`) — held only by service-internal overrides; satisfies every check.

## Relevant mise tasks

| Task                       | Purpose                                                                                                                                                            |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `mise run gen:goa-server`  | Regenerate `server/gen/access/**` after editing `server/design/access/design.go`.                                                                                  |
| `mise run gen:sdk`         | Regenerate the SDK and OpenAPI so dashboard/CLI consumers see the new scope vocabulary.                                                                            |
| `mise run gen:sqlc-server` | Regenerate `server/internal/access/repo/` when `queries.sql` changes. Requires `mise run infra:start` (sqlc connects to the local Postgres to type-check queries). |
| `mise run lint:server`     | Catches `exhaustruct` violations in the scope/grant structs.                                                                                                       |
| `mise run test:server`     | Runs the scope-count assertions and RBAC tests. Filter with `./internal/authz/... ./internal/access/...` when iterating.                                           |

## Maintaining this skill

This file documents conventions that evolve over time. Adding a new scope, resource type, or tweaking system-role defaults is already covered by "Jobs to be done" — those don't require skill edits. Structural changes do. Update this skill in the same commit when you make any of the following kinds of changes:

- Changing the `<resource>:<verb>` scope naming convention.
- Adding or removing a system role beyond `admin` and `member`.
- Replacing `authz.Engine` as the central enforcer, or changing its method set (`Require`, `RequireAny`, `Filter`, `PrepareContext`, `ShouldEnforce`, etc.) or constructor signature.
- Moving authorization primitives back into `access` or into a new package — the `authz` / `access` split is deliberate and load-bearing for import-cycle reasons.
- Changing the `Check` struct shape (currently `{Scope, ResourceKind, ResourceID, Dimensions}`) or the `Selector` type's matching rules.
- Adding a new selector dimension key (currently `tool`, `disposition` for MCP) — including changes to `allowedSelectorKeys` or `validDispositions` in `authz/selector.go`, or to the matching `SelectorModel` enums in the design file.
- Changing scope-expansion semantics (e.g. how `scopeSubScopes` is computed from `scopeExpansions`, or introducing transitive expansion). The expansion algorithm currently emits one entry per scope level (relying on selector matching to handle wildcards) — switching back to per-scope×per-resource enumeration would change the perf profile and is worth re-documenting.
- Changing where the full-access scope catalogue lives (currently inline in `access.ListGrants` and mirrored by `expectedFullAccessScopes` in tests), or where `ListScopes` is populated.
- Moving the hand-maintained client scope vocabulary out of `client/dashboard/src/pages/access/types.ts`, or changing the three-place-enum-lockstep count in the design file. Same applies if the `ANNOTATION_TO_DISPOSITION` / `DISPOSITION_TO_ANNOTATION` maps move out of that file.
- Changing the auth context invariant — e.g. if `ActiveOrganizationID` becomes optional, or a new invariant field is added.
- Changing or replacing the dashboard's RBAC primitives — `useRBAC` return shape (including `selectorMatches`/`resourceKindForScope` helpers), `<RequireScope>` levels/props, or the SDK hook the dashboard reads grants from.
- Renaming or replacing the canonical Go grant constructor (`authz.NewGrant`, `authz.NewGrantWithSelector`, `authz.NewSelector`) — every test in the codebase is wired through these.
- Adding a new RBAC-relevant mise task that belongs on the cheat sheet.
- Changing the test-helper surface in `authztest` (e.g. renaming `WithExactGrants` or adding a new canonical helper tests should use).

## Cross-references

- `gram-management-api` — the `access` service itself, and every service that gates handlers with `authz.Require`, follows that skill's flow.
- `gram-audit-logging` — role and member mutations emit audit events via `server/internal/audit/access.go`; subjects are `access_role` and `access_member`.
- `golang` — error handling through `oops`, the no-defensive-checks rule for `ActiveOrganizationID`, the `setup_test.go` / black-box test conventions used by RBAC tests.
- `frontend` — everything under `client/dashboard/src/pages/access/` (component structure, `cn()`/Moonshine styling, React Query usage).
- `postgresql` — the `principal_grants` (with `selectors JSONB NOT NULL`), `roles`, and related tables backing the `access/repo` SQLc package.
- `mise-tasks` — when modifying the `.mise-tasks/gen/*.sh` scripts referenced above.
