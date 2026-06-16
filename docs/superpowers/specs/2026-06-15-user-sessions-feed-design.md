# User Sessions Feed — Design

**Date:** 2026-06-15
**Status:** Approved (design), pending implementation plan

## Problem

When a user logs into a project, they cannot see the active user sessions —
the sessions clients hold _into_ the project's toolsets, established after an
OAuth/OIDC dance via `/mcp/{slug}/token`. We want a feed of these sessions,
analogous to the existing activity log, so an operator can see who/what is
connected and cut off a session when needed.

"User sessions" here means the `userSessions` concept (Gram-issued sessions for
clients connecting to issuer-gated toolsets), **not** `user_oauth_tokens` (the
credentials Gram holds _out to_ third-party MCP servers).

## Goals

- Show **active** user sessions as a read-only widget on the project home.
- Provide a full, filterable **User Sessions** page under the **Connect** nav
  section showing **all** sessions (Active / Expired / Revoked) with an inline
  **Revoke** action.
- Make each row human-readable (resolved subject, connecting client, gated
  toolset/issuer, status, timing) rather than a wall of UUIDs.

## Non-goals

- No changes to how sessions are minted or to the OAuth/OIDC flow.
- No `user_oauth_tokens` (outbound connection) surface.
- No database migration — this is read-path enrichment over existing tables.

## Existing building blocks

- **Service / API:** `userSessions` Goa service already exists with
  `listUserSessions` (`GET /rpc/userSessions.list`, cursor-paginated, filters
  for `subject_urn` and `user_session_issuer_id`) and `revokeUserSession`
  (`POST /rpc/userSessions.revoke`, soft-deletes + pushes `jti` to revocation
  cache). React hook `useUserSessions` is already declared via
  `x-speakeasy-react-hook`.
  - `server/design/usersessions/design.go`
  - `server/internal/usersessions/sessionhandlers.go`
  - `server/internal/mv/usersession.go`
  - `server/internal/usersessions/queries.sql` (`ListUserSessionsByProjectID`)
- **Data model:** `user_sessions` joins `user_session_issuers` (→ `slug`),
  `user_session_clients` (→ `client_name`), and a `subject_urn`
  (`user:<id>` | `apikey:<uuid>` | `anonymous:<mcp-session-id>`). Expiry from
  `expires_at` / `refresh_expires_at`; revoked = soft-deleted (`deleted_at`).
  - `server/database/schema.sql` (tables at ~748–857)
- **UI model to follow:** the audit log feed (fat backend query + thin `mv`
  builder + feed components + compact home widget).
  - `client/dashboard/src/components/auditlogs/feed.tsx`
  - `client/dashboard/src/pages/org/OrgAuditLogs.tsx`
  - compact widget precedent on project home:
    `client/dashboard/src/components/project/ActivityTimelineCard.tsx`
    (rendered by `ProjectDashboard.tsx`)

## Design

### 1. Backend (extend existing, no new service)

**Query** — `server/internal/usersessions/queries.sql`,
`ListUserSessionsByProjectID`:

- Already joins `user_session_issuers` for project scoping; additionally
  **select `iss.slug`**.
- **JOIN `user_session_clients`** to select `client_name` (nullable —
  `user_session_client_id` is nullable).
- **Resolve subject** via LEFT JOINs that parse the `subject_urn` prefix:
  - `user:<id>` → `users` (display name / email)
  - `apikey:<uuid>` → `api_keys` (key name)
  - `anonymous:<…>` → no row, label as Anonymous
- Add `include_revoked` narg: default false keeps `s.deleted IS FALSE`; true
  drops it so revoked rows are returned. Preserve existing `subject_urn`,
  `user_session_issuer_id`, `cursor` filters and `ORDER BY s.id DESC`.

**Goa type** — `server/design/usersessions/design.go`, `UserSession`: add
`issuer_slug`, `client_name` (nullable), `subject_type`
(`user`/`apikey`/`anonymous`), `subject_display_name` (nullable), `revoked_at`
(nullable, from `deleted_at`). Add `include_revoked` and `status` filter params
to `listUserSessions`.

**Model view** — `server/internal/mv/usersession.go`: map the new columns into
the extended type.

**Regeneration order:** `mise gen:goa-server` → `mise gen:sqlc-server` →
`mise gen:sdk`. No migration (no schema change).

### 2. Status (derived client-side)

No new backend state — derive from returned fields:

- **Revoked** — `revoked_at` is set.
- **Expired** — not revoked && `expires_at` ≤ now.
- **Active** — not revoked && `expires_at` > now.

### 3. Project-home widget (read-only)

New `UserSessionsCard` rendered alongside `ActivityTimelineCard` in
`ProjectDashboard.tsx`. Calls `useUserSessions({ status: "active" })`
(auto-filtered to active), shows up to **5** rows, "View all →" links to the
new page. No revoke action.

Row format:

> `[status dot]` **Subject** (resolved name / "API key: …" / "Anonymous") ·
> _client_name_ · gated by _issuer slug_ · started 5m ago · expires in 2h

### 4. Connect → User Sessions page (full)

- Register route `userSessions` in
  `client/dashboard/src/routes.tsx`.
- Add a `ScopeGatedNavItem` to the **Connect** group in
  `client/dashboard/src/components/app-sidebar.tsx` (after `deployments`).
- Page modeled on `OrgAuditLogs`: status filter (All / Active / Expired /
  Revoked), issuer + subject filters, cursor pagination + load-more footer,
  reusing audit-feed components/patterns where practical.
- Each row has an inline **Revoke** action (confirm dialog → `userSessions.revoke`
  → refetch). Revoke hidden/disabled for already-expired or already-revoked
  rows.

### 5. RBAC + audit

- Reuse **project-level read scope** to view (gates nav item + page +
  widget) and **project-level manage scope** to revoke. Wire via
  `scopeFor(routes.userSessions)` like sibling Connect items.
- Revoke emits an audit-log event (the revoke handler comment already states it
  emits one — verify during implementation) so revocations appear in the
  existing activity feed.

### 6. Testing

- **Backend:** extend `listusersessions_test.go` for the new fields,
  subject resolution (all three subject types), and `include_revoked`.
  `revokeusersession_test.go` already covers revoke.
- **Frontend:** widget renders active-only + empty state; page status filter
  and the revoke confirm flow.

## Decisions (resolved during brainstorming)

1. Data source = `userSessions` (sessions held _into_ toolsets), not
   `user_oauth_tokens`.
2. Both surfaces: read-only home widget (auto-filtered to active) **and** a
   filterable Connect → User Sessions page showing all.
3. Status model = all-with-status (Active / Expired / Revoked).
4. Revoke on the dedicated page only; widget is read-only.
5. RBAC = reuse project-level read + manage scopes.
6. Subject resolution = full (resolve `user`/`apikey` to display names).
7. Widget shows 5 rows.

## Risks / things to verify in planning

- Exact project read/manage scope identifiers in the `gram-rbac` setup.
- Whether `revokeUserSession` already emits an audit event.
- `subject_urn` parsing in SQL (prefix split + UUID cast) across the three
  subject shapes; confirm `users` / `api_keys` table + column names.
- Cursor pagination interaction with the `include_revoked` / `status` filters.
