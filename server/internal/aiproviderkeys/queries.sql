-- The backfill, verification and finalize side of moving AI provider and
-- model keys to organization keys (package orgencryption's CopyStore), one set
-- of queries per table. Every query covers soft-deleted rows too, and every
-- write changes only the copy column it names, never updated_at:
-- openrouter_api_keys.updated_at is a limit generation that billing
-- reconciliation compares. The writes that store a new key live in each
-- consumer's own queries.sql.
--
-- openrouter_api_keys has no id: its rows are keyed by (organization_id,
-- key_type), so key_type is the row id within an organization.

-- name: ListAIIntegrationConfigAPIKeyWaiting :many
SELECT c.id, c.api_key_encrypted::text AS platform_copy
FROM ai_integration_configs AS c
WHERE c.organization_id = @organization_id::text
  AND c.api_key_encrypted IS NOT NULL
  AND c.api_key_org_encrypted IS NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: SealAIIntegrationConfigAPIKeyWaiting :execrows
-- Lands only while the row still holds the platform-key copy that was read
-- and no organization-key copy, so a concurrent replacement wins.
UPDATE ai_integration_configs AS c
SET api_key_org_encrypted = @org_copy
WHERE c.id = @id
  AND c.organization_id = @organization_id::text
  AND c.api_key_encrypted = @platform_copy
  AND c.api_key_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'ai-provider-keys'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListAIIntegrationConfigAPIKeySealed :many
SELECT c.id, c.api_key_encrypted::text AS platform_copy, c.api_key_org_encrypted::text AS org_copy
FROM ai_integration_configs AS c
WHERE c.organization_id = @organization_id::text
  AND c.api_key_encrypted IS NOT NULL
  AND c.api_key_org_encrypted IS NOT NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: CountAIIntegrationConfigAPIKeyCopies :one
-- Every row has an organization, so nothing is unattributed.
SELECT
    COUNT(*) FILTER (
        WHERE c.api_key_encrypted IS NOT NULL
          AND c.api_key_org_encrypted IS NULL
          AND u.id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE c.api_key_encrypted IS NOT NULL
          AND c.api_key_org_encrypted IS NULL
          AND u.id IS NOT NULL
    )::bigint AS unreadable,
    COUNT(*) FILTER (WHERE c.api_key_encrypted IS NOT NULL)::bigint AS platform_copies
FROM ai_integration_configs AS c
LEFT JOIN organization_encryption_unreadable_values AS u
  ON u.organization_id = c.organization_id
  AND u.use_case = 'ai-provider-keys'
  AND u.source_column = 'ai_integration_configs.api_key_encrypted'
  AND u.row_id = c.id::text
  AND u.platform_ciphertext_sha256 = sha256(convert_to(c.api_key_encrypted, 'UTF8'))
WHERE c.organization_id = @organization_id::text;

-- name: ClearAIIntegrationConfigAPIKeyPlatformCopies :one
-- Returns how many cleared copies stood alone and how many stood beside an
-- organization-key copy.
WITH cleared AS (
    UPDATE ai_integration_configs AS c
    SET api_key_encrypted = NULL
    WHERE c.id IN (
        SELECT target.id FROM ai_integration_configs AS target
        WHERE target.organization_id = @organization_id::text
          AND target.api_key_encrypted IS NOT NULL
        ORDER BY target.id
        LIMIT @limit_value
      )
      AND c.organization_id = @organization_id::text
      AND COALESCE((
        SELECT t.generation FROM organization_encryption_transitions AS t
        WHERE t.organization_id = c.organization_id AND t.use_case = 'ai-provider-keys'
        FOR KEY SHARE
      ), 0) = @decided_generation
    RETURNING (c.api_key_org_encrypted IS NULL) AS platform_only
)
SELECT
    COUNT(*) FILTER (WHERE cleared.platform_only)::bigint AS platform_only,
    COUNT(*) FILTER (WHERE NOT cleared.platform_only)::bigint AS beside_org_copy
FROM cleared;

-- name: ListModelProviderKeyWaiting :many
SELECT c.id, c.api_key_encrypted::text AS platform_copy
FROM model_provider_keys AS c
WHERE c.organization_id = @organization_id::text
  AND c.api_key_encrypted IS NOT NULL
  AND c.api_key_org_encrypted IS NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: SealModelProviderKeyWaiting :execrows
-- Lands only while the row still holds the platform-key copy that was read
-- and no organization-key copy, so a concurrent replacement wins.
UPDATE model_provider_keys AS c
SET api_key_org_encrypted = @org_copy
WHERE c.id = @id
  AND c.organization_id = @organization_id::text
  AND c.api_key_encrypted = @platform_copy
  AND c.api_key_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'ai-provider-keys'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListModelProviderKeySealed :many
SELECT c.id, c.api_key_encrypted::text AS platform_copy, c.api_key_org_encrypted::text AS org_copy
FROM model_provider_keys AS c
WHERE c.organization_id = @organization_id::text
  AND c.api_key_encrypted IS NOT NULL
  AND c.api_key_org_encrypted IS NOT NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: CountModelProviderKeyCopies :one
-- Every row has an organization, so nothing is unattributed.
SELECT
    COUNT(*) FILTER (
        WHERE c.api_key_encrypted IS NOT NULL
          AND c.api_key_org_encrypted IS NULL
          AND u.id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE c.api_key_encrypted IS NOT NULL
          AND c.api_key_org_encrypted IS NULL
          AND u.id IS NOT NULL
    )::bigint AS unreadable,
    COUNT(*) FILTER (WHERE c.api_key_encrypted IS NOT NULL)::bigint AS platform_copies
FROM model_provider_keys AS c
LEFT JOIN organization_encryption_unreadable_values AS u
  ON u.organization_id = c.organization_id
  AND u.use_case = 'ai-provider-keys'
  AND u.source_column = 'model_provider_keys.api_key_encrypted'
  AND u.row_id = c.id::text
  AND u.platform_ciphertext_sha256 = sha256(convert_to(c.api_key_encrypted, 'UTF8'))
WHERE c.organization_id = @organization_id::text;

-- name: ClearModelProviderKeyPlatformCopies :one
-- Returns how many cleared copies stood alone and how many stood beside an
-- organization-key copy.
WITH cleared AS (
    UPDATE model_provider_keys AS c
    SET api_key_encrypted = NULL
    WHERE c.id IN (
        SELECT target.id FROM model_provider_keys AS target
        WHERE target.organization_id = @organization_id::text
          AND target.api_key_encrypted IS NOT NULL
        ORDER BY target.id
        LIMIT @limit_value
      )
      AND c.organization_id = @organization_id::text
      AND COALESCE((
        SELECT t.generation FROM organization_encryption_transitions AS t
        WHERE t.organization_id = c.organization_id AND t.use_case = 'ai-provider-keys'
        FOR KEY SHARE
      ), 0) = @decided_generation
    RETURNING (c.api_key_org_encrypted IS NULL) AS platform_only
)
SELECT
    COUNT(*) FILTER (WHERE cleared.platform_only)::bigint AS platform_only,
    COUNT(*) FILTER (WHERE NOT cleared.platform_only)::bigint AS beside_org_copy
FROM cleared;

-- name: ListOpenRouterAPIKeyWaiting :many
SELECT c.key_type, c.key_encrypted::text AS platform_copy
FROM openrouter_api_keys AS c
WHERE c.organization_id = @organization_id::text
  AND c.key_encrypted IS NOT NULL
  AND c.key_org_encrypted IS NULL
  AND (sqlc.narg('after')::text IS NULL OR c.key_type > sqlc.narg('after')::text)
ORDER BY c.key_type
LIMIT @limit_value;

-- name: SealOpenRouterAPIKeyWaiting :execrows
-- Lands only while the row still holds the platform-key copy that was read
-- and no organization-key copy, so a concurrent replacement wins.
UPDATE openrouter_api_keys AS c
SET key_org_encrypted = @org_copy
WHERE c.key_type = @id
  AND c.organization_id = @organization_id::text
  AND c.key_encrypted = @platform_copy
  AND c.key_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'ai-provider-keys'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListOpenRouterAPIKeySealed :many
SELECT c.key_type, c.key_encrypted::text AS platform_copy, c.key_org_encrypted::text AS org_copy
FROM openrouter_api_keys AS c
WHERE c.organization_id = @organization_id::text
  AND c.key_encrypted IS NOT NULL
  AND c.key_org_encrypted IS NOT NULL
  AND (sqlc.narg('after')::text IS NULL OR c.key_type > sqlc.narg('after')::text)
ORDER BY c.key_type
LIMIT @limit_value;

-- name: CountOpenRouterAPIKeyCopies :one
-- Every row has an organization, so nothing is unattributed.
SELECT
    COUNT(*) FILTER (
        WHERE c.key_encrypted IS NOT NULL
          AND c.key_org_encrypted IS NULL
          AND u.id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE c.key_encrypted IS NOT NULL
          AND c.key_org_encrypted IS NULL
          AND u.id IS NOT NULL
    )::bigint AS unreadable,
    COUNT(*) FILTER (WHERE c.key_encrypted IS NOT NULL)::bigint AS platform_copies
FROM openrouter_api_keys AS c
LEFT JOIN organization_encryption_unreadable_values AS u
  ON u.organization_id = c.organization_id
  AND u.use_case = 'ai-provider-keys'
  AND u.source_column = 'openrouter_api_keys.key_encrypted'
  AND u.row_id = c.key_type
  AND u.platform_ciphertext_sha256 = sha256(convert_to(c.key_encrypted, 'UTF8'))
WHERE c.organization_id = @organization_id::text;

-- name: ClearOpenRouterAPIKeyPlatformCopies :one
-- Returns how many cleared copies stood alone and how many stood beside an
-- organization-key copy.
WITH cleared AS (
    UPDATE openrouter_api_keys AS c
    SET key_encrypted = NULL
    WHERE c.key_type IN (
        SELECT target.key_type FROM openrouter_api_keys AS target
        WHERE target.organization_id = @organization_id::text
          AND target.key_encrypted IS NOT NULL
        ORDER BY target.key_type
        LIMIT @limit_value
      )
      AND c.organization_id = @organization_id::text
      AND COALESCE((
        SELECT t.generation FROM organization_encryption_transitions AS t
        WHERE t.organization_id = c.organization_id AND t.use_case = 'ai-provider-keys'
        FOR KEY SHARE
      ), 0) = @decided_generation
    RETURNING (c.key_org_encrypted IS NULL) AS platform_only
)
SELECT
    COUNT(*) FILTER (WHERE cleared.platform_only)::bigint AS platform_only,
    COUNT(*) FILTER (WHERE NOT cleared.platform_only)::bigint AS beside_org_copy
FROM cleared;
