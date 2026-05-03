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
