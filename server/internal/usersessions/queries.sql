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

-- name: CreateDefaultUserSessionIssuer :one
-- Insert half of the get-or-create backing the implicit project-default
-- issuer (see GetOrCreateDefaultIssuer). ON CONFLICT DO NOTHING makes
-- concurrent first-touch callers race-safe; the conflict case returns no
-- row (pgx.ErrNoRows) and the caller re-reads by slug.
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
ON CONFLICT (project_id, slug) WHERE deleted IS FALSE DO NOTHING
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
SELECT cli.*, iss.project_id AS issuer_project_id
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
-- Operator visibility into all DCR-issued clients in the project, with optional
-- filter by user_session_issuer_id. Joins through issuers for project scoping.
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
       u.display_name AS user_display_name,
       u.email AS user_email,
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
  -- "active"/"expired" are keyed off refresh_expires_at (the session/refresh
  -- lifetime), NOT expires_at (the ~1h access-token lifetime). An active MCP
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
