-- name: UpsertUser :one
INSERT INTO users (id, email, display_name, photo_url, admin)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET
  email = EXCLUDED.email,
  display_name = EXCLUDED.display_name,
  photo_url = EXCLUDED.photo_url,
  admin = EXCLUDED.admin,
  updated_at = clock_timestamp()
RETURNING *;

-- name: GetUser :one
SELECT * FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1;