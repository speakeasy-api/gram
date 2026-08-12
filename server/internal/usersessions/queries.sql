-- name: CreateUserSessionIssuer :one
INSERT INTO user_session_issuers (
    project_id,
    slug,
    authn_challenge_mode,
    session_duration
)
VALUES (
    @project_id,
    @slug,
    @authn_challenge_mode,
    @session_duration
)
RETURNING *;

-- name: GetUserSessionIssuerByID :one
SELECT *
FROM user_session_issuers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetUserSessionIssuerBySlug :one
SELECT *
FROM user_session_issuers
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: ListUserSessionIssuersByProjectID :many
SELECT *
FROM user_session_issuers
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
ORDER BY id DESC
LIMIT sqlc.arg('limit_value');

-- name: UpdateUserSessionIssuer :one
UPDATE user_session_issuers
SET
    slug = COALESCE(sqlc.narg('slug')::text, slug),
    authn_challenge_mode = COALESCE(sqlc.narg('authn_challenge_mode')::text, authn_challenge_mode),
    session_duration = COALESCE(sqlc.narg('session_duration')::interval, session_duration),
    -- Omitting the mode keeps the stored value, including NULL. Once a
    -- concrete mode is written the column can never return to NULL through
    -- this endpoint: "never configured" is a one-way state, and the API
    -- reports the resolved effective mode either way.
    client_id_metadata_admission_mode = COALESCE(sqlc.narg('client_id_metadata_admission_mode')::text, client_id_metadata_admission_mode),
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteUserSessionIssuer :one
-- Recheck active owners in the write so an owner added after the handler's
-- preflight check prevents the issuer from being soft-deleted.
UPDATE user_session_issuers AS issuer
SET deleted_at = clock_timestamp()
WHERE issuer.id = @id
  AND issuer.project_id = @project_id
  AND issuer.deleted IS FALSE
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_servers AS server
    WHERE server.project_id = @project_id
      AND server.user_session_issuer_id = issuer.id
      AND server.deleted IS FALSE

    UNION ALL

    SELECT 1
    FROM toolsets AS toolset
    WHERE toolset.project_id = @project_id
      AND toolset.user_session_issuer_id = issuer.id
      AND toolset.deleted IS FALSE
  )
RETURNING issuer.*;

-- name: UserSessionIssuerHasActiveOwner :one
-- An issuer can be referenced by an MCP server or toolset. Only delete it once
-- no active owner remains.
SELECT EXISTS (
    SELECT 1
    FROM mcp_servers AS server
    WHERE server.project_id = sqlc.arg('project_id')
      AND server.user_session_issuer_id = sqlc.arg('user_session_issuer_id')::uuid
      AND server.deleted IS FALSE

    UNION ALL

    SELECT 1
    FROM toolsets AS toolset
    WHERE toolset.project_id = sqlc.arg('project_id')
      AND toolset.user_session_issuer_id = sqlc.arg('user_session_issuer_id')::uuid
      AND toolset.deleted IS FALSE
);

-- name: DeleteRemoteSessionClientAttachmentsForUserSessionIssuer :exec
DELETE FROM remote_session_client_user_session_issuers AS link
USING user_session_issuers AS usi
WHERE link.user_session_issuer_id = usi.id
  AND usi.id = @user_session_issuer_id
  AND usi.project_id = @project_id;

-- name: SoftDeleteUserSessionsByIssuerID :many
-- Cascading soft-delete of user_sessions for an issuer being soft-deleted.
-- Returns the affected rows so the handler can emit per-row audit events.
UPDATE user_sessions
SET deleted_at = clock_timestamp()
WHERE user_session_issuer_id = @user_session_issuer_id AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteUserSessionConsentsByIssuerID :many
-- Cascading soft-delete of user_session_consents for an issuer being
-- soft-deleted. Joins through user_session_clients since consents are
-- per-client. Project scoping is guaranteed because the parent issuer was
-- already verified to belong to the caller's project.
UPDATE user_session_consents AS c
SET deleted_at = clock_timestamp()
FROM user_session_clients AS cli
WHERE c.user_session_client_id = cli.id
  AND cli.user_session_issuer_id = @user_session_issuer_id
  AND c.deleted IS FALSE
RETURNING c.*;

-- name: GetUserSessionClientByID :one
SELECT cli.*
FROM user_session_clients AS cli
JOIN user_session_issuers AS iss ON iss.id = cli.user_session_issuer_id
WHERE cli.id = @id AND iss.project_id = @project_id AND cli.deleted IS FALSE;

-- name: GetUserSessionClientByClientID :one
-- Lookup a registered DCR client by its issuer-scoped client_id. Used by the
-- /authorize, /token, and /revoke handlers to resolve the client behind the
-- request. Project scoping is intentionally NOT applied here — the OAuth
-- surface is public and the issuer_id is the authoritative scope.
SELECT cli.*
FROM user_session_clients AS cli
WHERE cli.user_session_issuer_id = @user_session_issuer_id
  AND cli.client_id = @client_id
  AND cli.deleted IS FALSE;

-- name: ListUserSessionClientsByProjectID :many
-- Operator visibility into every client registered against an issuer in the
-- project -- DCR-registered and CIMD-resolved alike -- with optional filter by
-- user_session_issuer_id. Joins through issuers for project scoping.
SELECT cli.*
FROM user_session_clients AS cli
JOIN user_session_issuers AS iss ON iss.id = cli.user_session_issuer_id
WHERE iss.project_id = @project_id
  AND cli.deleted IS FALSE
  AND iss.deleted IS FALSE
  AND (sqlc.narg('user_session_issuer_id')::uuid IS NULL OR cli.user_session_issuer_id = sqlc.narg('user_session_issuer_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR cli.id < sqlc.narg('cursor')::uuid)
ORDER BY cli.id DESC
LIMIT sqlc.arg('limit_value');

-- name: CountActiveUserSessionsByClientIDs :many
-- Active-session tallies for a set of clients, so the clients listing can show
-- how many live sessions each registration holds without a round trip per row.
-- "Active" is defined exactly as the 'active' branch of
-- ListUserSessionsByProjectID defines it: not revoked, and keyed off
-- refresh_expires_at (the authorization deadline) rather than expires_at (the
-- ~1h access-token lifetime), so a live connection that has not refreshed
-- recently still counts.
--
-- Project scoping is intentionally NOT applied: callers pass ids they already
-- resolved through a project-scoped client query, and re-joining issuers here
-- would only repeat that check.
--
-- Clients with no active sessions are absent from the result rather than
-- returning zero; callers treat a missing id as zero.
SELECT s.user_session_client_id AS user_session_client_id, COUNT(*)::int AS active_count
FROM user_sessions AS s
WHERE s.user_session_client_id = ANY(@user_session_client_ids::uuid[])
  AND s.deleted IS FALSE
  AND s.refresh_expires_at > now()
GROUP BY s.user_session_client_id;

-- name: RevokeUserSessionClient :one
UPDATE user_session_clients AS cli
SET deleted_at = clock_timestamp()
FROM user_session_issuers AS iss
WHERE cli.id = @id
  AND iss.id = cli.user_session_issuer_id
  AND iss.project_id = @project_id
  AND cli.deleted IS FALSE
RETURNING cli.*;

-- name: SoftDeleteUserSessionsByClientID :many
-- Cascading soft-delete of user_sessions issued through a client being revoked.
-- Returns the affected rows so the handler can emit per-row audit events.
UPDATE user_sessions
SET deleted_at = clock_timestamp()
WHERE user_session_client_id = @user_session_client_id AND deleted IS FALSE
RETURNING *;

-- name: IssuerAdmitsCimdClientURI :one
-- Admission check for a presented CIMD client_id that missed the compile-time
-- preset catalog. Runs on the unauthenticated /authorize surface, so project
-- scoping is intentionally NOT applied — the issuer_id is the authoritative
-- scope, matching GetUserSessionClientByClientID. Served by the partial
-- unique index on (user_session_issuer_id, client_id_metadata_uri).
--
-- Callers MUST bound the length of @client_id_metadata_uri before reaching
-- this query: the value is attacker-supplied and otherwise unbounded.
SELECT EXISTS (
  SELECT 1
  FROM user_session_issuer_cimd_clients AS cimd
  WHERE cimd.user_session_issuer_id = @user_session_issuer_id
    AND cimd.client_id_metadata_uri = @client_id_metadata_uri
    AND cimd.deleted IS FALSE
);

-- name: CreateUserSessionIssuerCimdClient :one
-- Adds an issuer-specific allowed CIMD document URL. The SELECT source
-- scopes the write to a live issuer in the caller's project, so a bad
-- issuer id yields no rows (404) rather than an orphan write. Adding a URL
-- that is already live is idempotent via ON CONFLICT; adding one that was
-- previously soft-deleted inserts a fresh row, since the unique index
-- covers live rows only and the audit trail should show a new grant rather
-- than silently reviving an old one.
--
-- `inserted` distinguishes the two so the caller only records an add event
-- for a real new grant. The DO UPDATE is a deliberate no-op write of the
-- existing updated_at: a genuine touch would misreport a re-add as a
-- modification, but ON CONFLICT still needs an action for RETURNING to
-- yield the row. `xmax = 0` is the standard test for "this row came from
-- the INSERT rather than the UPDATE".
INSERT INTO user_session_issuer_cimd_clients (project_id, user_session_issuer_id, client_id_metadata_uri)
SELECT @project_id, iss.id, @client_id_metadata_uri
FROM user_session_issuers AS iss
WHERE iss.id = @user_session_issuer_id
  AND iss.project_id = @project_id
  AND iss.deleted IS FALSE
ON CONFLICT (user_session_issuer_id, client_id_metadata_uri) WHERE deleted IS FALSE
DO UPDATE SET updated_at = user_session_issuer_cimd_clients.updated_at
RETURNING *, (xmax = 0) AS inserted;

-- name: GetUserSessionIssuerCimdClientByID :one
SELECT cimd.*
FROM user_session_issuer_cimd_clients AS cimd
JOIN user_session_issuers AS iss ON iss.id = cimd.user_session_issuer_id
WHERE cimd.id = @id
  AND iss.project_id = @project_id
  AND cimd.deleted IS FALSE
  AND iss.deleted IS FALSE;

-- name: ListUserSessionIssuerCimdClientsByIssuerID :many
-- Operator visibility into an issuer's custom CIMD URLs. Joins through
-- issuers for project scoping.
SELECT cimd.*
FROM user_session_issuer_cimd_clients AS cimd
JOIN user_session_issuers AS iss ON iss.id = cimd.user_session_issuer_id
WHERE iss.project_id = @project_id
  AND cimd.user_session_issuer_id = @user_session_issuer_id
  AND cimd.deleted IS FALSE
  AND iss.deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR cimd.id < sqlc.narg('cursor')::uuid)
ORDER BY cimd.id DESC
LIMIT sqlc.arg('limit_value');

-- name: DeleteUserSessionIssuerCimdClient :one
-- The issuer must still be live. Soft-deleting an issuer leaves its CIMD
-- rows behind (the FK cascade only fires on a hard delete), and those rows
-- are already inert: admission requires a live issuer, and the list query
-- filters them out. Without this predicate the handler would soft-delete a
-- row and then fail looking up its issuer for the audit event, turning an
-- unreachable resource into a 500.
UPDATE user_session_issuer_cimd_clients AS cimd
SET deleted_at = clock_timestamp()
FROM user_session_issuers AS iss
WHERE cimd.id = @id
  AND iss.id = cimd.user_session_issuer_id
  AND iss.project_id = @project_id
  AND cimd.deleted IS FALSE
  AND iss.deleted IS FALSE
RETURNING cimd.*;

-- name: GetUserSessionConsentByID :one
SELECT c.*, cli.user_session_issuer_id AS user_session_issuer_id
FROM user_session_consents AS c
JOIN user_session_clients AS cli ON cli.id = c.user_session_client_id
JOIN user_session_issuers AS iss ON iss.id = cli.user_session_issuer_id
WHERE c.id = @id AND iss.project_id = @project_id AND c.deleted IS FALSE;

-- name: ListUserSessionConsentsByProjectID :many
SELECT c.*, cli.user_session_issuer_id AS user_session_issuer_id
FROM user_session_consents AS c
JOIN user_session_clients AS cli ON cli.id = c.user_session_client_id
JOIN user_session_issuers AS iss ON iss.id = cli.user_session_issuer_id
WHERE iss.project_id = @project_id
  AND c.deleted IS FALSE
  AND cli.deleted IS FALSE
  AND iss.deleted IS FALSE
  AND (sqlc.narg('subject_urn')::text IS NULL OR c.subject_urn = sqlc.narg('subject_urn')::text)
  AND (sqlc.narg('user_session_client_id')::uuid IS NULL OR c.user_session_client_id = sqlc.narg('user_session_client_id')::uuid)
  AND (sqlc.narg('user_session_issuer_id')::uuid IS NULL OR cli.user_session_issuer_id = sqlc.narg('user_session_issuer_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR c.id < sqlc.narg('cursor')::uuid)
ORDER BY c.id DESC
LIMIT sqlc.arg('limit_value');

-- name: RevokeUserSessionConsent :one
UPDATE user_session_consents AS c
SET deleted_at = clock_timestamp()
FROM user_session_clients AS cli, user_session_issuers AS iss
WHERE c.id = @id
  AND cli.id = c.user_session_client_id
  AND iss.id = cli.user_session_issuer_id
  AND iss.project_id = @project_id
  AND c.deleted IS FALSE
RETURNING c.*, cli.user_session_issuer_id AS user_session_issuer_id;

-- name: GetUserSessionByID :one
-- Returns the session row scoped to the caller's project, joined through
-- user_session_issuers so project scoping is enforced in the same query.
SELECT s.*
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
WHERE s.id = @id AND iss.project_id = @project_id AND s.deleted IS FALSE;

-- name: ListUserSessionsByProjectID :many
-- refresh_token_hash is excluded from the projection so the management API
-- surface cannot accidentally return it.
SELECT s.id, s.user_session_issuer_id, s.user_session_client_id, s.subject_urn, s.jti,
       s.refresh_expires_at, s.expires_at,
       s.created_at, s.updated_at, s.deleted_at, s.deleted,
       iss.slug AS issuer_slug,
       c.client_name AS client_name,
       c.client_id_metadata_uri AS client_id_metadata_uri,
       u.display_name AS user_display_name,
       u.email AS user_email,
       u.photo_url AS user_photo_url,
       k.name AS api_key_name
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
LEFT JOIN user_session_clients AS c ON c.id = s.user_session_client_id
LEFT JOIN users AS u
  ON s.subject_urn::text LIKE 'user:%'
  AND u.id = split_part(s.subject_urn::text, ':', 2)
LEFT JOIN api_keys AS k
  ON k.id = CASE
             WHEN s.subject_urn::text LIKE 'apikey:%'
             THEN split_part(s.subject_urn::text, ':', 2)::uuid
           END
WHERE iss.project_id = @project_id
  AND iss.deleted IS FALSE
  -- "active"/"expired" are keyed off refresh_expires_at (the authorization
  -- deadline), NOT expires_at (the ~1h access-token lifetime). An active MCP
  -- connection only refreshes its access token on demand, so a live session
  -- routinely has a past expires_at while its refresh token is still valid;
  -- keying "active" off expires_at would drop those sessions and make the
  -- Active MCP Connections list flicker between showing them and "No active
  -- sessions" depending on how recently the client last refreshed.
  AND CASE sqlc.narg('status')::text
        WHEN 'active'  THEN (s.deleted IS FALSE AND s.refresh_expires_at > now())
        WHEN 'expired' THEN (s.deleted IS FALSE AND s.refresh_expires_at <= now())
        WHEN 'revoked' THEN (s.deleted IS TRUE)
        WHEN 'all'     THEN TRUE
        ELSE (s.deleted IS FALSE)
      END
  AND (sqlc.narg('subject_urn')::text IS NULL OR s.subject_urn = sqlc.narg('subject_urn')::text)
  AND (sqlc.narg('user_session_issuer_id')::uuid IS NULL OR s.user_session_issuer_id = sqlc.narg('user_session_issuer_id')::uuid)
  AND (sqlc.narg('client_id')::uuid IS NULL OR s.user_session_client_id = sqlc.narg('client_id')::uuid)
  AND (sqlc.narg('id')::uuid IS NULL OR s.id = sqlc.narg('id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR s.id < sqlc.narg('cursor')::uuid)
ORDER BY s.id DESC
LIMIT sqlc.arg('limit_value');

-- name: RevokeUserSession :one
-- Soft-deletes the session. Project scoping is enforced through the join on
-- user_session_issuers. Returns the affected row so the handler can push the
-- jti into the revocation cache and emit an audit event.
UPDATE user_sessions AS s
SET deleted_at = clock_timestamp()
FROM user_session_issuers AS iss
WHERE s.id = @id
  AND iss.id = s.user_session_issuer_id
  AND iss.project_id = @project_id
  AND s.deleted IS FALSE
RETURNING s.*;

-- name: RevokeUserSessionByRefreshTokenHash :one
-- Soft-deletes the session matching the supplied refresh-token hash, scoped
-- to the issuer. Used by the OAuth /revoke endpoint (RFC 7009) on the public
-- MCP surface, where project scoping isn't applicable -- the issuer_id is
-- the authoritative scope. Returns the affected row so the handler can push
-- the jti into the revocation cache.
UPDATE user_sessions
SET deleted_at = clock_timestamp()
WHERE user_session_issuer_id = @user_session_issuer_id
  AND refresh_token_hash = @refresh_token_hash
  AND deleted IS FALSE
RETURNING *;

-- name: GetUserSessionByJTI :one
-- Looks up the session row by jti, scoped to the issuer. Used by the OAuth
-- /revoke endpoint to verify a presented access token belongs to the
-- authenticated client (RFC 7009 §2.1) before pushing the jti into the
-- revocation cache.
SELECT *
FROM user_sessions
WHERE user_session_issuer_id = @user_session_issuer_id
  AND jti = @jti
  AND deleted IS FALSE;

-- name: GetUserSessionByRefreshTokenHash :one
-- Looks up the session row by refresh-token hash, scoped to the issuer.
-- Used by the OAuth /revoke endpoint to verify a presented refresh token
-- belongs to the authenticated client (RFC 7009 §2.1) BEFORE soft-deleting
-- the row — otherwise a malicious client could invalidate another client's
-- refresh token by presenting it to /revoke.
SELECT *
FROM user_sessions
WHERE user_session_issuer_id = @user_session_issuer_id
  AND refresh_token_hash = @refresh_token_hash
  AND deleted IS FALSE;

-- The Create* queries below are exercised by tests and by the OAuth surface
-- that lands in milestone #2 (DCR registration, /token exchange, /authorize
-- consent). They have no exposure on the management API.

-- name: CreateUserSessionClient :one
INSERT INTO user_session_clients (
    project_id,
    user_session_issuer_id,
    client_id,
    client_secret_hash,
    client_name,
    redirect_uris,
    client_secret_expires_at
)
VALUES (
    (SELECT project_id FROM user_session_issuers WHERE id = @user_session_issuer_id),
    @user_session_issuer_id,
    @client_id,
    @client_secret_hash,
    @client_name,
    @redirect_uris,
    @client_secret_expires_at
)
RETURNING *;

-- name: UpsertUserSessionClientFromCIMD :one
-- Lazy upsert for a client resolved from a Client ID Metadata Document at
-- authorize time. For CIMD rows the document URL IS the client_id, so the
-- conflict target is the same partial unique index that serves DCR lookups.
-- On refresh the mutable metadata (client_name, redirect_uris) and every
-- cache column are replaced wholesale, including the ETag, which is set to
-- NULL when the response carried no usable validator so the next refresh is
-- unconditional rather than replaying a stale one.
--
-- The cache expiry is derived from the database clock rather than the
-- application's, so it can never land before the client_id_metadata_fetched_at
-- written in the same statement.
--
-- Two deliberate behaviors:
--   - A soft-deleted row does not conflict (partial index), so revoking a
--     CIMD client does not stick: the next authorize resolves the document
--     again and inserts a fresh row. CIMD identity lives at the URL, and
--     durable blocking is admission control's job, not revocation's.
--   - The DO UPDATE is guarded so it can never touch a secret-bearing DCR
--     row that happens to share the client_id: rewriting it would trip the
--     client_id_metadata_uri CHECK constraints with an opaque 500. The
--     guard makes such a collision surface as no-rows, which handlers
--     already map to invalid_client.
INSERT INTO user_session_clients (
    project_id,
    user_session_issuer_id,
    client_id,
    client_secret_hash,
    client_name,
    redirect_uris,
    client_secret_expires_at,
    client_id_metadata_uri,
    client_id_metadata_fetched_at,
    client_id_metadata_cache_expires_at,
    client_id_metadata_etag
)
VALUES (
    (SELECT project_id FROM user_session_issuers WHERE id = @user_session_issuer_id),
    @user_session_issuer_id,
    @client_id,
    NULL,
    @client_name,
    @redirect_uris,
    NULL,
    @client_id,
    clock_timestamp(),
    clock_timestamp() + make_interval(secs => @cache_ttl_seconds::double precision),
    sqlc.narg('client_id_metadata_etag')
)
ON CONFLICT (user_session_issuer_id, client_id) WHERE deleted IS FALSE
DO UPDATE SET
    client_name = EXCLUDED.client_name,
    redirect_uris = EXCLUDED.redirect_uris,
    client_id_metadata_uri = EXCLUDED.client_id_metadata_uri,
    client_id_metadata_fetched_at = EXCLUDED.client_id_metadata_fetched_at,
    client_id_metadata_cache_expires_at = EXCLUDED.client_id_metadata_cache_expires_at,
    client_id_metadata_etag = EXCLUDED.client_id_metadata_etag,
    updated_at = clock_timestamp()
WHERE user_session_clients.client_secret_hash IS NULL
RETURNING *;

-- name: UpdateUserSessionClientCIMDCache :one
-- Refreshes the cache bookkeeping on a CIMD-resolved client whose document
-- host answered 304 Not Modified. The stored client_name and redirect_uris
-- are current by definition of the 304, so they are deliberately untouched;
-- only the fetch stamp, the expiry, and the validator move.
--
-- The guards mirror UpsertUserSessionClientFromCIMD's. A secret-bearing DCR
-- row, or a row that is not CIMD-resolved at all, is never written, so this
-- statement cannot push a row into violating the client_id_metadata_uri
-- CHECK constraints; such a collision surfaces as no-rows, which handlers
-- already map to invalid_client. Project scoping is intentionally absent for
-- the same reason as GetUserSessionClientByClientID: the OAuth surface is
-- public, and the id comes from a row the caller already resolved through
-- the issuer.
UPDATE user_session_clients
SET client_id_metadata_fetched_at = clock_timestamp(),
    client_id_metadata_cache_expires_at = clock_timestamp() + make_interval(secs => @cache_ttl_seconds::double precision),
    client_id_metadata_etag = sqlc.narg('client_id_metadata_etag'),
    updated_at = clock_timestamp()
WHERE id = @id
  AND client_id_metadata_uri IS NOT NULL
  AND client_secret_hash IS NULL
  AND deleted IS FALSE
RETURNING *;

-- name: PurgeUserSessionClientCIMDCache :one
-- Forces the next authorize to re-read, re-parse, and re-validate a CIMD
-- client's metadata document instead of serving the stored copy.
--
-- This is the purge lever for the cache: a document whose contents must stop
-- being honoured before its TTL lapses — a compromised or mistakenly
-- published redirect_uris set, a validation rule tightened after the row was
-- written — is dealt with by running this and letting the next authorize
-- refetch. The validator is cleared along with the expiry on purpose: leaving
-- it would make the refresh conditional, and a 304 would confirm the very
-- document being purged without the body ever being re-validated.
--
-- Revoking the client purges its cache as a side effect, since the lookup
-- behind every authorize filters on deleted IS FALSE and a miss forces an
-- unconditional fetch. This query exists for the case where the client should
-- keep working and only its stored document is suspect. It has no endpoint
-- yet and is run by hand; AIS-211 wires it to a per-client refresh action.
UPDATE user_session_clients
SET client_id_metadata_cache_expires_at = NULL,
    client_id_metadata_etag = NULL,
    updated_at = clock_timestamp()
WHERE id = @id
  AND client_id_metadata_uri IS NOT NULL
  AND deleted IS FALSE
RETURNING *;

-- name: CreateUserSession :one
-- user_session_client_id binds the session to the DCR client that minted it.
-- The /token refresh path requires the same client to refresh; see
-- HandleToken's refresh_token grant.
INSERT INTO user_sessions (
    project_id,
    user_session_issuer_id,
    user_session_client_id,
    subject_urn,
    jti,
    refresh_token_hash,
    refresh_expires_at,
    expires_at
)
VALUES (
    (SELECT project_id FROM user_session_issuers WHERE id = @user_session_issuer_id),
    @user_session_issuer_id,
    @user_session_client_id,
    @subject_urn,
    @jti,
    @refresh_token_hash,
    @refresh_expires_at,
    @expires_at
)
RETURNING *;

-- name: CreateUserSessionConsent :one
INSERT INTO user_session_consents (
    project_id,
    subject_urn,
    user_session_client_id,
    remote_set_hash
)
VALUES (
    (SELECT project_id FROM user_session_clients WHERE id = @user_session_client_id),
    @subject_urn,
    @user_session_client_id,
    @remote_set_hash
)
RETURNING *;

-- name: ListUserSessionServerFacets :many
SELECT s.user_session_issuer_id::text AS value, iss.slug AS display_name, COUNT(*)::bigint AS count
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
WHERE iss.project_id = @project_id AND iss.deleted IS FALSE AND s.deleted IS FALSE
GROUP BY s.user_session_issuer_id, iss.slug
ORDER BY count DESC, iss.slug ASC;

-- name: ListUserSessionClientFacets :many
SELECT c.id::text AS value, c.client_name AS display_name, COUNT(*)::bigint AS count
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
JOIN user_session_clients AS c ON c.id = s.user_session_client_id
WHERE iss.project_id = @project_id AND iss.deleted IS FALSE AND c.deleted IS FALSE AND s.deleted IS FALSE
GROUP BY c.id, c.client_name
ORDER BY count DESC, c.client_name ASC;

-- name: ListUserSessionUserFacets :many
SELECT s.subject_urn::text AS value,
       COALESCE(u.display_name, u.email, s.subject_urn::text) AS display_name,
       COUNT(*)::bigint AS count
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
LEFT JOIN users AS u ON u.id = split_part(s.subject_urn::text, ':', 2)
WHERE iss.project_id = @project_id AND iss.deleted IS FALSE AND s.deleted IS FALSE
  AND s.subject_urn::text LIKE 'user:%'
GROUP BY s.subject_urn, u.display_name, u.email
ORDER BY count DESC, display_name ASC;
