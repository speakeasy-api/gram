-- Every write of a network ingress's provider credentials: both copies
-- (credentials_encrypted under the platform key, credentials_org_encrypted
-- under the organization's key). Nothing else writes those columns; package
-- ingresscredentials calls these queries and owns the sealing rules, and its
-- tests check that every write here is fenced on the organization's
-- transition generation for the tailscale-oauth-clients use case.

-- name: SetNetworkIngressCredentials :one
-- Stores new or replaced credentials as the copies sealed under the decided
-- transition generation. New credentials are new desired state, so the
-- ingress goes back to pending for the reconciler. No rows means the ingress
-- is gone or the transition moved since the decision; the caller tells the
-- two apart by reading the transition again.
UPDATE network_ingresses AS n
SET
    credentials_encrypted = sqlc.narg('platform_copy'),
    credentials_org_encrypted = sqlc.narg('org_copy'),
    status = 'pending',
    last_error = NULL,
    updated_at = clock_timestamp()
WHERE n.id = @id
  AND n.organization_id = @organization_id
  AND n.deleted IS FALSE
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = n.organization_id AND t.use_case = 'tailscale-oauth-clients'
    FOR KEY SHARE
  ), 0) = @decided_generation
RETURNING *;

-- name: ClearDeletedNetworkIngressResources :execrows
-- AIS-611 calls this only after every persisted provider resource is confirmed
-- absent. Clearing the credentials and the resources is the replacement-create
-- release boundary.
-- orgencryption:unfenced erasing both copies is correct under any transition state
UPDATE network_ingresses
SET
    credentials_encrypted = NULL,
    credentials_org_encrypted = NULL,
    provider_resources = '{}'::jsonb,
    last_error = NULL,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS TRUE;

-- name: ListNetworkIngressCredentialsWaiting :many
-- Credentials of the organization with a platform-key copy and no
-- organization-key copy, soft-deleted ingresses included, paged by id.
SELECT n.id, n.credentials_encrypted::text AS platform_copy
FROM network_ingresses AS n
WHERE n.organization_id = @organization_id
  AND n.credentials_encrypted IS NOT NULL
  AND n.credentials_org_encrypted IS NULL
  AND (sqlc.narg('after')::uuid IS NULL OR n.id > sqlc.narg('after')::uuid)
ORDER BY n.id
LIMIT @limit_value;

-- name: SealNetworkIngressCredentialsWaiting :execrows
-- Adds the organization-key copy of credentials the backfill sealed. It lands
-- only while the row still holds the platform-key copy that was read and no
-- organization-key copy. The same credentials sealed again are not new desired
-- state, so updated_at stays.
UPDATE network_ingresses AS n
SET credentials_org_encrypted = @org_copy
WHERE n.id = @id
  AND n.organization_id = @organization_id
  AND n.credentials_encrypted = @platform_copy
  AND n.credentials_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = n.organization_id AND t.use_case = 'tailscale-oauth-clients'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListNetworkIngressCredentialsSealed :many
-- Credentials of the organization that hold both copies, soft-deleted
-- ingresses included, paged by id, for verification.
SELECT n.id, n.credentials_encrypted::text AS platform_copy, n.credentials_org_encrypted::text AS org_copy
FROM network_ingresses AS n
WHERE n.organization_id = @organization_id
  AND n.credentials_encrypted IS NOT NULL
  AND n.credentials_org_encrypted IS NOT NULL
  AND (sqlc.narg('after')::uuid IS NULL OR n.id > sqlc.narg('after')::uuid)
ORDER BY n.id
LIMIT @limit_value;

-- name: CountNetworkIngressCredentialCopies :one
-- The organization's credential counts for the backfill and finalize. Waiting
-- credentials recorded as unreadable count as unreadable only while the
-- recorded hash still matches the stored platform-key copy. organization_id is
-- NOT NULL, so nothing is unattributed.
SELECT
    COUNT(*) FILTER (
        WHERE n.credentials_encrypted IS NOT NULL
          AND n.credentials_org_encrypted IS NULL
          AND u.id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE n.credentials_encrypted IS NOT NULL
          AND n.credentials_org_encrypted IS NULL
          AND u.id IS NOT NULL
    )::bigint AS unreadable,
    COUNT(*) FILTER (
        WHERE n.credentials_encrypted IS NOT NULL
    )::bigint AS platform_copies
FROM network_ingresses AS n
LEFT JOIN organization_encryption_unreadable_values AS u
  ON u.organization_id = n.organization_id
  AND u.use_case = 'tailscale-oauth-clients'
  AND u.source_column = 'network_ingresses.credentials_encrypted'
  AND u.row_id = n.id::text
  AND u.platform_ciphertext_sha256 = sha256(convert_to(n.credentials_encrypted, 'UTF8'))
WHERE n.organization_id = @organization_id;

-- name: ClearNetworkIngressCredentialPlatformCopies :one
-- Destroys up to limit platform-key copies of the organization's credentials
-- after finalize. Credentials with an organization-key copy keep it; those
-- recorded as unreadable have none, so they are left with no credentials at
-- all. Either way this is not new desired state, so updated_at stays.
-- Returns how many cleared copies stood alone and how many stood beside an
-- organization-key copy. The outer statement rechecks that the platform-key
-- copy is still set, so a row a concurrent clear already emptied is neither
-- updated nor counted.
WITH cleared AS (
    UPDATE network_ingresses AS n
    SET credentials_encrypted = NULL
    WHERE n.id IN (
        SELECT target.id FROM network_ingresses AS target
        WHERE target.organization_id = @organization_id
          AND target.credentials_encrypted IS NOT NULL
        ORDER BY target.id
        LIMIT @limit_value
      )
      AND n.organization_id = @organization_id
      AND n.credentials_encrypted IS NOT NULL
      AND COALESCE((
        SELECT t.generation FROM organization_encryption_transitions AS t
        WHERE t.organization_id = n.organization_id AND t.use_case = 'tailscale-oauth-clients'
        FOR KEY SHARE
      ), 0) = @decided_generation
    RETURNING (n.credentials_org_encrypted IS NULL) AS platform_only
)
SELECT
    COUNT(*) FILTER (WHERE cleared.platform_only)::bigint AS platform_only,
    COUNT(*) FILTER (WHERE NOT cleared.platform_only)::bigint AS beside_org_copy
FROM cleared;

-- name: SetNetworkIngressCredentialCopiesFixture :one
-- Test fixture: plants both copies as given, unsealed and unfenced.
-- orgencryption:unfenced test fixture that plants copies directly
UPDATE network_ingresses
SET credentials_encrypted = sqlc.narg('platform_copy'), credentials_org_encrypted = sqlc.narg('org_copy')
WHERE id = @id
  AND organization_id = @organization_id
RETURNING *;
