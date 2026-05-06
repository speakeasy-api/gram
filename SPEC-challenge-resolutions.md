# Spec: Authz Challenge Resolutions — App Code

## Context

- **PR #2591** landed authz challenge logging to ClickHouse (`authz_challenges` table)
- **PR #2597** landed the `authz_challenge_resolutions` PostgreSQL table
- **PR #2542** (feat/challenge-ui) has the FE with mock data — needs real endpoints
- This branch: backend endpoints + FE react-query hooks to replace mocks

## Decisions

| Decision                   | Answer                                                                                                  |
| -------------------------- | ------------------------------------------------------------------------------------------------------- |
| Join strategy              | Server-side — single endpoint queries CH then enriches with PG resolution data                          |
| resolveChallenge atomicity | Resolution-only — FE assigns role first via existing endpoint, then calls resolveChallenge to record it |
| Avatar enrichment          | Backend joins against PG `users` table (has `photo_url`, `email`, keyed by `id`; CH stores `user_id`)   |
| Resolution types           | `role_assigned` and `dismissed` from day one                                                            |
| Delete/unresolve           | Not supported                                                                                           |
| Deduplication              | Return all events (no dedup for now)                                                                    |
| Service home               | Extend existing `access` Goa service                                                                    |
| RBAC                       | `listChallenges` → `org:read`, `resolveChallenge` → `org:admin`                                         |
| FE approach                | Build react-query hooks on this branch; challenge-ui branch rebases later                               |

## Endpoints

### 1. `GET /rpc/access.listChallenges`

**Scope:** `org:read`

**Query params:**

| Param             | Type                | Required | Notes                                             |
| ----------------- | ------------------- | -------- | ------------------------------------------------- |
| `organization_id` | string              | yes      | Tenant isolation                                  |
| `project_id`      | string              | no       | Filter to specific project                        |
| `outcome`         | `"allow" \| "deny"` | no       | Filter by outcome                                 |
| `principal_urn`   | string              | no       | Filter by principal                               |
| `scope`           | string              | no       | Filter by scope                                   |
| `resolved`        | bool                | no       | `true` = only resolved, `false` = only unresolved |
| `limit`           | int                 | no       | Default 50, max 200                               |
| `offset`          | int                 | no       | Default 0                                         |

**Implementation flow:**

1. Query ClickHouse `authz_challenges` with filters, ordered by `timestamp DESC`, with `limit`/`offset`
2. Collect unique `challenge_id`s from results
3. Batch-query PG `authz_challenge_resolutions` by `(organization_id, challenge_id IN (...))` to get resolution state
4. If `resolved` filter is set, apply it post-join (filter out rows based on resolution existence)
5. Collect unique non-nil `user_id`s from CH results
6. Batch-query PG `users` table by `id IN (...)` to get `email`, `photo_url`, `display_name`
7. Merge and return

**Response:**

```json
{
  "challenges": [
    {
      "id": "string",
      "timestamp": "2026-05-01T12:00:00Z",
      "organization_id": "org-1",
      "project_id": "proj-1",
      "principal_urn": "user:usr_abc123",
      "principal_type": "user",
      "user_email": "alice@acme.com",
      "photo_url": "https://...",
      "operation": "require",
      "outcome": "deny",
      "reason": "scope_unsatisfied",
      "scope": "project:write",
      "resource_kind": "project",
      "resource_id": "proj-1",
      "role_slugs": ["member"],
      "evaluated_grant_count": 3,
      "matched_grant_count": 0,
      "resolved_at": "2026-05-02T10:00:00Z",
      "resolution_type": "role_assigned",
      "resolved_by": "user:usr_admin1",
      "resolution_role_slug": "editor"
    }
  ],
  "total": 142
}
```

**Notes:**

- `resolved_at` / `resolution_type` / `resolved_by` / `resolution_role_slug` are null when unresolved
- `matched_grant_count` = length of `matched_grants` array from CH (not stored as a column — compute on read)
- `total` count: run a separate CH count query with same filters (without limit/offset) for pagination
- `resolved` filter: if `true`, inner-join resolutions; if `false`, left-join and filter `WHERE resolution IS NULL`; if omitted, left-join with no filter

### 2. `POST /rpc/access.resolveChallenge`

**Scope:** `org:admin`

**Request:**

```json
{
  "organization_id": "org-1",
  "challenge_id": "ulid-from-clickhouse",
  "principal_urn": "user:usr_abc123",
  "scope": "project:write",
  "resource_kind": "project",
  "resource_id": "proj-1",
  "resolution_type": "role_assigned",
  "role_slug": "editor"
}
```

**Validation:**

- `resolution_type` must be `"role_assigned"` or `"dismissed"`
- If `role_assigned`, `role_slug` is required
- If `dismissed`, `role_slug` must be nil/empty
- `challenge_id`, `principal_urn`, `scope` are required

**Implementation:**

1. Insert into `authz_challenge_resolutions` with `resolved_by` = current user's principal URN from auth context
2. On conflict `(organization_id, challenge_id)` → return error (already resolved)

**Response:** The created resolution object.

```json
{
  "id": "uuid",
  "organization_id": "org-1",
  "challenge_id": "ulid",
  "principal_urn": "user:usr_abc123",
  "resolution_type": "role_assigned",
  "role_slug": "editor",
  "resolved_by": "user:usr_admin1",
  "created_at": "2026-05-02T10:00:00Z"
}
```

## Files to Create/Modify

### Backend (server/)

| File                                    | Action     | What                                                                |
| --------------------------------------- | ---------- | ------------------------------------------------------------------- |
| `server/design/access.go`               | Modify     | Add `listChallenges` + `resolveChallenge` methods to access service |
| `server/gen/`                           | Regenerate | `mise run gen:goa-server`                                           |
| `server/internal/access/impl.go`        | Modify     | Implement the two new methods                                       |
| `server/internal/access/queries.sql`    | Modify     | Add SQLc queries for resolution CRUD + user batch lookup            |
| `server/internal/access/repo/`          | Regenerate | `mise run gen:sqlc-server`                                          |
| `server/internal/authz/repo/queries.go` | Modify     | Add `ListChallenges` CH query method                                |

### SQLc Queries (PG)

```sql
-- name: ListChallengeResolutions :many
SELECT * FROM authz_challenge_resolutions
WHERE organization_id = $1 AND challenge_id = ANY($2::text[]);

-- name: InsertChallengeResolution :one
INSERT INTO authz_challenge_resolutions (
  organization_id, challenge_id, principal_urn, scope,
  resource_kind, resource_id, resolution_type, role_slug, resolved_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: BatchGetUsersByID :many
SELECT id, email, display_name, photo_url FROM users
WHERE id = ANY($1::text[]);
```

### ClickHouse Query

```sql
SELECT
  id, timestamp, organization_id, project_id,
  principal_urn, principal_type, user_id, user_email,
  operation, outcome, reason,
  scope, resource_kind, resource_id,
  role_slugs, evaluated_grant_count,
  length(matched_grants.scope) AS matched_grant_count
FROM authz_challenges
WHERE organization_id = ?
  AND (? = '' OR project_id = ?)
  AND (? = '' OR outcome = ?)
  AND (? = '' OR principal_urn = ?)
  AND (? = '' OR scope = ?)
ORDER BY timestamp DESC
LIMIT ? OFFSET ?
```

Plus a `SELECT count(*)` variant for total.

### Frontend (client/dashboard/)

| File                                 | Action     | What                                                                                           |
| ------------------------------------ | ---------- | ---------------------------------------------------------------------------------------------- |
| SDK types                            | Regenerate | `mise run gen:sdk` after Goa changes                                                           |
| `src/hooks/useChallenges.ts` (new)   | Create     | react-query hooks: `useChallenges(filters)` + `useResolveChallenge()` mutation                 |
| `src/pages/access/ChallengesTab.tsx` | Modify     | Replace `MOCK_CHALLENGES` with `useChallenges()` hook                                          |
| `src/pages/access/GrantDrawer.tsx`   | Modify     | Wire "Assign" confirm step to call existing role-assign endpoint, then `useResolveChallenge()` |

## RBAC Summary

| Endpoint           | Scope       | Rationale                                     |
| ------------------ | ----------- | --------------------------------------------- |
| `listChallenges`   | `org:read`  | Viewing authz telemetry is read-only org data |
| `resolveChallenge` | `org:admin` | Resolving challenges = administrative action  |

## Out of Scope

- Delete/unresolve resolution
- Deduplication of challenges (future enhancement)
- Audit logging of resolutions (follow-up)
- Filtering by time range (can add later)
