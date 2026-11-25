-- Every write of a function runner key copy. See the runnerkeys package doc.

-- name: GetProjectOrganization :one
-- The organization that owns the project, which new runner keys are sealed
-- for. functions_access.organization_id references organization_metadata, so
-- an organization without a metadata row is not returned.
SELECT p.organization_id
FROM projects AS p
JOIN organization_metadata AS om ON om.id = p.organization_id
WHERE p.id = @project_id;

-- name: CreateRunnerKey :one
-- Stores a new runner key for one function of a deployment, sealed under the
-- decided transition generation. The row's organization is the project's own,
-- so a caller can never file a key under another organization. No rows means
-- the project does not belong to that organization, or the transition moved
-- since the decision; the caller tells the two apart by reading the transition
-- again.
INSERT INTO functions_access (
    project_id
  , organization_id
  , deployment_id
  , function_id
  , encryption_key
  , encryption_key_org_encrypted
  , bearer_format
)
SELECT
    p.id
  , p.organization_id
  , @deployment_id
  , @function_id
  , sqlc.narg('platform_copy')::bytea
  , sqlc.narg('org_copy')::text
  , @bearer_format
FROM projects AS p
WHERE p.id = @project_id
  AND p.organization_id = @organization_id::text
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = p.organization_id AND t.use_case = 'function-runner-keys'
    FOR KEY SHARE
  ), 0) = @decided_generation
RETURNING id;

-- name: ListRunnerKeysWaiting :many
-- Runner keys of the organization with a platform-key copy and no
-- organization-key copy, soft-deleted ones included, paged by id.
SELECT fa.id, fa.encryption_key
FROM functions_access AS fa
WHERE fa.organization_id = @organization_id::text
  AND fa.encryption_key IS NOT NULL
  AND fa.encryption_key_org_encrypted IS NULL
  AND fa.id > @after::uuid
ORDER BY fa.id
LIMIT @limit_value;

-- name: SealRunnerKeyWaiting :execrows
-- Adds the organization-key copy of a runner key the organization encryption
-- backfill sealed. It lands only while the row still holds the platform-key
-- copy that was read and no organization-key copy. Nothing else about the
-- row changes.
UPDATE functions_access AS fa
SET encryption_key_org_encrypted = @org_copy
WHERE fa.id = @id
  AND fa.organization_id = @organization_id::text
  AND fa.encryption_key = sqlc.arg('platform_copy')::bytea
  AND fa.encryption_key_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = fa.organization_id AND t.use_case = 'function-runner-keys'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListRunnerKeysSealed :many
-- Runner keys of the organization that hold both copies, soft-deleted ones
-- included, paged by id, for verification.
SELECT fa.id, fa.encryption_key, fa.encryption_key_org_encrypted::text AS org_copy
FROM functions_access AS fa
WHERE fa.organization_id = @organization_id::text
  AND fa.encryption_key IS NOT NULL
  AND fa.encryption_key_org_encrypted IS NOT NULL
  AND fa.id > @after::uuid
ORDER BY fa.id
LIMIT @limit_value;

-- name: CountRunnerKeyCopies :one
-- The organization's runner key counts for the organization encryption
-- backfill and finalize. A waiting key recorded as unreadable counts as
-- unreadable only while the recorded hash still matches the stored
-- platform-key copy. Unattributed counts keys of the organization's projects
-- stored without organization_id, which the organization-scoped queries never
-- reach.
-- value_count and last_written_at cover the organization's own values under
-- either key, soft-deleted rows included: the rows the backfill and clear
-- reach, as ListOrganizationsWith* reports them.
WITH platform_copies AS (
  SELECT fa.encryption_key_org_encrypted, u.id AS unreadable_id
  FROM functions_access AS fa
  LEFT JOIN organization_encryption_unreadable_values AS u
    ON u.organization_id = fa.organization_id
    AND u.use_case = 'function-runner-keys'
    AND u.source_column = 'functions_access.encryption_key'
    AND u.row_id = fa.id::text
    AND u.platform_ciphertext_sha256 = sha256(fa.encryption_key)
  WHERE fa.organization_id = @organization_id::text
    AND fa.encryption_key IS NOT NULL
)
SELECT
    COUNT(*) FILTER (
        WHERE k.encryption_key_org_encrypted IS NULL
          AND k.unreadable_id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE k.encryption_key_org_encrypted IS NULL
          AND k.unreadable_id IS NOT NULL
    )::bigint AS unreadable,
    (
      SELECT COUNT(*)
      FROM functions_access AS fa
      JOIN projects AS p ON p.id = fa.project_id
      WHERE fa.organization_id IS NULL
        AND p.organization_id = @organization_id::text
        AND (fa.encryption_key IS NOT NULL OR fa.encryption_key_org_encrypted IS NOT NULL)
    )::bigint AS unattributed,
    COUNT(*)::bigint AS platform_copies,
    (
      SELECT COUNT(*)
      FROM functions_access AS fa
      WHERE fa.organization_id = @organization_id::text
        AND (fa.encryption_key IS NOT NULL OR fa.encryption_key_org_encrypted IS NOT NULL)
    )::bigint AS value_count,
    (
      SELECT MAX(fa.updated_at)
      FROM functions_access AS fa
      WHERE fa.organization_id = @organization_id::text
        AND (fa.encryption_key IS NOT NULL OR fa.encryption_key_org_encrypted IS NOT NULL)
    )::timestamptz AS last_written_at
FROM platform_copies AS k;

-- name: ClearRunnerKeyPlatformCopies :one
-- Destroys up to limit platform-key copies of the organization's runner keys
-- after finalize. The organization-key copy stays.
-- Returns how many cleared copies stood alone and how many stood beside an
-- organization-key copy. The outer statement rechecks that the platform-key
-- copy is still set, so a row a concurrent clear already emptied is neither
-- updated nor counted.
WITH cleared AS (
    UPDATE functions_access AS fa
    SET encryption_key = NULL
    WHERE fa.id IN (
        SELECT target.id FROM functions_access AS target
        WHERE target.organization_id = @organization_id::text
          AND target.encryption_key IS NOT NULL
        ORDER BY target.id
        LIMIT @limit_value
      )
      AND fa.organization_id = @organization_id::text
      AND fa.encryption_key IS NOT NULL
      AND COALESCE((
        SELECT t.generation FROM organization_encryption_transitions AS t
        WHERE t.organization_id = fa.organization_id AND t.use_case = 'function-runner-keys'
        FOR KEY SHARE
      ), 0) = @decided_generation
    RETURNING (fa.encryption_key_org_encrypted IS NULL) AS platform_only
)
SELECT
    COUNT(*) FILTER (WHERE cleared.platform_only)::bigint AS platform_only,
    COUNT(*) FILTER (WHERE NOT cleared.platform_only)::bigint AS beside_org_copy
FROM cleared;

-- name: ListOrganizationsWithRunnerKeys :many
-- Every organization storing a function runner key under either key,
-- soft-deleted rows included, with how many and the latest updated_at of the
-- rows storing them. Rows without organization_id are left out.
SELECT
    fa.organization_id::text AS organization_id,
    COUNT(*)::bigint AS value_count,
    MAX(fa.updated_at)::timestamptz AS last_written_at
FROM functions_access AS fa
WHERE fa.organization_id IS NOT NULL
  AND (fa.encryption_key IS NOT NULL OR fa.encryption_key_org_encrypted IS NOT NULL)
GROUP BY fa.organization_id;
