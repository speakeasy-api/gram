# User Sessions Enhancements — Design

**Date:** 2026-06-16
**Status:** Approved (design), pending implementation plan
**Builds on:** `2026-06-15-user-sessions-feed-design.md` (the base feature: enriched `userSessions.list`, home widget, Connect → User Sessions page with revoke).

## Problem

The base User Sessions feed shipped. Five follow-on asks:

1. Surface a server's active sessions on the **MCP server Authentication tab** (every server).
2. Add **filters** to the User Sessions page: status, client, user, MCP server.
3. Let an operator **revoke** a session from a **right-click / extra-options (⋮) menu**.
4. Clear, brand-aligned **color coding** of session status.
5. Expose **read-only platform tools** so the project assistant can fetch/read user sessions.

## Non-goals

- No assistant write/revoke (read-only tools only).
- No changes to how sessions are minted.
- No DB migration (read-path + additive query/params only).

## Existing building blocks (verified)

- Base service `userSessions`: `listUserSessions` (filters `subject_urn`, `user_session_issuer_id`, `status`; cursor pagination), `revokeUserSession`. Query: `server/internal/usersessions/queries.sql` (`ListUserSessionsByProjectID`).
- Enriched `UserSession` type (SDK): `id`, `subjectUrn`, `subjectType` (user/apikey/anonymous), `subjectDisplayName?`, `clientName?`, `issuerSlug`, `expiresAt` (Date), `revokedAt?` (Date), `createdAt`, `updatedAt`.
- Frontend: `pages/connect/UserSessions.tsx` (inline `SessionRow` + revoke confirm `Dialog`, status filter, `useUserSessionsInfinite`), `components/project/UserSessionsCard.tsx` (home widget), `lib/user-session-status.ts` (`sessionStatus`, `subjectLabel`; status color map duplicated here and in both components).
- Audit-log facets pattern (model to follow): `auditlogs.listFacets` → `AuditLogFacetOption {value, display_name, count}`; FE `FacetSelect` at `client/dashboard/src/components/auditlogs/feed.tsx`.
- Filter/menu/badge primitives: `components/ui/select.tsx`, `components/ui/context-menu.tsx` (Trigger/Content/Item, `variant: default|destructive`), `components/ui/more-actions.tsx` (⋮ DropdownMenu), `components/ui/badge.tsx` (variants `default|secondary|destructive|warning|outline`).
- MCP new page: `pages/mcp/x/MCPServerDetails.tsx`; auth card `pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection.tsx` reads `mcpServer.userSessionIssuerId`; sections stack in `tabs/settings/SettingsTab.tsx` (`space-y-10`); compound `SettingsSection` component.
- Platform tools: `server/internal/platformtools/registry.go` (factory list → `BuildExecutors()`), `core/types.go` (`PlatformToolExecutor` interface: `Descriptor()` + `Call(ctx, env, payload, wr)`), example `platformtools/logs/search.go`, wired to the assistant via `assistantPlatformExtras` in `cmd/.../start.go`.

## Design

### A. Shared frontend module (foundation)

Eliminate duplication and centralize so the page, the MCP embed, and the home widget share one implementation:

- `client/dashboard/src/lib/user-session-status.ts` — keep `sessionStatus`/`subjectLabel`; add a single status→presentation map (badge variant + dot class). Remove the duplicated `STATUS_DOT` maps from `UserSessions.tsx` and `UserSessionsCard.tsx`.
- `client/dashboard/src/components/sessions/SessionStatusBadge.tsx` — renders a themed `Badge` from `sessionStatus(session)`. Mapping: **active → `default`/success-toned**, **expired → `secondary`** (muted), **revoked → `destructive`**. Uses the existing semantic `Badge` variants (already brand-themed); no hard-coded brand hexes.
- `client/dashboard/src/components/sessions/SessionRow.tsx` — the row + revoke confirm `Dialog`, extracted from `UserSessions.tsx`. Revoke is reachable two ways: (i) wrapping the row in `ContextMenu` (right-click) and (ii) a trailing ⋮ `MoreActions` menu. Both open the same confirm dialog → `useRevokeUserSessionMutation`. Props: `session`, `onRevoked`, `canRevoke?` (default derived: status === "active"). Revoke item uses the destructive variant; hidden for non-active sessions.

`UserSessions.tsx` and `UserSessionsCard.tsx` are refactored to consume these shared pieces (behavior unchanged for the widget).

### B. Filters + facets (#2)

**Backend:**

- Add `userSessions.listFacets` method (mirror `auditlogs.listFacets`): `GET /rpc/userSessions.listFacets`, project-scoped (`project:read`), returning `UserSessionFacets { clients: []FacetOption, users: []FacetOption, servers: []FacetOption }`, each `FacetOption {value, display_name, count}`.
  - `clients`: group by `user_session_client_id` → value = client id, display = `client_name`, count.
  - `users`: group by `subject_urn` where kind = user → value = `subject_urn`, display = resolved display name/email, count.
  - `servers`: group by `user_session_issuer_id` → value = issuer id, display = issuer `slug`, count.
  - Facets computed over all project sessions (incl. revoked) so any status filter can be combined.
- Add a `client_id` filter param (uuid) to `listUserSessions` (status, `user_session_issuer_id`, `subject_urn` already exist). New repo query `ListUserSessionFacets` (grouped counts) + add `client_id` predicate to `ListUserSessionsByProjectID`.
- Regenerate goa → sqlc → sdk.

**Page:** four filters above the list — **Status** (`Select`, the enum incl. "All"), **Client** / **User** / **MCP server** (`FacetSelect` fed by `useUserSessionsListFacets`). Each composes into `useUserSessionsInfinite({ status, clientId, subjectUrn, userSessionIssuerId })`.

### C. MCP auth-card embed (#1)

- `client/dashboard/src/pages/mcp/x/tabs/settings/sections/authentication/McpServerSessionsPanel.tsx` — rendered at the **bottom of `AuthenticationSection`** (after existing auth content), so it appears on every server's Authentication tab.
- If `mcpServer.userSessionIssuerId` is null → empty state ("This server isn't gated by a session issuer.").
- Else → `useUserSessionsInfinite({ userSessionIssuerId, status: "active" })`, rendering shared `SessionRow`s (revoke included; gated by the auth tab's existing MCP scope). Defaults to active sessions.

### D. Revoke menu (#3) + color (#4)

Delivered by the shared `SessionRow` + `SessionStatusBadge` from (A): right-click `ContextMenu` and ⋮ `MoreActions`, both with a destructive "Revoke" → confirm `Dialog` → `revokeUserSession`; status shown via the themed badge.

### E. Assistant platform tools (#5), read-only

New package `server/internal/platformtools/usersessions/` with two executors implementing `core.PlatformToolExecutor` (pattern from `logs/search.go`):

- **`list_user_sessions`** — `Descriptor` name `platform_list_user_sessions`; input schema: `status?` (enum active/expired/revoked/all), `user_session_issuer_id?`, `subject_urn?`, `client_id?`, `cursor?`, `limit?`. `Call` resolves project from the tool-call env, runs `ListUserSessionsByProjectID`, builds the enriched view (reusing `mv.BuildUserSessionListView`), writes JSON `{items, next_cursor}`. No token material (projection already excludes `refresh_token_hash`).
- **`get_user_session`** — name `platform_get_user_session`; input `{id}`; returns one enriched session or a not-found error.

Register both factories in `platformtools/registry.go`; add to `assistantPlatformExtras` wiring in `start.go`. Project scoping enforced from env; no revoke/write executor.

## Components & boundaries

| Unit                           | Responsibility                           | Depends on                                 |
| ------------------------------ | ---------------------------------------- | ------------------------------------------ |
| `lib/user-session-status.ts`   | status derivation + presentation map     | SDK `UserSession`                          |
| `SessionStatusBadge`           | render status as themed badge            | status lib, `Badge`                        |
| `SessionRow`                   | row + revoke (context menu + ⋮ + dialog) | status lib, badge, revoke hook, menus      |
| `McpServerSessionsPanel`       | server's active sessions in auth tab     | `SessionRow`, infinite hook                |
| Page filters                   | 4 facet/select filters                   | `useUserSessionsListFacets`, `FacetSelect` |
| `userSessions.listFacets` (BE) | facet options + counts                   | repo `ListUserSessionFacets`               |
| `client_id` param (BE)         | extra list filter                        | `ListUserSessionsByProjectID`              |
| `platformtools/usersessions`   | read-only assistant tools                | repo queries, `mv` builder                 |

## Sequencing (one spec, 3 workstreams)

1. **Backend**: facets endpoint + `client_id` param (B) and platform tools (E) → regen goa/sqlc/sdk + build SDK.
2. **Shared FE module** (A) → depends on regenerated SDK.
3. **Surfaces**: MCP embed (C) + page filters/menu/color (D) → depend on A + SDK.

## Testing

- **Backend:** `ListUserSessionFacets` (correct grouping/counts for clients/users/servers, project-scoped); `client_id` filter narrows results; `listFacets` handler authz (`project:read`). Platform tools: `list_user_sessions` (filters honored, project-scoped, JSON shape, `refresh_token_hash` absent), `get_user_session` (by id, not-found path), both read-only.
- **Frontend:** `SessionRow` revoke via right-click AND ⋮ (both open dialog, call mutation, refetch); `SessionStatusBadge` variant per status; page renders + composes the 4 filters; `McpServerSessionsPanel` empty state when no issuer and list when present.

## Decisions (resolved during brainstorming)

1. MCP embed on the **new** `mcp/x` page, bottom of the Authentication tab, on **all** servers (empty state when no issuer).
2. Filters: Status, Client (`client_name`), User (subject), MCP server (issuer) — **server-side facets**.
3. Assistant tools: exactly two, **read-only** (`list_user_sessions`, `get_user_session`); no aggregates.
4. Revoke from **both** right-click context menu and ⋮ menu.
5. Status colors via existing **semantic `Badge` variants** (brand-themed): active→success, expired→secondary, revoked→destructive.

## Risks / verify during planning

- Exact `FacetSelect` props + `useAuditLogFacets`-equivalent generated hook name for the new `listFacets`.
- `more-actions.tsx` / `context-menu.tsx` exact exported API and how to compose both around one row without event conflicts.
- Platform-tool env: how `toolconfig.ToolCallEnv` exposes the active project id; confirm against `logs/search.go`.
- `AuthenticationSection` insertion point + `SettingsSection` compound API for the embedded panel.
- Whether the home widget should also adopt `SessionStatusBadge` (keep visual parity; low risk).
