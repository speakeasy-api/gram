-- Every write of a stored upstream OAuth token: both copies of each value
-- (the *_encrypted column under the platform key, the *_org_encrypted column
-- under the organization's key). Package upstreamtokens calls these queries
-- and owns the sealing rules, and its tests check that every write here is
-- fenced on the organization's transition generation. Statements elsewhere
-- may only erase a value, by setting both of its copies to NULL.
--
-- The fence compares the transition's organization with the row's own
-- organization_id. A row without one (a session under a global client) finds
-- no transition row and reads generation 0, the only generation such a row is
-- sealed under.

-- name: SetRemoteSessionTokens :one
-- Stores the access token, and the refresh token when replace_refresh_token
-- is set, as the copies sealed under the decided transition generation. A
-- refresh token that is not replaced keeps both stored copies as they are.
-- Run it in the transaction of the statement that created or changed the
-- session, which owns updated_at; this statement changes nothing else. No
-- rows means the session is gone, belongs to another organization, or the
-- transition moved since the decision; the caller tells them apart by reading
-- the transition again.
UPDATE remote_sessions AS s
SET
    access_token_encrypted = sqlc.narg('access_platform_copy')::text,
    access_token_org_encrypted = sqlc.narg('access_org_copy')::text,
    refresh_token_encrypted = CASE
        WHEN @replace_refresh_token::boolean THEN sqlc.narg('refresh_platform_copy')::text
        ELSE s.refresh_token_encrypted END,
    refresh_token_org_encrypted = CASE
        WHEN @replace_refresh_token::boolean THEN sqlc.narg('refresh_org_copy')::text
        ELSE s.refresh_token_org_encrypted END
WHERE s.id = @id
  AND s.organization_id IS NOT DISTINCT FROM sqlc.narg('organization_id')::text
  AND s.deleted IS FALSE
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = s.organization_id AND t.use_case = 'oauth-session-tokens'
    FOR KEY SHARE
  ), 0) = @decided_generation
RETURNING *;

-- name: ListRemoteSessionAccessTokensWaiting :many
-- Access tokens of the organization with a platform-key copy and no
-- organization-key copy, soft-deleted sessions included, paged by id.
SELECT s.id, s.access_token_encrypted::text AS platform_copy
FROM remote_sessions AS s
WHERE s.organization_id = @organization_id::text
  AND s.access_token_encrypted IS NOT NULL
  AND s.access_token_org_encrypted IS NULL
  AND (sqlc.narg('after')::uuid IS NULL OR s.id > sqlc.narg('after')::uuid)
ORDER BY s.id
LIMIT @limit_value;

-- name: SealRemoteSessionAccessTokenWaiting :execrows
-- Adds the organization-key copy of an access token the backfill sealed. It
-- lands only while the row still holds the platform-key copy that was read
-- and no organization-key copy. Nothing else about the session changes, so
-- updated_at, the refresh compare-and-swap version, stays put.
UPDATE remote_sessions AS s
SET access_token_org_encrypted = @org_copy
WHERE s.id = @id
  AND s.organization_id = @organization_id::text
  AND s.access_token_encrypted = @platform_copy
  AND s.access_token_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = s.organization_id AND t.use_case = 'oauth-session-tokens'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListRemoteSessionAccessTokensSealed :many
-- Access tokens of the organization that hold both copies, soft-deleted
-- sessions included, paged by id, for verification.
SELECT s.id, s.access_token_encrypted::text AS platform_copy, s.access_token_org_encrypted::text AS org_copy
FROM remote_sessions AS s
WHERE s.organization_id = @organization_id::text
  AND s.access_token_encrypted IS NOT NULL
  AND s.access_token_org_encrypted IS NOT NULL
  AND (sqlc.narg('after')::uuid IS NULL OR s.id > sqlc.narg('after')::uuid)
ORDER BY s.id
LIMIT @limit_value;

-- name: CountRemoteSessionAccessTokenCopies :one
-- The organization's access token counts for the backfill and finalize. A
-- waiting token recorded as unreadable counts as unreadable only while the
-- recorded hash still matches the stored platform-key copy. Unattributed
-- counts sessions without organization_id whose client belongs to the
-- organization, directly or through its project: the organization-scoped
-- queries never reach them.
SELECT
    COUNT(*) FILTER (
        WHERE s.organization_id = @organization_id::text
          AND s.access_token_encrypted IS NOT NULL
          AND s.access_token_org_encrypted IS NULL
          AND u.id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE s.organization_id = @organization_id::text
          AND s.access_token_encrypted IS NOT NULL
          AND s.access_token_org_encrypted IS NULL
          AND u.id IS NOT NULL
    )::bigint AS unreadable,
    COUNT(*) FILTER (
        WHERE s.organization_id IS NULL
          AND (s.access_token_encrypted IS NOT NULL OR s.access_token_org_encrypted IS NOT NULL)
    )::bigint AS unattributed,
    COUNT(*) FILTER (
        WHERE s.organization_id = @organization_id::text
          AND s.access_token_encrypted IS NOT NULL
    )::bigint AS platform_copies
FROM remote_sessions AS s
LEFT JOIN remote_session_clients AS c ON c.id = s.remote_session_client_id
LEFT JOIN projects AS p ON p.id = c.project_id
LEFT JOIN organization_encryption_unreadable_values AS u
  ON u.organization_id = s.organization_id
  AND u.use_case = 'oauth-session-tokens'
  AND u.source_column = 'remote_sessions.access_token_encrypted'
  AND u.row_id = s.id::text
  AND u.platform_ciphertext_sha256 = sha256(convert_to(s.access_token_encrypted, 'UTF8'))
WHERE s.organization_id = @organization_id::text
   OR (s.organization_id IS NULL AND COALESCE(p.organization_id, c.organization_id) = @organization_id::text);

-- name: ClearRemoteSessionAccessTokenPlatformCopies :execrows
-- Destroys up to limit platform-key copies of the organization's access
-- tokens after finalize. The organization-key copy stays.
UPDATE remote_sessions AS s
SET access_token_encrypted = NULL
WHERE s.id IN (
    SELECT target.id FROM remote_sessions AS target
    WHERE target.organization_id = @organization_id::text
      AND target.access_token_encrypted IS NOT NULL
    ORDER BY target.id
    LIMIT @limit_value
  )
  AND s.organization_id = @organization_id::text
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = s.organization_id AND t.use_case = 'oauth-session-tokens'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListRemoteSessionRefreshTokensWaiting :many
-- Refresh tokens of the organization with a platform-key copy and no
-- organization-key copy, soft-deleted sessions included, paged by id.
SELECT s.id, s.refresh_token_encrypted::text AS platform_copy
FROM remote_sessions AS s
WHERE s.organization_id = @organization_id::text
  AND s.refresh_token_encrypted IS NOT NULL
  AND s.refresh_token_org_encrypted IS NULL
  AND (sqlc.narg('after')::uuid IS NULL OR s.id > sqlc.narg('after')::uuid)
ORDER BY s.id
LIMIT @limit_value;

-- name: SealRemoteSessionRefreshTokenWaiting :execrows
-- Adds the organization-key copy of a refresh token the backfill sealed,
-- under the same rules as SealRemoteSessionAccessTokenWaiting.
UPDATE remote_sessions AS s
SET refresh_token_org_encrypted = @org_copy
WHERE s.id = @id
  AND s.organization_id = @organization_id::text
  AND s.refresh_token_encrypted = @platform_copy
  AND s.refresh_token_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = s.organization_id AND t.use_case = 'oauth-session-tokens'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListRemoteSessionRefreshTokensSealed :many
-- Refresh tokens of the organization that hold both copies, soft-deleted
-- sessions included, paged by id, for verification.
SELECT s.id, s.refresh_token_encrypted::text AS platform_copy, s.refresh_token_org_encrypted::text AS org_copy
FROM remote_sessions AS s
WHERE s.organization_id = @organization_id::text
  AND s.refresh_token_encrypted IS NOT NULL
  AND s.refresh_token_org_encrypted IS NOT NULL
  AND (sqlc.narg('after')::uuid IS NULL OR s.id > sqlc.narg('after')::uuid)
ORDER BY s.id
LIMIT @limit_value;

-- name: CountRemoteSessionRefreshTokenCopies :one
-- The organization's refresh token counts, under the same rules as
-- CountRemoteSessionAccessTokenCopies.
SELECT
    COUNT(*) FILTER (
        WHERE s.organization_id = @organization_id::text
          AND s.refresh_token_encrypted IS NOT NULL
          AND s.refresh_token_org_encrypted IS NULL
          AND u.id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE s.organization_id = @organization_id::text
          AND s.refresh_token_encrypted IS NOT NULL
          AND s.refresh_token_org_encrypted IS NULL
          AND u.id IS NOT NULL
    )::bigint AS unreadable,
    COUNT(*) FILTER (
        WHERE s.organization_id IS NULL
          AND (s.refresh_token_encrypted IS NOT NULL OR s.refresh_token_org_encrypted IS NOT NULL)
    )::bigint AS unattributed,
    COUNT(*) FILTER (
        WHERE s.organization_id = @organization_id::text
          AND s.refresh_token_encrypted IS NOT NULL
    )::bigint AS platform_copies
FROM remote_sessions AS s
LEFT JOIN remote_session_clients AS c ON c.id = s.remote_session_client_id
LEFT JOIN projects AS p ON p.id = c.project_id
LEFT JOIN organization_encryption_unreadable_values AS u
  ON u.organization_id = s.organization_id
  AND u.use_case = 'oauth-session-tokens'
  AND u.source_column = 'remote_sessions.refresh_token_encrypted'
  AND u.row_id = s.id::text
  AND u.platform_ciphertext_sha256 = sha256(convert_to(s.refresh_token_encrypted, 'UTF8'))
WHERE s.organization_id = @organization_id::text
   OR (s.organization_id IS NULL AND COALESCE(p.organization_id, c.organization_id) = @organization_id::text);

-- name: ClearRemoteSessionRefreshTokenPlatformCopies :execrows
-- Destroys up to limit platform-key copies of the organization's refresh
-- tokens after finalize. The organization-key copy stays.
UPDATE remote_sessions AS s
SET refresh_token_encrypted = NULL
WHERE s.id IN (
    SELECT target.id FROM remote_sessions AS target
    WHERE target.organization_id = @organization_id::text
      AND target.refresh_token_encrypted IS NOT NULL
    ORDER BY target.id
    LIMIT @limit_value
  )
  AND s.organization_id = @organization_id::text
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = s.organization_id AND t.use_case = 'oauth-session-tokens'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: GetRemoteSessionRowFixture :one
-- Test fixture: the session row as stored, soft-deleted or not.
SELECT * FROM remote_sessions WHERE id = @id;

-- name: CreateRemoteSessionFixture :one
-- Test fixture: a live session with no tokens. Its organization_id is its
-- client's organization, as every writer of the table sets it.
INSERT INTO remote_sessions (subject_urn, user_session_issuer_id, remote_session_client_id, organization_id)
VALUES (
    @subject_urn,
    @user_session_issuer_id,
    @remote_session_client_id,
    (
        SELECT org.id
        FROM remote_session_clients AS client
        LEFT JOIN projects AS project ON project.id = client.project_id
        JOIN organization_metadata AS org ON org.id = COALESCE(project.organization_id, client.organization_id)
        WHERE client.id = @remote_session_client_id
    )
)
RETURNING *;

-- name: ClearRemoteSessionOrganizationFixture :one
-- Test fixture: a session whose organization_id was never backfilled.
UPDATE remote_sessions SET organization_id = NULL WHERE id = @id
RETURNING *;

-- name: SoftDeleteRemoteSessionFixture :exec
-- Test fixture: soft-deletes a session, keeping its tokens as revocation does.
UPDATE remote_sessions SET deleted_at = clock_timestamp() WHERE id = @id;
