-- name: UpsertUser :one
INSERT INTO users (id, email, display_name, photo_url, admin)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET
  email = EXCLUDED.email,
  display_name = EXCLUDED.display_name,
  photo_url = EXCLUDED.photo_url,
  admin = EXCLUDED.admin,
  last_login = clock_timestamp(),
  updated_at = clock_timestamp()
RETURNING *, (xmax = 0) AS was_created;

-- name: GetUser :one
SELECT * FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1;

-- name: GetUserIDByWorkosID :one
SELECT id FROM users
WHERE workos_id = $1
LIMIT 1;

-- name: GetUsersByWorkosIDs :many
SELECT * FROM users
WHERE workos_id = ANY(@workos_ids::text[]);

-- name: GetConnectedUsersByWorkosIDs :many
SELECT u.* FROM users u
JOIN organization_user_relationships our ON our.user_id = u.id
WHERE u.workos_id = ANY(@workos_ids::text[])
  AND our.organization_id = @organization_id
  AND our.deleted_at IS NULL;

-- name: SetUserWorkosID :exec
UPDATE users 
SET workos_id = @workos_id, 
  updated_at = clock_timestamp()
WHERE id = @id AND 
  workos_id IS NULL;
