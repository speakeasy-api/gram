-- The backfill, verification and finalize side of moving integration
-- credentials to organization keys (package orgencryption's CopyStore), one
-- set of queries per table. Every query covers soft-deleted rows too, and
-- every write changes only the copy column it names, never updated_at:
-- network ingress reconciliation treats a new updated_at as a new desired
-- state. The writes that store a new credential live in each integration's
-- own queries.sql.

-- name: ListSlackDirectoryConnectionCredentialsWaiting :many
SELECT c.id, c.credentials_encrypted::text AS platform_copy
FROM slack_directory_connections AS c
WHERE c.organization_id = @organization_id::text
  AND c.credentials_encrypted IS NOT NULL
  AND c.credentials_org_encrypted IS NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: SealSlackDirectoryConnectionCredentialsWaiting :execrows
-- Lands only while the row still holds the platform-key copy that was read
-- and no organization-key copy, so a concurrent rotation or disconnect wins.
UPDATE slack_directory_connections AS c
SET credentials_org_encrypted = @org_copy
WHERE c.id = @id
  AND c.organization_id = @organization_id::text
  AND c.credentials_encrypted = @platform_copy
  AND c.credentials_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'integration-credentials'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListSlackDirectoryConnectionCredentialsSealed :many
SELECT c.id, c.credentials_encrypted::text AS platform_copy, c.credentials_org_encrypted::text AS org_copy
FROM slack_directory_connections AS c
WHERE c.organization_id = @organization_id::text
  AND c.credentials_encrypted IS NOT NULL
  AND c.credentials_org_encrypted IS NOT NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: CountSlackDirectoryConnectionCredentialCopies :one
-- Every row has an organization, so nothing is unattributed.
SELECT
    COUNT(*) FILTER (
        WHERE c.credentials_encrypted IS NOT NULL
          AND c.credentials_org_encrypted IS NULL
          AND u.id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE c.credentials_encrypted IS NOT NULL
          AND c.credentials_org_encrypted IS NULL
          AND u.id IS NOT NULL
    )::bigint AS unreadable,
    COUNT(*) FILTER (WHERE c.credentials_encrypted IS NOT NULL)::bigint AS platform_copies
FROM slack_directory_connections AS c
LEFT JOIN organization_encryption_unreadable_values AS u
  ON u.organization_id = c.organization_id
  AND u.use_case = 'integration-credentials'
  AND u.source_column = 'slack_directory_connections.credentials_encrypted'
  AND u.row_id = c.id::text
  AND u.platform_ciphertext_sha256 = sha256(convert_to(c.credentials_encrypted, 'UTF8'))
WHERE c.organization_id = @organization_id::text;

-- name: ClearSlackDirectoryConnectionCredentialPlatformCopies :execrows
UPDATE slack_directory_connections AS c
SET credentials_encrypted = NULL
WHERE c.id IN (
    SELECT target.id FROM slack_directory_connections AS target
    WHERE target.organization_id = @organization_id::text
      AND target.credentials_encrypted IS NOT NULL
    ORDER BY target.id
    LIMIT @limit_value
  )
  AND c.organization_id = @organization_id::text
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'integration-credentials'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListDeviceIntegrationConfigCredentialsWaiting :many
SELECT c.id, c.credentials_encrypted::text AS platform_copy
FROM device_integration_configs AS c
WHERE c.organization_id = @organization_id::text
  AND c.credentials_encrypted IS NOT NULL
  AND c.credentials_org_encrypted IS NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: SealDeviceIntegrationConfigCredentialsWaiting :execrows
-- Lands only while the row still holds the platform-key copy that was read
-- and no organization-key copy, so a concurrent rotation or disconnect wins.
UPDATE device_integration_configs AS c
SET credentials_org_encrypted = @org_copy
WHERE c.id = @id
  AND c.organization_id = @organization_id::text
  AND c.credentials_encrypted = @platform_copy
  AND c.credentials_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'integration-credentials'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListDeviceIntegrationConfigCredentialsSealed :many
SELECT c.id, c.credentials_encrypted::text AS platform_copy, c.credentials_org_encrypted::text AS org_copy
FROM device_integration_configs AS c
WHERE c.organization_id = @organization_id::text
  AND c.credentials_encrypted IS NOT NULL
  AND c.credentials_org_encrypted IS NOT NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: CountDeviceIntegrationConfigCredentialCopies :one
-- Every row has an organization, so nothing is unattributed.
SELECT
    COUNT(*) FILTER (
        WHERE c.credentials_encrypted IS NOT NULL
          AND c.credentials_org_encrypted IS NULL
          AND u.id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE c.credentials_encrypted IS NOT NULL
          AND c.credentials_org_encrypted IS NULL
          AND u.id IS NOT NULL
    )::bigint AS unreadable,
    COUNT(*) FILTER (WHERE c.credentials_encrypted IS NOT NULL)::bigint AS platform_copies
FROM device_integration_configs AS c
LEFT JOIN organization_encryption_unreadable_values AS u
  ON u.organization_id = c.organization_id
  AND u.use_case = 'integration-credentials'
  AND u.source_column = 'device_integration_configs.credentials_encrypted'
  AND u.row_id = c.id::text
  AND u.platform_ciphertext_sha256 = sha256(convert_to(c.credentials_encrypted, 'UTF8'))
WHERE c.organization_id = @organization_id::text;

-- name: ClearDeviceIntegrationConfigCredentialPlatformCopies :execrows
UPDATE device_integration_configs AS c
SET credentials_encrypted = NULL
WHERE c.id IN (
    SELECT target.id FROM device_integration_configs AS target
    WHERE target.organization_id = @organization_id::text
      AND target.credentials_encrypted IS NOT NULL
    ORDER BY target.id
    LIMIT @limit_value
  )
  AND c.organization_id = @organization_id::text
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'integration-credentials'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListNetworkIngressCredentialsWaiting :many
SELECT c.id, c.credentials_encrypted::text AS platform_copy
FROM network_ingresses AS c
WHERE c.organization_id = @organization_id::text
  AND c.credentials_encrypted IS NOT NULL
  AND c.credentials_org_encrypted IS NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: SealNetworkIngressCredentialsWaiting :execrows
-- Lands only while the row still holds the platform-key copy that was read
-- and no organization-key copy, so a concurrent rotation or disconnect wins.
UPDATE network_ingresses AS c
SET credentials_org_encrypted = @org_copy
WHERE c.id = @id
  AND c.organization_id = @organization_id::text
  AND c.credentials_encrypted = @platform_copy
  AND c.credentials_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'integration-credentials'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListNetworkIngressCredentialsSealed :many
SELECT c.id, c.credentials_encrypted::text AS platform_copy, c.credentials_org_encrypted::text AS org_copy
FROM network_ingresses AS c
WHERE c.organization_id = @organization_id::text
  AND c.credentials_encrypted IS NOT NULL
  AND c.credentials_org_encrypted IS NOT NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: CountNetworkIngressCredentialCopies :one
-- Every row has an organization, so nothing is unattributed.
SELECT
    COUNT(*) FILTER (
        WHERE c.credentials_encrypted IS NOT NULL
          AND c.credentials_org_encrypted IS NULL
          AND u.id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE c.credentials_encrypted IS NOT NULL
          AND c.credentials_org_encrypted IS NULL
          AND u.id IS NOT NULL
    )::bigint AS unreadable,
    COUNT(*) FILTER (WHERE c.credentials_encrypted IS NOT NULL)::bigint AS platform_copies
FROM network_ingresses AS c
LEFT JOIN organization_encryption_unreadable_values AS u
  ON u.organization_id = c.organization_id
  AND u.use_case = 'integration-credentials'
  AND u.source_column = 'network_ingresses.credentials_encrypted'
  AND u.row_id = c.id::text
  AND u.platform_ciphertext_sha256 = sha256(convert_to(c.credentials_encrypted, 'UTF8'))
WHERE c.organization_id = @organization_id::text;

-- name: ClearNetworkIngressCredentialPlatformCopies :execrows
UPDATE network_ingresses AS c
SET credentials_encrypted = NULL
WHERE c.id IN (
    SELECT target.id FROM network_ingresses AS target
    WHERE target.organization_id = @organization_id::text
      AND target.credentials_encrypted IS NOT NULL
    ORDER BY target.id
    LIMIT @limit_value
  )
  AND c.organization_id = @organization_id::text
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'integration-credentials'
    FOR KEY SHARE
  ), 0) = @decided_generation;
