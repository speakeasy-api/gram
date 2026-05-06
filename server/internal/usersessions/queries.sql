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
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteUserSessionIssuer :one
UPDATE user_session_issuers
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

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

-- The Create* queries below are exercised by tests and by the OAuth surface
-- that lands in milestone #2 (DCR registration, /token exchange, /authorize
-- consent). They have no exposure on the management API.

-- name: CreateUserSessionClient :one
INSERT INTO user_session_clients (
    user_session_issuer_id,
    client_id,
    client_secret_hash,
    client_name,
    redirect_uris,
    client_secret_expires_at
)
VALUES (
    @user_session_issuer_id,
    @client_id,
    @client_secret_hash,
    @client_name,
    @redirect_uris,
    @client_secret_expires_at
)
RETURNING *;

-- name: CreateUserSession :one
INSERT INTO user_sessions (
    user_session_issuer_id,
    subject_urn,
    jti,
    refresh_token_hash,
    refresh_expires_at,
    expires_at
)
VALUES (
    @user_session_issuer_id,
    @subject_urn,
    @jti,
    @refresh_token_hash,
    @refresh_expires_at,
    @expires_at
)
RETURNING *;

-- name: CreateUserSessionConsent :one
INSERT INTO user_session_consents (
    subject_urn,
    user_session_client_id,
    remote_set_hash
)
VALUES (
    @subject_urn,
    @user_session_client_id,
    @remote_set_hash
)
RETURNING *;
