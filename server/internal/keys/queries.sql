-- name: CreateAPIKey :one
INSERT INTO api_keys (
    organization_id
  , project_id
  , created_by_user_id
  , name
  , key_prefix
  , key_hash
  , scopes
) VALUES (
    @organization_id
  , @project_id
  , @created_by_user_id
  , @name
  , @key_prefix
  , @key_hash
  , @scopes::text[]
)
RETURNING *;

-- name: GetAPIKeyByKeyHash :one
SELECT api_keys.*, users.email
FROM api_keys
JOIN users ON users.id = api_keys.created_by_user_id
WHERE key_hash = @key_hash
  AND deleted IS FALSE;

-- name: ListAPIKeysByOrganization :many
SELECT *
FROM api_keys
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: DeleteAPIKey :one
UPDATE api_keys
SET deleted_at = NOW()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING id, organization_id, project_id, name, scopes;

-- name: DeleteAPIKeysByNameAndUser :exec
-- Soft-delete every live key with the given name owned by the given user in the
-- org. Used by the device-agent token exchange to revoke a user's prior
-- device-agent key(s) before minting a fresh one (DNO-383 rotation).
UPDATE api_keys
SET deleted_at = NOW()
WHERE organization_id = @organization_id
  AND created_by_user_id = @created_by_user_id
  AND name = @name
  AND deleted IS FALSE;

-- name: UpdateAPIKeyLastAccessedAt :exec
UPDATE api_keys
SET last_accessed_at = clock_timestamp()
WHERE id = @id
  AND deleted IS FALSE
  -- This check reduces writes to the database to at most once per minute per
  -- key as a way to mitigate excessive write spikes.
  AND (last_accessed_at IS NULL OR last_accessed_at < clock_timestamp() - interval '1 minute');
