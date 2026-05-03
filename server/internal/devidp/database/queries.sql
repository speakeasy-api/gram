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
