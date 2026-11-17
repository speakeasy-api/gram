-- Every write of a remote session client's secret: both copies
-- (client_secret_encrypted under the platform key, client_secret_org_encrypted
-- under the organization's key) and the client_secret_generation revision.
-- Nothing else writes those columns; package clientsecrets calls these
-- queries and owns the sealing rules, and its tests check that every write
-- here is fenced on the organization's transition generation.
--
-- The fence compares the transition's organization with the row's own
-- organization_id. A global client has none, so the subquery finds no row and
-- reads generation 0, the only generation a global client is sealed under.

-- name: SetRemoteSessionClientSecret :one
-- Stores a new or replaced secret as the copies sealed under the decided
-- transition generation, and counts a new revision. A replaced secret takes
-- the expiry supplied with it, or none, and clears the upstream rejection
-- marker: the rejection may have been the old secret. No rows means the row
-- is gone or the transition moved since the decision; the caller tells the two
-- apart by reading the transition again.
UPDATE remote_session_clients AS c
SET
    client_secret_encrypted = sqlc.narg('platform_copy'),
    client_secret_org_encrypted = sqlc.narg('org_copy'),
    client_secret_generation = c.client_secret_generation + 1,
    client_secret_expires_at = sqlc.narg('client_secret_expires_at'),
    upstream_rejected_at = NULL,
    updated_at = clock_timestamp()
WHERE c.id = @id
  AND c.organization_id IS NOT DISTINCT FROM sqlc.narg('organization_id')::text
  AND c.deleted IS FALSE
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'oauth-client-secrets'
    FOR KEY SHARE
  ), 0) = @decided_generation
RETURNING *;

-- name: ClearRemoteSessionClientSecret :one
-- Removes the secret: both copies, so a removed secret cannot come back from
-- the other one, and counts a new revision so anything derived from the old
-- secret stops matching. Like a replaced secret, it clears the upstream
-- rejection marker, which belonged to the removed secret. The registration's
-- expiry is left to the statement that changed the registration.
-- orgencryption:unfenced erasing both copies is correct under any transition state
UPDATE remote_session_clients AS c
SET
    client_secret_encrypted = NULL,
    client_secret_org_encrypted = NULL,
    client_secret_generation = c.client_secret_generation + 1,
    upstream_rejected_at = NULL,
    updated_at = clock_timestamp()
WHERE c.id = @id
  AND c.organization_id IS NOT DISTINCT FROM sqlc.narg('organization_id')::text
  AND c.deleted IS FALSE
RETURNING *;

-- name: ListRemoteSessionClientSecretsWaiting :many
-- Secrets of the organization with a platform-key copy and no
-- organization-key copy, soft-deleted clients included, paged by id.
SELECT c.id, c.client_secret_encrypted::text AS platform_copy
FROM remote_session_clients AS c
WHERE c.organization_id = @organization_id::text
  AND c.client_secret_encrypted IS NOT NULL
  AND c.client_secret_org_encrypted IS NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: SealRemoteSessionClientSecretWaiting :execrows
-- Adds the organization-key copy of a secret the backfill sealed. It lands
-- only while the row still holds the platform-key copy that was read and no
-- organization-key copy. The same secret sealed again is not a new revision,
-- and nothing else about the client changes.
UPDATE remote_session_clients AS c
SET client_secret_org_encrypted = @org_copy
WHERE c.id = @id
  AND c.organization_id = @organization_id::text
  AND c.client_secret_encrypted = @platform_copy
  AND c.client_secret_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'oauth-client-secrets'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListRemoteSessionClientSecretsSealed :many
-- Secrets of the organization that hold both copies, soft-deleted clients
-- included, paged by id, for verification.
SELECT c.id, c.client_secret_encrypted::text AS platform_copy, c.client_secret_org_encrypted::text AS org_copy
FROM remote_session_clients AS c
WHERE c.organization_id = @organization_id::text
  AND c.client_secret_encrypted IS NOT NULL
  AND c.client_secret_org_encrypted IS NOT NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: CountRemoteSessionClientSecretCopies :one
-- The organization's secret counts for the backfill and finalize. A waiting
-- secret recorded as unreadable counts as unreadable only while the recorded
-- hash still matches the stored platform-key copy. Unattributed counts project
-- clients of the organization's projects stored without organization_id,
-- which the organization-scoped queries never reach.
SELECT
    COUNT(*) FILTER (
        WHERE c.organization_id = @organization_id::text
          AND c.client_secret_encrypted IS NOT NULL
          AND c.client_secret_org_encrypted IS NULL
          AND u.id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE c.organization_id = @organization_id::text
          AND c.client_secret_encrypted IS NOT NULL
          AND c.client_secret_org_encrypted IS NULL
          AND u.id IS NOT NULL
    )::bigint AS unreadable,
    COUNT(*) FILTER (
        WHERE c.organization_id IS NULL
          AND (c.client_secret_encrypted IS NOT NULL OR c.client_secret_org_encrypted IS NOT NULL)
    )::bigint AS unattributed,
    COUNT(*) FILTER (
        WHERE c.organization_id = @organization_id::text
          AND c.client_secret_encrypted IS NOT NULL
    )::bigint AS platform_copies
FROM remote_session_clients AS c
LEFT JOIN projects AS p ON p.id = c.project_id
LEFT JOIN organization_encryption_unreadable_values AS u
  ON u.organization_id = c.organization_id
  AND u.use_case = 'oauth-client-secrets'
  AND u.source_column = 'remote_session_clients.client_secret_encrypted'
  AND u.row_id = c.id::text
  AND u.platform_ciphertext_sha256 = sha256(convert_to(c.client_secret_encrypted, 'UTF8'))
WHERE c.organization_id = @organization_id::text
   OR (c.organization_id IS NULL AND p.organization_id = @organization_id::text);

-- name: ClearRemoteSessionClientSecretPlatformCopies :execrows
-- Destroys up to limit platform-key copies of the organization's secrets
-- after finalize. The organization-key copy stays, so this is not a new
-- revision.
UPDATE remote_session_clients AS c
SET client_secret_encrypted = NULL
WHERE c.id IN (
    SELECT target.id FROM remote_session_clients AS target
    WHERE target.organization_id = @organization_id::text
      AND target.client_secret_encrypted IS NOT NULL
    ORDER BY target.id
    LIMIT @limit_value
  )
  AND c.organization_id = @organization_id::text
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'oauth-client-secrets'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: GetRemoteSessionClientRowFixture :one
-- Test fixture: the client row as stored, soft-deleted or not.
SELECT * FROM remote_session_clients WHERE id = @id;
