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
-- Deliberately does NOT join users the way GetAPIKeyByKeyHash does. A key
-- whose created_by_user_id is not a users.id is unusable (auth's join drops
-- it), but hiding it here would leave the owner with an invisible row they
-- cannot inspect or delete. RepairOrphanedAPIKeyCreators repoints those rows
-- instead; the durable guarantee belongs on a foreign key, not on this SELECT.
SELECT api_keys.*
FROM api_keys
WHERE api_keys.organization_id = @organization_id
  AND api_keys.deleted IS FALSE
  AND NOT EXISTS (
    SELECT 1
    FROM litellm_instances li
    WHERE li.organization_id = api_keys.organization_id
      AND li.project_id = api_keys.project_id
      AND li.api_key_id = api_keys.id
      AND li.deleted IS FALSE
  )
ORDER BY api_keys.created_at DESC;

-- name: RepairOrphanedAPIKeyCreators :execrows
-- Points API keys whose created_by_user_id is not a users.id (including the
-- 'system' placeholder minted by the plugin rollout sweep) at the oldest
-- connected member of their organization. GetAPIKeyByKeyHash JOINs users on
-- that column, so these keys authenticate as 401 until this rewrite. This is
-- a deliberate cross-tenant repair — unlike tenant-scoped queries it is not
-- constrained to a project_id.
UPDATE api_keys k
SET
  created_by_user_id = member.user_id,
  updated_at = clock_timestamp()
FROM (
  SELECT DISTINCT ON (our.organization_id)
    our.organization_id,
    our.user_id
  FROM organization_user_relationships our
  JOIN users u ON u.id = our.user_id
  WHERE our.deleted IS FALSE
    AND our.user_id IS NOT NULL
    AND u.deleted_at IS NULL
  ORDER BY our.organization_id, our.created_at ASC, our.user_id ASC
) member
WHERE k.organization_id = member.organization_id
  AND k.deleted IS FALSE
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.id = k.created_by_user_id);

-- name: IsAPIKeyManagedByActiveLiteLLMInstance :one
SELECT EXISTS (
  SELECT 1
  FROM litellm_instances li
  JOIN api_keys ak
    ON ak.organization_id = li.organization_id
   AND ak.project_id = li.project_id
   AND ak.id = li.api_key_id
  WHERE ak.id = @id
    AND ak.organization_id = @organization_id
    AND ak.deleted IS FALSE
    AND li.deleted IS FALSE
);

-- name: DeleteAPIKey :one
UPDATE api_keys
SET deleted_at = NOW()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING id, organization_id, project_id, name, scopes;

-- name: DeleteAPIKeyByProject :one
UPDATE api_keys
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING id, organization_id, project_id, name, key_prefix, scopes;

-- name: UpdateAPIKeyLastAccessedAt :exec
UPDATE api_keys
SET last_accessed_at = clock_timestamp()
WHERE id = @id
  AND deleted IS FALSE
  -- This check reduces writes to the database to at most once per minute per
  -- key as a way to mitigate excessive write spikes.
  AND (last_accessed_at IS NULL OR last_accessed_at < clock_timestamp() - interval '1 minute');
