-- =============================================================================
-- organizations
-- =============================================================================

-- name: CreateOrganization :one
INSERT INTO organizations (name, slug, account_type, workos_id)
VALUES (
  @name,
  @slug,
  COALESCE(sqlc.narg('account_type')::text, 'free'),
  sqlc.narg('workos_id')
)
RETURNING *;

-- name: UpdateOrganization :one
UPDATE organizations
SET
  name = COALESCE(sqlc.narg('name'), name),
  slug = COALESCE(sqlc.narg('slug'), slug),
  account_type = COALESCE(sqlc.narg('account_type'), account_type),
  workos_id = CASE
    WHEN @clear_workos_id::boolean THEN NULL
    ELSE COALESCE(sqlc.narg('workos_id'), workos_id)
  END,
  updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- name: GetOrganization :one
SELECT * FROM organizations WHERE id = @id;

-- ListOrganizations uses keyset pagination on the (random) uuid id. Stable
-- across concurrent inserts but not insertion-ordered. Caller passes
-- uuid.Nil for the first page and the last returned id for subsequent ones.
-- Caller is also responsible for fetching limit+1 to detect `next_cursor`.
-- name: ListOrganizations :many
SELECT * FROM organizations
WHERE id > @after
ORDER BY id ASC
LIMIT @max_rows;

-- name: DeleteOrganization :exec
DELETE FROM organizations WHERE id = @id;

-- =============================================================================
-- users
-- =============================================================================

-- name: CreateUser :one
INSERT INTO users (email, display_name, photo_url, github_handle, admin, whitelisted)
VALUES (
  @email,
  @display_name,
  sqlc.narg('photo_url'),
  sqlc.narg('github_handle'),
  COALESCE(sqlc.narg('admin')::boolean, FALSE),
  COALESCE(sqlc.narg('whitelisted')::boolean, TRUE)
)
RETURNING *;

-- UpdateUser is a partial patch: every COALESCE leaves the column unchanged
-- when the corresponding sqlc.narg parameter is NULL. There is no
-- clear-via-empty semantics on the optional text columns; if a test needs to
-- null out photo_url or github_handle, recreate the user.
-- name: UpdateUser :one
UPDATE users
SET
  email = COALESCE(sqlc.narg('email'), email),
  display_name = COALESCE(sqlc.narg('display_name'), display_name),
  photo_url = COALESCE(sqlc.narg('photo_url'), photo_url),
  github_handle = COALESCE(sqlc.narg('github_handle'), github_handle),
  admin = COALESCE(sqlc.narg('admin')::boolean, admin),
  whitelisted = COALESCE(sqlc.narg('whitelisted')::boolean, whitelisted),
  updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = @id;

-- name: ListUsers :many
SELECT * FROM users
WHERE id > @after
ORDER BY id ASC
LIMIT @max_rows;

-- DeleteCurrentUsersBySubjectRef sweeps any current_users row whose
-- subject_ref matches the given text (local-mode pointers store
-- users.id.String()). No FK exists because workos-mode subject_refs are
-- external WorkOS subs, not UUIDs — see idp-design.md §5.
-- name: DeleteCurrentUsersBySubjectRef :exec
DELETE FROM current_users WHERE subject_ref = @subject_ref;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = @id;

-- =============================================================================
-- memberships
-- =============================================================================

-- CreateMembership is idempotent on (user_id, organization_id) per
-- idp-design.md §6.1. ON CONFLICT DO UPDATE with a no-op SET lets RETURNING
-- fire on the existing row so callers always get the canonical record back.
-- The role from the original insert wins; callers wanting a different role
-- on an existing membership should call UpdateMembership.
-- name: CreateMembership :one
INSERT INTO memberships (user_id, organization_id, role)
VALUES (@user_id, @organization_id, COALESCE(sqlc.narg('role')::text, 'member'))
ON CONFLICT (user_id, organization_id) DO UPDATE SET
  user_id = EXCLUDED.user_id
RETURNING *;

-- name: UpdateMembership :one
UPDATE memberships
SET
  role = @role,
  updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- name: GetMembership :one
SELECT * FROM memberships WHERE id = @id;

-- ListMemberships keyset-paginates by id with optional (user_id,
-- organization_id) exact-match filters. Either or both narg parameters
-- may be NULL, in which case the corresponding filter is not applied.
-- name: ListMemberships :many
SELECT * FROM memberships
WHERE id > @after
  AND (sqlc.narg('user_id')::uuid IS NULL OR user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('organization_id')::uuid IS NULL OR organization_id = sqlc.narg('organization_id')::uuid)
ORDER BY id ASC
LIMIT @max_rows;

-- name: DeleteMembership :exec
DELETE FROM memberships WHERE id = @id;

-- =============================================================================
-- current_users (per-mode pointers; idp-design.md §3, §6.2)
-- =============================================================================

-- name: GetCurrentUserPointer :one
SELECT * FROM current_users WHERE mode = @mode;

-- name: UpsertCurrentUserPointer :one
INSERT INTO current_users (mode, subject_ref)
VALUES (@mode, @subject_ref)
ON CONFLICT (mode) DO UPDATE SET
  subject_ref = EXCLUDED.subject_ref,
  updated_at = clock_timestamp()
RETURNING *;
