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

-- name: CreateAgentAPIKey :one
INSERT INTO api_keys (
    organization_id
  , project_id
  , created_by_user_id
  , name
  , key_prefix
  , key_hash
  , scopes
  , subject_urn
  , delegated_grants
  , delegated_grants_version
  , expires_at
) VALUES (
    @organization_id
  , NULL
  , @created_by_user_id
  , @name
  , @key_prefix
  , @key_hash
  , ARRAY[]::text[]
  , @subject_urn
  , @delegated_grants::jsonb
  , @delegated_grants_version
  , @expires_at
)
RETURNING *;

-- name: GetAPIKeyByKeyHash :one
-- expires_at is enforced here rather than in Go so every credential shape --
-- agent principal keys and scoped keys given a grace expiry by a rotation --
-- stops authenticating at the same moment.
SELECT api_keys.*, users.email,
       EXISTS (SELECT 1 FROM assistant_inference_credentials c
               WHERE c.api_key_id = api_keys.id AND c.project_id = api_keys.project_id) AS superseded_kit_credential
FROM api_keys
JOIN users ON users.id = api_keys.created_by_user_id
WHERE key_hash = @key_hash
  AND deleted IS FALSE
  AND (expires_at IS NULL OR expires_at > clock_timestamp());

-- name: GetActivePrincipalAPIKeyForAdmission :one
SELECT id
FROM api_keys
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
  AND cardinality(scopes) = 0
  AND subject_urn = @subject_urn
  AND created_by_user_id = @authorizer_user_id
  AND delegated_grants = @delegated_grants::jsonb
  AND delegated_grants_version = @delegated_grants_version
  AND expires_at > statement_timestamp()
  AND expires_at > created_at
  AND expires_at <= created_at + INTERVAL '365 days';

-- name: GetActiveLegacyAPIKeyForAdmission :one
-- A legacy key is admitted as its own principal only while its live row is in
-- the tenant, unrevoked and unexpired; agent keys keep their own admission.
SELECT id, project_id, scopes
FROM api_keys
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
  AND subject_urn IS NULL
  AND (expires_at IS NULL OR expires_at > clock_timestamp());

-- name: CurrentDatabaseTime :one
-- The database clock, used as the cutoff a credential rotation retires keys
-- against so the comparison shares a clock with api_keys.created_at.
SELECT clock_timestamp()::timestamptz;

-- Plugin distribution mints hooks keys as plugins-hooks-<timestamp>-<token>
-- (publish) or plugins-hooks-download-<timestamp>-<token> (ZIP download). The
-- two retirement statements below select that set the same way:
--
--   * the name matches the generated shape auth.IsOrgWidePluginHooksAPIKey
--     parses, and its six-character token marker agrees with the stored
--     key_prefix, so provenance is proven from the minting material rather than
--     from a name a user could once have typed;
--   * the key was created before the rotation began, which retires every
--     pre-rotation credential while leaving any replacement minted by an
--     overlapping rotation alone -- two concurrent rotations can race for
--     "newest key", but neither can revoke the other's replacement and neither
--     can leave a pre-rotation credential valid;
--   * the replacement of THIS rotation is excluded by hash as well, so the key
--     just handed to the caller cannot be swept up by its own statement.

-- name: RevokePluginHooksAPIKeysByProject :many
UPDATE api_keys
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND key_hash <> @replacement_key_hash
  AND created_at < @retire_keys_created_before
  AND scopes @> ARRAY['hooks']::text[]
  AND name ~ '^plugins-hooks-(download-)?[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$'
  AND right(key_prefix, 5) = left(substring(name from '[0-9a-f]{6}$'), 5)
RETURNING id, organization_id, project_id, name, key_prefix, scopes, expires_at;

-- name: ExpirePluginHooksAPIKeysByProject :many
-- LEAST keeps the earliest deadline: an already-expired key is excluded
-- outright, and a key already inside a grace window keeps that window rather
-- than having it extended, so repeated rotations cannot keep one credential
-- alive indefinitely.
UPDATE api_keys
SET expires_at = LEAST(COALESCE(expires_at, @expires_at), @expires_at)
  , updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND key_hash <> @replacement_key_hash
  AND created_at < @retire_keys_created_before
  AND (expires_at IS NULL OR expires_at > clock_timestamp())
  AND scopes @> ARRAY['hooks']::text[]
  AND name ~ '^plugins-hooks-(download-)?[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$'
  AND right(key_prefix, 5) = left(substring(name from '[0-9a-f]{6}$'), 5)
-- The returned expires_at is the deadline LEAST settled on, which for a key
-- already inside a shorter window is earlier than the one this rotation asked
-- for. Callers must report these rather than their own requested deadline.
RETURNING id, organization_id, project_id, name, key_prefix, scopes, expires_at;

-- name: LockMemberClaudeWebHooksAPIKeys :exec
-- First statement of the mint-and-revoke transaction: serializes a member's
-- Claude web key downloads so each revoke sees every earlier committed key,
-- and racing downloads cannot both stay live.
SELECT pg_advisory_xact_lock(hashtextextended('claude-web-hooks:' || @organization_id::text || ':' || @created_by_user_id::text, 0));

-- name: RevokeMemberClaudeWebHooksAPIKeys :many
-- Retires a member's earlier Claude web observability keys once a newer one is
-- minted, so repeated downloads leave one live key per member. Callers hold
-- LockMemberClaudeWebHooksAPIKeys, so downloads for one member run one at a
-- time. The key_prefix check proves a key was minted by this flow, as the
-- project rotation queries do, so a user-chosen name is never revoked.
UPDATE api_keys
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND created_by_user_id = @created_by_user_id
  AND deleted IS FALSE
  AND key_hash <> @replacement_key_hash
  AND created_at < @replacement_created_at
  AND scopes @> ARRAY['hooks']::text[]
  AND name ~ '^plugins-claude-web-hooks-[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$'
  AND right(key_prefix, 5) = left(substring(name from '[0-9a-f]{6}$'), 5)
RETURNING id, project_id, name;

-- name: RevokeStalePluginAPIKeys :many
-- Revokes plugin-minted keys nobody needs any more: plugins-hooks-* and
-- plugins-mcp-* (publish) and plugins-hooks-download-* (ZIP download). Claude
-- web keys are left alone because each member already keeps only one. Provenance
-- is proven the same way as the rotation queries above. A key is kept while any
-- of these holds:
--
--   * it was created within the retention window;
--   * it was created within the retention window before the project's latest
--     publish, so the current plugin generation keeps working however long ago
--     it shipped. Revoked publish keys still count as publishes;
--   * it was used within the retention window.
WITH plugin_keys AS (
  SELECT id, project_id, name, created_at, last_accessed_at, deleted
  FROM api_keys
  WHERE name ~ '^plugins-(mcp|hooks|hooks-download)-[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$'
    AND right(key_prefix, 5) = left(substring(name from '[0-9a-f]{6}$'), 5)
),
last_publish AS (
  SELECT project_id, max(created_at) AS published_at
  FROM plugin_keys
  WHERE project_id IS NOT NULL
    AND name ~ '^plugins-(mcp|hooks)-[0-9]{8}-'
  GROUP BY project_id
),
stale AS (
  SELECT k.id
  FROM plugin_keys k
  LEFT JOIN last_publish lp ON lp.project_id = k.project_id
  WHERE k.deleted IS FALSE
    AND k.created_at < clock_timestamp() - (sqlc.arg(retention_days)::int * interval '1 day')
    AND (lp.published_at IS NULL OR k.created_at < lp.published_at - (sqlc.arg(retention_days)::int * interval '1 day'))
    AND (k.last_accessed_at IS NULL OR k.last_accessed_at < clock_timestamp() - (sqlc.arg(retention_days)::int * interval '1 day'))
  ORDER BY k.created_at
  LIMIT sqlc.arg(batch_size)
)
UPDATE api_keys
SET deleted_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM stale)
  AND deleted IS FALSE
RETURNING id, organization_id, project_id, name, scopes;

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
  AND api_keys.subject_urn IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM litellm_instances li
    WHERE li.organization_id = api_keys.organization_id
      AND li.project_id = api_keys.project_id
      AND li.api_key_id = api_keys.id
      AND li.deleted IS FALSE
  )
ORDER BY api_keys.created_at DESC;

-- name: ListAPIKeysByOrganizationPage :many
-- The keys.list page read: the same rows as ListAPIKeysByOrganization (and,
-- like it, deliberately not joined to users), walked newest first by keyset.
-- The page starts after (cursor_created_at, cursor_id). The first page passes
-- 'infinity' and the largest UUID, so the comparison is always present and
-- api_keys_organization_id_created_at_id_idx serves every page. A NULL
-- page_limit means LIMIT ALL; a NULL name_pattern matches every name.
SELECT api_keys.*
FROM api_keys
WHERE api_keys.organization_id = @organization_id
  AND api_keys.deleted IS FALSE
  AND api_keys.subject_urn IS NULL
  AND (api_keys.created_at, api_keys.id) < (@cursor_created_at::timestamptz, @cursor_id::uuid)
  AND (sqlc.narg(name_pattern)::text IS NULL OR api_keys.name ILIKE sqlc.narg(name_pattern)::text ESCAPE '\')
  AND NOT EXISTS (
    SELECT 1
    FROM litellm_instances li
    WHERE li.organization_id = api_keys.organization_id
      AND li.project_id = api_keys.project_id
      AND li.api_key_id = api_keys.id
      AND li.deleted IS FALSE
  )
ORDER BY api_keys.created_at DESC, api_keys.id DESC
LIMIT sqlc.narg(page_limit)::int;

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

-- name: ListAgentAPIKeys :many
SELECT *
FROM api_keys
WHERE organization_id = @organization_id
  AND subject_urn = @subject_urn
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: GetAPIKeyByID :one
SELECT *
FROM api_keys
WHERE id = @id
  AND organization_id = @organization_id;

-- name: GetAPIKeyByIDForUpdate :one
SELECT *
FROM api_keys
WHERE id = @id
  AND organization_id = @organization_id
FOR UPDATE;

-- name: TouchAPIKeyUpdatedAt :one
UPDATE api_keys
SET updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
RETURNING updated_at;

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
  AND subject_urn IS NULL
  AND deleted IS FALSE
RETURNING id, organization_id, project_id, name, scopes;

-- name: DeleteAgentAPIKey :one
UPDATE api_keys
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND subject_urn = @subject_urn
  AND deleted IS FALSE
RETURNING *;

-- name: ExtendAgentAPIKeyDelegatedGrants :one
-- Matches the policy the caller read, so a concurrent change to the same key
-- is never overwritten with a policy built from the older one.
UPDATE api_keys
SET delegated_grants = @delegated_grants::jsonb,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND subject_urn = @subject_urn
  AND created_by_user_id = @created_by_user_id
  AND delegated_grants = @previous_delegated_grants::jsonb
  AND deleted IS FALSE
  AND expires_at > clock_timestamp()
RETURNING *;

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

-- name: CountLiveAuthClientsInOrganization :one
SELECT COUNT(*)
FROM mcp_auth_clients
WHERE organization_id = @organization_id
  AND id = ANY(@ids::uuid[])
  AND deleted IS FALSE;
