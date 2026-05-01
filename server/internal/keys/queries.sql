-- name: CreateAPIKey :one
INSERT INTO api_keys (
    organization_id
  , project_id
  , created_by_user_id
  , name
  , key_prefix
  , key_hash
  , scopes
  , toolset_id
  , plugin_id
  , system_managed
) VALUES (
    @organization_id
  , @project_id
  , @created_by_user_id
  , @name
  , @key_prefix
  , @key_hash
  , @scopes::text[]
  , @toolset_id
  , @plugin_id
  , @system_managed
)
RETURNING *;

-- name: SoftDeletePluginScopedKeys :many
-- Soft-deletes all active system-managed keys back-referenced to a plugin
-- and returns the revoked rows so the caller can emit one audit log entry
-- per key inside the same transaction. Used on republish to revoke the
-- prior generation's per-server keys before minting fresh ones. Scoped to
-- organization_id as a defensive guard since plugin_id alone is globally
-- unique (UUIDv7).
UPDATE api_keys
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE plugin_id = @plugin_id
  AND organization_id = @organization_id
  AND system_managed IS TRUE
  AND deleted IS FALSE
RETURNING id, name, scopes, plugin_id, toolset_id;

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
