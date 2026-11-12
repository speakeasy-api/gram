# RBAC reference

Background for the [gram-rbac skill](SKILL.md). Where this file and SKILL.md differ, SKILL.md wins: it describes the registry-driven model and the typed request API. Paragraphs here that describe the derived tables now say where they are derived from.

## Concepts and terminology

**Scope.** A named permission that authorizes an operation on a particular kind of resource.

**Resource type.** The kind of resource a scope protects — for example `org`, `project`, `assistant`, or `mcp`. Every scope has exactly one resource type.

**Scope expansion.** Higher-privilege scopes satisfy lower-privilege ones. `mcp:write` satisfies a `mcp:read` check. `mcp:connect` is separate: neither `mcp:read` nor `mcp:write` satisfies it, because viewing or managing a server is not permission to call its tools. It is each permission's `Implies` in `registry.Definitions`; `scopeExpansions` in `authz/tables_gen.go` is generated from it (key = required scope, value = higher-privilege scopes that also satisfy it).

**Selector.** A `map[string]string` of constraints attached to a grant or check. Always carries `resource_kind` and `resource_id` (both required); MCP scopes additionally allow `tool` and `disposition`. Wildcards are explicit values — `{"resource_kind":"*","resource_id":"*"}`, never empty `{}`. Defined in [server/internal/authz/selector.go](server/internal/authz/selector.go).

**Selector matching.** A grant selector satisfies a check selector when, for every key the grant constrains, either the values are equal or the grant value is `"*"`. Keys present on the grant but absent from the check are skipped — this is what lets a disposition-scoped grant (`{"disposition":"read_only"}`) still satisfy a connection-level check that doesn't constrain disposition. Auth client checks (`mcp:auth`, `mcp:blocked_auth`) are the exception: they always match strictly, and the `connection_owner` value `$represented_user` matches only the represented user the engine binds from trusted context. See "Auth client checks" in docs/rbac.md. Delegated policies older than version 5 read each `mcp:connect` grant as an auth client ceiling at admission (`runtimepolicy.admitStoredPolicy`); never raise `IssuedDelegatedPolicyVersion` to 5 until the agent session, workload and device agent encoders state `mcp:auth` themselves.

**Grant.** A tuple of `{Scope, Selector}` held by a principal. The API-visible forms are `RoleGrant` (carrying `Selectors []Selector`) and `ListRoleGrant` (which also carries the transitively-implied `sub_scopes`). Use `authz.NewGrant(scope, resourceID)` to construct one — it derives the selector's `resource_kind` from the scope family.

**Principal.** Who holds a grant — a `urn.Principal` with a type (user, role, agent, api_key) and an id. An `api_key:<api_keys.id>` principal holds only its own `mcp:connect`/`mcp:auth` policy, written through `keys.create` or provisioned by the auth client backfill (see docs/rbac.md, "Default auth permissions"); its grants always match strictly, never outrank a block, and never inherit `user:all` or the creator's grants.

**Principal precedence.** Blocklist exclusions (`*:blocked_*`) normally subtract from allows regardless of which principal holds either. One exception: a grant made directly to the user (`user:<id>`) that names a concrete resource (`resource_id` not `*`) outranks a block the user inherits from a role or `user:all`. Agent grants never outrank, because agent owners can write them without being administrators. The user's own blocks always apply, wildcard direct grants never outrank, roles and `user:all` share one level, a narrowed direct grant outranks only as far as it reaches, and `risk_policy:bypass` is never outranked. The rule lives in `server/internal/authz/precedence.go` (`IsDirectGrant`, `DirectOverrideGrants`, `ExclusionYieldsToDirectGrants`) and is mirrored in `ListAccessibleMCPServersForUser` / `ListAccessibleSkillsForUser`, `runtimepolicy.DelegationContained`, `access.listGrants` (`direct_selectors`), and the dashboard's `useRBAC` and per-server access list. Any new code that evaluates exclusions outside `authz.Engine` must apply it too.

**Dimensions.** Optional narrowing keys on a `Check` beyond `resource_id`. Today: `tool` and `disposition` for MCP scopes (see [server/internal/authz/checks.go](server/internal/authz/checks.go) and `MCPToolCallCheck`), `project_id` for MCP, environment, assistant, inference and `mcp:auth` scopes, and `client_type`, `connection_owner` and `upstream_client` for `mcp:auth` (built only through `MCPAuthClientCheck`). A `project_id` dimension is how a per-resource check inherits project-wide grants: the check names the resource and its project, so `{"resource_id":"*","project_id":P}` covers every resource in P while `{"resource_id":R}` covers only R (`MCPCheck`, `AssistantCheck`). Every check on the `mcp` resource kind (`mcp:read`, `mcp:write`, `mcp:connect` and their blocks) must carry `project_id`: `requiredCheckDimensions` in `authz/checks.go` makes the engine reject one without it as invalid, since matching would otherwise skip a project-bounded grant's bound. Never build such a check as a raw `authz.Check` literal; use `MCPCheck` or `MCPToolCallCheck` with `ProjectID`. `mcp:auth` checks are a separate kind (`mcp_auth_client`), built only through `MCPAuthClientCheck` with `MCPAuthClientDimensions.ProjectID`; they use strict matching, so a grant key the check omits never matches. Allowed keys per scope family are enforced by `ValidateSelector`; new dimensions must be added to `allowedSelectorKeys` in `selector.go`.

**Disposition.** A snake_case bucket derived from MCP tool annotation hints — `read_only`, `destructive`, `idempotent`, `open_world`. Constants live in `authz/selector.go`; `conv.DispositionFromAnnotations(annotations)` is the canonical conversion from `*types.ToolAnnotations`.

**System role.** A built-in role shipped with the server. Speakeasy defines two: **admin** (every default scope except `chat:*` and `log:read`, which transcripts and raw tool logs need granted explicitly) and **member** (the read-and-connect subset). Constants `authz.SystemRoleAdmin` and `authz.SystemRoleMember`.

**Enforcement.** Inside a handler, authorization is an explicit one-line check: the handler names the scope (and resource, if project-scoped) it needs, and the RBAC engine either allows the call or returns a forbidden error.

**Auth context invariant.** Organization-scoped handlers can assume `ActiveOrganizationID` is populated. During login, session authentication can briefly produce an org-less context; `Engine.PrepareContext` handles that middleware boundary by installing zero grants instead of trying organization-scoped principal resolution. Endpoints already exposed during login keep their explicit org-less behavior: for example, `access.ListGrants` returns no grants, `productfeatures.GetProductFeatures` returns unauthorized, and `auth.Info` continues the login flow. Do not spread defensive empty-org checks into other handlers.

## Server

RBAC is split across two Go packages: `server/internal/authz/` holds the enforcement primitives, and `server/internal/access/` implements the Goa management service that exposes them over HTTP. When adding new authorization primitives (scopes, checks, enforcement logic) edit `authz`; when adding or changing the management API (role/member endpoints) edit `access`.

### `authz` package — the enforcer

Scope vocabulary, grant types, and enforcement logic are defined here. `authz`'s imports are deliberately minimal (DB, logger, cache, WorkOS, urn) so any package that gates on RBAC can depend on it without import cycles — never add an import to `authz` from a package that transitively depends on `authz`, since the split exists specifically to prevent the cycles that motivated it.

**Scope declarations.** Every scope is declared by a permission in `registry.Definitions` (`authz/internal/registry/definitions.go`). The `Scope*` constants are generated into `scopes_gen.go` by `mise run gen:authz`; string values follow `<resource>:<verb>` (e.g. `mcp:read`, `org:admin`). `ScopeRoot` is reserved for service-internal superadmin overrides. `tables_gen.go` holds the generated `scopeExpansions`, `scopes.go` its inverse; `CalculateSubScopes(scope)` exposes the inverse to callers.

**System role grants.** `SystemRoles` in `registry.Definitions` declares the admin and member defaults; `SystemRoleGrants` in `server/internal/authz/grants.go` is built from it. Adding a scope usually means adding it to admin, and optionally to member if end users should get it by default. `SeedSystemRoleGrants` upserts the full set; `SyncGrants` upserts grants for a single role slug.

**`authz.Engine`.** The central enforcer. Methods: `PrepareContext`, `Require(ctx, checks...)`, `RequireAny`, `RequireAnyUnblocked`, `Can`, `Evaluate`, `Filter(ctx, checks)`, `FindMatched`, `ShouldEnforce`, `GetScopeOverrides`. Constructed in `server/cmd/gram/start.go` via `authz.NewEngine(logger, db, challengeLogging, membership, opts...)` and injected into every service that gates on RBAC. RBAC is always enforced for eligible authenticated requests; the `MembershipFetcher` is the WorkOS client used for role-slug lookups.

**Organization provisioning.** `authz.Provisioner` seeds both built-in roles and their grants for every organization created through Speakeasy. `ProvisionOrganizationAdmin` performs that seed and assigns the first user to `SystemRoleAdmin` in one transaction. New organizations (provisioning, admin create, and WorkOS organizations created by their `organization` event) also get the `user:all` personal `mcp:auth` default; reconciling an existing organization never adds it back. The `identity` leaf package never imports `authz`. WorkOS organization event reconciliation calls `SeedSystemRoleGrantsTx` for organizations discovered through WorkOS. Default `mcp:auth` grants and legacy key policies the backfill provisions are recorded in `auth_permission_provisions`; any new explicit grant write path must mark a matching row taken over after writing (`authz.MarkAuthPermissionTakenOverTx`), as `UpsertPrincipalGrant` callers do.

**`authz.Check`.** The thing a handler asks `Require` to enforce, defined in `server/internal/authz/access.go`. Build it with a typed ref from `refs_gen.go` (see SKILL.md); raw literals remain only in packages not yet migrated.

**`authz.Filter` for list endpoints.** When a handler lists resources the caller might only partially own, `s.authz.Filter(ctx, checks) ([]string, error)` returns the resource IDs of the checks the caller satisfies. The standard pattern is: gather candidate IDs from the repo, call `Filter`, then rebuild the response from the allowed IDs. Prefer this over a post-hoc per-item `Require` loop. Canonical call sites: `server/internal/projects/impl.go` (projects list) and `server/internal/toolsets/impl.go` (toolsets list).

**Auth context accessor.** `contextvalues.GetAuthContext(ctx)` returns the current `*AuthContext`. RBAC-relevant fields: `ActiveOrganizationID`, `ProjectID`, `UserID`, `Email`, `IsAdmin`, `APIKeyID`, `SessionID`. `AccountType` is billing metadata and does not control RBAC enforcement.

**Scope overrides.** A local-dev/superadmin header can inject a restricted grant set for the request, parsed in `override.go` and surfaced via `Engine.GetScopeOverrides`. `access.ListGrants` returns the override set verbatim when active so the dashboard reflects what the engine will enforce.

**Error model.** `errors.go` defines sentinel errors (`ErrDenied`, `ErrMissingGrants`, `ErrNoChecks`, `ErrInvalidCheck`) and typed errors (`DeniedError`, `InvalidCheckError`). The engine maps these to `oops` codes — `ErrDenied` → `oops.CodeForbidden`, everything else → `oops.CodeUnexpected` with a logged message.

**Grant loading.** `LoadGrants(ctx, db, orgID, principals)` reads the principal URN set and returns the flattened `[]Grant`. Called by both `Engine.PrepareContext` (middleware path) and `access.ListGrants` (user-facing). Each row's `selectors` JSONB is parsed via `SelectorFromRow`. It returns exactly the stored rows and never synthesizes defaults: built-in role grants are editable per organization, so an empty grant set for a scope family may be an explicit revocation. When new default scopes must reach existing organizations, write the rows with a one-time operator backfill (as `server/cmd/tools/migrations/assistantrolegrants` does for the assistant scopes) instead of filling them in at load time.

**Temporary grants.** A `principal_grants` row with `expires_at` set is a temporary grant, given to one principal (member, `user:all`, role or active agent) through `access.createTemporaryGrant` and ended by its deadline or `access.revokeTemporaryGrant` (`server/internal/access/temporary_grants.go`). `ListTemporaryGrants` takes one principal and returns its active grants by default, at most 500, soonest deadline first; `include_expired` adds expired rows as history. Any new SQL that reads `principal_grants` for authorization or for showing current access must add `(expires_at IS NULL OR expires_at > clock_timestamp())` (only the temporary grant history list, `ListTemporaryGrants` with `include_expired`, reads expired rows); audience reads and deletes and anything that derives permanent configuration from grants (backfills, provisioning) must read `expires_at IS NULL` only. Permanent writes clear `expires_at` on conflict, and nothing but the temporary grant endpoints may delete a row by `expires_at`. See "Temporary grants" in docs/rbac.md.

**Sync semantics.** `SyncGrants` distinguishes nil from empty: `RoleGrant{Selectors: nil}` writes a single wildcard row; `RoleGrant{Selectors: []Selector{}}` writes nothing (no access). Each non-nil selector is validated by `ValidateSelector` before insert.

### `access` package — the management API

`server/internal/access/` implements the Goa `access` service on top of `authz`. Every handler calls `s.authz.Require(...)` with the appropriate scope before doing work. The package also owns `queries.sql` and the generated `server/internal/access/repo/` SQLc package that both `access` and `authz` use to read and write grant rows.

**Scope metadata.** `ListScopes` in `server/internal/access/impl.go` supplies scope slugs, descriptions, resource types, and visibility to dashboard scope pickers. The RBAC override toolbar also uses this catalog: user-visible scopes and groups appear automatically, and `resourceType` controls narrowing (plugin scopes use `project`). Do not maintain a duplicate frontend scope list.

**Override recovery.** `ListScopes` requires `org:read`. The override editor always renders an `org:read` toggle, but does not force it on. If catalog access is denied, saved choices and cached metadata remain available. With no cached catalog, the editor still shows `org:read` and saved scopes under a fallback group; undiscovered catalog scopes appear after recovery. Re-enabling `org:read` automatically clears stale organization restrictions and persists the change before refetching. Alternatively, disable the override.

**Full-access grant list.** `ListGrants` returns the full scope catalogue (`authz.ScopeCatalog()`) when enforcement is intentionally skipped, such as sessionless or API-key requests. The test expectation is `expectedFullAccessScopes` in `server/internal/access/listusergrants_test.go`.

**System role gating.** `isSystemRole(slug)` in `impl.go` checks against `authz.SystemRoleAdmin` and `authz.SystemRoleMember`. System roles cannot be renamed or deleted. Their grants are stored per organization and may be edited through `UpdateRole` (the Admin role must keep `org:admin`), and their members can be reassigned.

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
| `server/internal/authz/precedence.go`  | Principal precedence: direct concrete grants outrank blocklist exclusions inherited from roles or `user:all`.                                             |
| `server/internal/authz/provisioner.go` | New-organization provisioning for built-in role grants and the initial Admin assignment.                                                                  |
| `server/internal/authz/scopes.go`      | Scope type and helpers over the generated expansion tables.                                                                                               |
| `server/internal/authz/selector.go`    | `Selector` type, matching rules, `NewSelector`/`NewGrant` helpers, `ValidateSelector`, `ResourceKindForScope`, disposition vocabulary, `SelectorFromRow`. |
| `server/internal/authztest/helpers.go` | Test helpers other packages reuse for RBAC setup, including `WithExactGrants`.                                                                            |
| `server/internal/access/impl.go`       | Implementation of the `/rpc/access.*` Goa service.                                                                                                        |
| `server/internal/access/queries.sql`   | SQLc queries for principals, grants, roles, and members. Regenerates `server/internal/access/repo/` via `mise run gen:sqlc-server`.                       |

### Generated files

| Path                                            | Generator                                                                                                                      |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `server/gen/access/`, `server/gen/http/access/` | `mise run gen:goa-server` from `server/design/access/design.go`.                                                               |
| `server/internal/access/repo/`                  | `mise run gen:sqlc-server` from `server/internal/access/queries.sql` (via the `access` stanza in `server/database/sqlc.yaml`). |

## Server-client contract

Scope and resource-type changes on the server ripple into the generated SDK types (via `gen:sdk`) and a few hand-maintained docs and tests — adding a scope is not purely a server-package change.

**HTTP routes** (design: `server/design/access/design.go`):

- `/rpc/access.listScopes` — every scope the server knows about, with `resource_type` and description.
- `/rpc/access.listRoles`, `getRole`, `createRole`, `updateRole`, `deleteRole` — custom role CRUD.
- `/rpc/access.listMembers`, `updateMemberRole` — org membership and role assignment.
- `/rpc/access.listUserGrants` — the caller's effective grants.

**Scope enum.** `server/design/access/design.go` reads the scope enum from `authz.ScopeNames()`, so it follows the model after `mise run gen:goa-server`. Adding a resource type still means adding it to `SelectorModel.resource_kind`'s enum by hand.

**Generated SDK types.** `client/dashboard/src/sdk/src/models/components/scopedefinition.ts`, `rolegrant.ts`, `listrolegrant.ts`, `selector.ts`, etc. Regenerated by `mise run gen:sdk` after every design change.

**UI-owned access-page types.** `client/dashboard/src/pages/access/types.ts` owns the dashboard-only abstractions layered over the generated SDK: the `ResourceType` string-literal union, `AnnotationHint`, `CustomTab`, `ActivePanel`, `PolicyEffect`, `ScopeRule`, a UI `RoleGrant` interface (`selectors: Selector[] | null`, distinct from the SDK's `RoleGrant`), and `toRoleSlug`. It also owns the `ANNOTATION_TO_DISPOSITION` / `DISPOSITION_TO_ANNOTATION` maps that mirror the disposition vocabulary in `authz/selector.go` — keep these in lockstep when adding or renaming dispositions. It `import`s `Scope` / `Selector` / `SelectorDisposition` from the SDK for local use in those definitions.

## Client

The dashboard pages under `client/dashboard/src/pages/access/` render membership and role management on top of the generated SDK and the `listScopes` response. RBAC-aware UI across the rest of the dashboard gates itself through a shared hook and component.

### Conventions

**`useRBAC` hook.** `client/dashboard/src/hooks/useRBAC.ts` wraps the generated `useGrants` React Query hook and exposes `hasScope(scope, resourceId?, projectId?)`, `hasAllScopes(scopes, resourceId?, projectId?)`, `hasAnyScope(scopes, resourceId?, projectId?)`, plus `isLoading`, `grants`, and `error`. Returns `false` from the `has*` checks while grants are loading. The module also exports `selectorMatches(grant, check)` and `resourceKindForScope(scope)` — direct mirrors of the server-side helpers in `authz/selector.go` — for code that needs parity with backend matching outside the standard `hasScope` flow.

**`RequireScope` component.** `client/dashboard/src/components/require-scope.tsx` is the primary rendering gate. Props: `scope: Scope | Scope[]`, `all?: boolean` (AND vs OR when multiple scopes), `resourceId?: string`, `projectId?: string` (the project the resource belongs to, for scope families with a `project_id` dimension), `level: "page" | "section" | "component"`, `children`, and level-specific extras (`fallback` for page/section, `reason`/`className` for component).

- `level="page"` — renders a full Unauthorized fallback page when the scope is missing.
- `level="section"` — hides the children entirely.
- `level="component"` — renders disabled with a tooltip explaining why (good for buttons and inputs).

**Scope vocabulary import.** `useRBAC`, `RequireScope`, and the access pages import the `Scope` union directly from the generated SDK (`@gram/client/models/components/rolegrant.js`). That union is regenerated from the server scope enum by `gen:sdk`, and the design reads that enum from the generated scope constants, so a new scope reaches the client gates after `gen:goa-server` and `gen:sdk`.

**Dashboard grant reference.** [docs/rbac.md](../../../docs/rbac.md) contains the "Dashboard Grant Reference" table that maps dashboard pages and actions to the grants required to use them. Whenever adding a new scope, changing a system-role grant default, adding a new dashboard RBAC gate, or changing the grant required by a dashboard feature, update that table in the same change. The table is the first place to answer questions like "what grant is required to create a project?"

### Non-generated files

| File                                                                                                     | Purpose                                                                                                                       |
| -------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `client/dashboard/src/components/require-scope.tsx`                                                      | `RequireScope` gating component — page, section, and component-level rendering gates.                                         |
| `client/dashboard/src/hooks/useRBAC.ts`                                                                  | `useRBAC` hook — scope checks and raw grants for the dashboard.                                                               |
| `client/dashboard/src/pages/access/Access.tsx`                                                           | Top-level access page shell.                                                                                                  |
| `client/dashboard/src/pages/access/ChangeRoleDialog.tsx`, `CreateRoleDialog.tsx`, `DeleteRoleDialog.tsx` | Role and member-role mutation dialogs.                                                                                        |
| `client/dashboard/src/pages/access/MembersTab.tsx`, `RolesTab.tsx`                                       | The two tabs of the access page.                                                                                              |
| `client/dashboard/src/pages/access/ScopePickerPopover.tsx`                                               | Scope selection UI.                                                                                                           |
| `client/dashboard/src/pages/access/types.ts`                                                             | UI-only access-page types (`ResourceType`, `ScopeRule`, UI `RoleGrant`) and disposition maps. (See "Server-client contract".) |

## Jobs to be done

### How to change system role defaults

Use this when adjusting what `admin` or `member` gets out of the box. Prefer additive changes — removing a grant from a shipped role is an observable permissions change for existing users.

1. Edit `SystemRoles` in `server/internal/authz/internal/registry/definitions.go`, then run `mise run gen:authz`.
2. Update `expectedFullAccessScopes` in `server/internal/access/listusergrants_test.go` if the admin set changed.
3. Update the "Dashboard Grant Reference" table in [docs/rbac.md](../../../docs/rbac.md) if the default grant change affects what a built-in role can do in the dashboard.
4. Consider whether existing orgs' grant tables need a migration to reflect the new defaults; new orgs pick up defaults automatically during organization provisioning.
5. Run `mise run lint:server` and `mise run test:server`.

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

3. **Resource-specific gates.** Pass `resourceId` when the scope only applies to a specific resource: `<RequireScope scope="mcp:write" resourceId={toolsetId} projectId={projectId} level="component">`. For scope families with a `project_id` dimension (MCP, environments, assistants, inference), also pass `projectId` so project-wide grants match and grants for other projects do not (`mcp:auth` also carries one, but it cannot be checked in the dashboard: the server binds the connection owner): `hasScope("assistant:write", assistant.id, project.id)` for one assistant, `hasScope("assistant:write", project.id, project.id)` for project-level actions such as create, and `hasScope("assistant:read", undefined, project.id)` for "any assistant in this project".

4. **Imperative checks — use `useRBAC`.** When you need the scope result as a value (to compute a class name, skip an effect, pick a label), pull from the hook instead of wrapping markup:

   ```tsx
   const { hasScope, isLoading } = useRBAC();
   const canEdit = hasScope("mcp:write", toolsetId, projectId);
   ```

5. **The `Scope` string you pass must match the server.** Import `Scope` directly from the generated SDK (`@gram/client/models/components/rolegrant.js`). If TypeScript complains about an unknown scope, you're missing the union update from the server scope add (see "How to add a new scope to an existing resource type").

6. **Update the dashboard grant reference.** Any new or changed dashboard gate must update the "Dashboard Grant Reference" table in [docs/rbac.md](../../../docs/rbac.md), including page-level access, component/action-level access, resource selector target, and any notable server-side check that differs from the visible UI gate.

### How to inspect the caller's grants

- **In the dashboard**: `const { grants } = useRBAC();` returns the raw `RoleGrant[]`. Prefer `hasScope` for gating; reach for `grants` only when you need to render them (the access page itself, diagnostics, dev overlays).
- **In Go handlers**: `authz.GrantsFromContext(ctx)` returns the grants on the request context after the engine's `PrepareContext` middleware has run.
- **Within long-lived requests**: use `Engine.RefreshContext` before a later operation that must observe grant revocation. It clears resolved grant/policy snapshots and repeats admission while retaining the original credential constraints. Continue with the returned context and fail closed on error.
- **Admitted jobs**: capture the originally prepared policies with `CaptureAdmissionBoundary`, then `Apply` that boundary to callback contexts when a job must never gain newly granted authority. The boundary retains no request context, survives later refreshes, and every policy must allow the operation.
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

## Cross-references

- `gram-management-api` — the `access` service itself, and every service that gates handlers with `authz.Require`, follows that skill's flow.
- `gram-audit-logging` — role and member mutations emit audit events via `server/internal/audit/access.go`; subjects are `access_role` and `access_member`.
- `golang` — error handling through `oops`, the no-defensive-checks rule for `ActiveOrganizationID`, the `setup_test.go` / black-box test conventions used by RBAC tests.
- `frontend` — everything under `client/dashboard/src/pages/access/` (component structure, `cn()`/design-system styling, React Query usage).
- `postgresql` — the `principal_grants` (with `selectors JSONB NOT NULL`), `roles`, and related tables backing the `access/repo` SQLc package.
- `mise-tasks` — when modifying the `.mise-tasks/gen/*.sh` scripts referenced above.
