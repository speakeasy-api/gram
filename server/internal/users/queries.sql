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

-- name: GetUsersByIDs :many
SELECT * FROM users
WHERE id = ANY(@ids::text[]);

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1;

-- name: SetUserWorkosID :exec
UPDATE users 
SET workos_id = @workos_id, 
  updated_at = clock_timestamp()
WHERE id = @id AND 
  workos_id IS NULL;
