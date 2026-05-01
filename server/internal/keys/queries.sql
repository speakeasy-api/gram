-- name: CreateAPIKey :one
INSERT INTO api_keys (
    organization_id
  , project_id
  , created_by_user_id
  , name
  , key_prefix
  , key_hash
  , scopes
  , system_managed
) VALUES (
    @organization_id
  , @project_id
  , @created_by_user_id
  , @name
  , @key_prefix
  , @key_hash
  , @scopes::text[]
  , @system_managed
)
RETURNING *;

-- name: GetAPIKeyByKeyHash :one
SELECT api_keys.*, users.email
FROM api_keys
JOIN users ON users.id = api_keys.created_by_user_id
WHERE key_hash = @key_hash
  AND deleted IS FALSE;

-- name: ListAPIKeysByOrganization :many
-- Returns user-managed keys only. System-managed keys (e.g. those minted
-- by plugin publish — rfc-plugin-scoped-keys.md) are filtered out so they
-- don't clutter the dashboard's keys page or count against user quotas.
-- Use a different query if you need to see system-managed keys.
SELECT *
FROM api_keys
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND system_managed IS FALSE
ORDER BY created_at DESC;

-- name: DeleteAPIKey :one
UPDATE api_keys
SET deleted_at = NOW()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING id, organization_id, project_id, name, scopes;

-- name: UpdateAPIKeyLastAccessedAt :exec
UPDATE api_keys
SET last_accessed_at = clock_timestamp()
WHERE id = @id
  AND deleted IS FALSE
  -- This check reduces writes to the database to at most once per minute per
  -- key as a way to mitigate excessive write spikes.
  AND (last_accessed_at IS NULL OR last_accessed_at < clock_timestamp() - interval '1 minute');
