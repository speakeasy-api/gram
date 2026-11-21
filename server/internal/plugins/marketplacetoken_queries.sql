-- Every write of a plugin marketplace token: its hash, both sealed copies
-- (marketplace_token_encrypted under the platform key,
-- marketplace_token_org_encrypted under the organization's key) and the
-- deprecated plaintext marketplace_token column, which only the backfill
-- clears. Nothing else writes those columns; package marketplacetokens calls
-- these queries and owns the sealing rules, and its tests check that every
-- write here is fenced on the organization's transition generation.
--
-- The fence compares the transition's organization with the row's own
-- organization_id. A connection stored before that column existed has none,
-- so the subquery finds no row and reads generation 0; the backfill fills
-- organization_id before it seals anything.

-- name: StoreMarketplaceToken :execrows
-- Gives a connection without a marketplace token the hash and copies of a new
-- token, sealed under the decided transition generation. The publish path
-- runs it in the transaction that upserted the connection, which holds the
-- row's lock. It never replaces a token: no rows means the connection is gone,
-- belongs to another organization, already holds a token, or the transition
-- moved since the decision; the caller tells the last apart by reading the
-- transition again. updated_at stays the last-published timestamp the upsert
-- set.
UPDATE plugin_github_connections AS c
SET
    marketplace_token_hash = @token_hash,
    marketplace_token_encrypted = sqlc.narg('platform_copy'),
    marketplace_token_org_encrypted = sqlc.narg('org_copy')
WHERE c.id = @id
  AND c.organization_id = @organization_id::text
  AND c.marketplace_token_hash IS NULL
  AND c.marketplace_token IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'marketplace-tokens'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: MoveLegacyMarketplaceToken :execrows
-- The backfill's move of a plaintext token: stores its hash and copies sealed
-- under the decided transition generation and clears the plaintext column, in
-- one statement so the token is never without a readable form. It lands only
-- while the row still holds the plaintext token that was read and no hash.
-- Nothing else about the connection changes.
UPDATE plugin_github_connections AS c
SET
    marketplace_token_hash = @token_hash,
    marketplace_token_encrypted = sqlc.narg('platform_copy'),
    marketplace_token_org_encrypted = sqlc.narg('org_copy'),
    marketplace_token = NULL
WHERE c.id = @id
  AND c.organization_id = @organization_id::text
  AND c.marketplace_token = @legacy_token
  AND c.marketplace_token_hash IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'marketplace-tokens'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: AttributeGitHubConnections :execrows
-- The backfill's first step: copies each project's organization onto up to
-- limit connections stored without one, so every later write fences on the
-- right organization. projects.organization_id has no foreign key, so a
-- project whose organization has no metadata row is never picked: its
-- connection keeps a NULL organization_id and counts as unattributed. It
-- writes no token column, and updated_at stays the last-published timestamp.
UPDATE plugin_github_connections AS c
SET organization_id = p.organization_id
FROM projects AS p
WHERE c.id IN (
    SELECT target.id FROM plugin_github_connections AS target
    JOIN projects AS tp ON tp.id = target.project_id
    JOIN organization_metadata AS om ON om.id = tp.organization_id
    WHERE target.organization_id IS NULL
    ORDER BY target.id
    LIMIT @limit_value
  )
  AND p.id = c.project_id
  AND c.organization_id IS NULL;

-- name: ListLegacyMarketplaceTokens :many
-- The backfill's next page of connections that still hold a plaintext token,
-- paged by id. Connections without an organization are left to
-- AttributeGitHubConnections.
SELECT c.id, c.organization_id::text AS organization_id, c.marketplace_token::text AS legacy_token
FROM plugin_github_connections AS c
WHERE c.marketplace_token IS NOT NULL
  AND c.organization_id IS NOT NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: CountMarketplaceTokenBackfill :one
-- What the backfill has left to do across every organization.
SELECT
    COUNT(*) FILTER (WHERE c.organization_id IS NULL)::bigint AS unattributed,
    COUNT(*) FILTER (WHERE c.marketplace_token IS NOT NULL)::bigint AS legacy,
    COUNT(*) FILTER (WHERE c.marketplace_token_hash IS NOT NULL)::bigint AS hashed
FROM plugin_github_connections AS c;

-- name: ListMarketplaceTokensWaiting :many
-- Tokens of the organization with a platform-key copy and no
-- organization-key copy, paged by id.
SELECT c.id, c.marketplace_token_encrypted::text AS platform_copy
FROM plugin_github_connections AS c
WHERE c.organization_id = @organization_id::text
  AND c.marketplace_token_encrypted IS NOT NULL
  AND c.marketplace_token_org_encrypted IS NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: SealMarketplaceTokenWaiting :execrows
-- Adds the organization-key copy of a token the organization encryption
-- backfill sealed. It lands only while the row still holds the platform-key
-- copy that was read and no organization-key copy. Nothing else about the
-- connection changes.
UPDATE plugin_github_connections AS c
SET marketplace_token_org_encrypted = @org_copy
WHERE c.id = @id
  AND c.organization_id = @organization_id::text
  AND c.marketplace_token_encrypted = @platform_copy
  AND c.marketplace_token_org_encrypted IS NULL
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'marketplace-tokens'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: ListMarketplaceTokensSealed :many
-- Tokens of the organization that hold both copies, paged by id, for
-- verification.
SELECT c.id, c.marketplace_token_encrypted::text AS platform_copy, c.marketplace_token_org_encrypted::text AS org_copy
FROM plugin_github_connections AS c
WHERE c.organization_id = @organization_id::text
  AND c.marketplace_token_encrypted IS NOT NULL
  AND c.marketplace_token_org_encrypted IS NOT NULL
  AND (sqlc.narg('after')::uuid IS NULL OR c.id > sqlc.narg('after')::uuid)
ORDER BY c.id
LIMIT @limit_value;

-- name: CountMarketplaceTokenCopies :one
-- The organization's token counts for the organization encryption backfill and
-- finalize. A waiting token recorded as unreadable counts as unreadable only
-- while the recorded hash still matches the stored platform-key copy.
-- Unattributed counts tokens the organization-scoped queries never reach:
-- connections of the organization's projects stored without organization_id,
-- and connections that still hold a plaintext token the marketplace token
-- backfill has not moved. Either keeps the organization from finalizing.
SELECT
    COUNT(*) FILTER (
        WHERE c.organization_id = @organization_id::text
          AND c.marketplace_token_encrypted IS NOT NULL
          AND c.marketplace_token_org_encrypted IS NULL
          AND u.id IS NULL
    )::bigint AS waiting,
    COUNT(*) FILTER (
        WHERE c.organization_id = @organization_id::text
          AND c.marketplace_token_encrypted IS NOT NULL
          AND c.marketplace_token_org_encrypted IS NULL
          AND u.id IS NOT NULL
    )::bigint AS unreadable,
    COUNT(*) FILTER (
        WHERE c.marketplace_token IS NOT NULL
           OR (
               c.organization_id IS NULL
               AND (
                   c.marketplace_token_hash IS NOT NULL
                   OR c.marketplace_token_encrypted IS NOT NULL
                   OR c.marketplace_token_org_encrypted IS NOT NULL
               )
           )
    )::bigint AS unattributed,
    COUNT(*) FILTER (
        WHERE c.organization_id = @organization_id::text
          AND c.marketplace_token_encrypted IS NOT NULL
    )::bigint AS platform_copies
FROM plugin_github_connections AS c
LEFT JOIN projects AS p ON p.id = c.project_id
LEFT JOIN organization_encryption_unreadable_values AS u
  ON u.organization_id = c.organization_id
  AND u.use_case = 'marketplace-tokens'
  AND u.source_column = 'plugin_github_connections.marketplace_token_encrypted'
  AND u.row_id = c.id::text
  AND u.platform_ciphertext_sha256 = sha256(convert_to(c.marketplace_token_encrypted, 'UTF8'))
WHERE c.organization_id = @organization_id::text
   OR (c.organization_id IS NULL AND p.organization_id = @organization_id::text);

-- name: ClearMarketplaceTokenPlatformCopies :execrows
-- Destroys up to limit platform-key copies of the organization's tokens after
-- finalize. The hash and the organization-key copy stay.
UPDATE plugin_github_connections AS c
SET marketplace_token_encrypted = NULL
WHERE c.id IN (
    SELECT target.id FROM plugin_github_connections AS target
    WHERE target.organization_id = @organization_id::text
      AND target.marketplace_token_encrypted IS NOT NULL
    ORDER BY target.id
    LIMIT @limit_value
  )
  AND c.organization_id = @organization_id::text
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = c.organization_id AND t.use_case = 'marketplace-tokens'
    FOR KEY SHARE
  ), 0) = @decided_generation;

-- name: GetGitHubConnectionRowFixture :one
-- Test fixture: the connection row as stored.
SELECT * FROM plugin_github_connections WHERE id = @id;

-- name: PlantMarketplaceTokenFixture :exec
-- Test fixture: stores token columns as given, unfenced, for tests that need a
-- connection in a state the publish path never writes, such as a plaintext
-- token from before the backfill.
-- orgencryption:unfenced test fixture that production code never calls
UPDATE plugin_github_connections AS c
SET
    organization_id = sqlc.narg('organization_id'),
    marketplace_token = sqlc.narg('legacy_token'),
    marketplace_token_hash = sqlc.narg('token_hash'),
    marketplace_token_encrypted = sqlc.narg('platform_copy'),
    marketplace_token_org_encrypted = sqlc.narg('org_copy')
WHERE c.id = @id;
