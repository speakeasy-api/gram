-- Remote session issuers — upstream Authorization Server identity records
-- that Gram talks to as an OAuth client.

-- name: CreateRemoteSessionIssuer :one
INSERT INTO remote_session_issuers (
    project_id,
    slug,
    issuer,
    authorization_endpoint,
    token_endpoint,
    registration_endpoint,
    jwks_uri,
    scopes_supported,
    grant_types_supported,
    response_types_supported,
    token_endpoint_auth_methods_supported,
    oidc,
    passthrough
)
VALUES (
    @project_id,
    @slug,
    @issuer,
    @authorization_endpoint,
    @token_endpoint,
    @registration_endpoint,
    @jwks_uri,
    @scopes_supported,
    @grant_types_supported,
    @response_types_supported,
    @token_endpoint_auth_methods_supported,
    @oidc,
    @passthrough
)
RETURNING *;

-- name: GetRemoteSessionIssuerByID :one
SELECT *
FROM remote_session_issuers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetRemoteSessionIssuerBySlug :one
SELECT *
FROM remote_session_issuers
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: ListRemoteSessionIssuersByProjectID :many
SELECT *
FROM remote_session_issuers
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
ORDER BY id DESC
LIMIT sqlc.arg('limit_value');

-- name: UpdateRemoteSessionIssuer :one
UPDATE remote_session_issuers
SET
    slug = COALESCE(sqlc.narg('slug'), slug),
    issuer = COALESCE(sqlc.narg('issuer'), issuer),
    authorization_endpoint = COALESCE(sqlc.narg('authorization_endpoint'), authorization_endpoint),
    token_endpoint = COALESCE(sqlc.narg('token_endpoint'), token_endpoint),
    registration_endpoint = COALESCE(sqlc.narg('registration_endpoint'), registration_endpoint),
    jwks_uri = COALESCE(sqlc.narg('jwks_uri'), jwks_uri),
    scopes_supported = COALESCE(sqlc.narg('scopes_supported')::text[], scopes_supported),
    grant_types_supported = COALESCE(sqlc.narg('grant_types_supported')::text[], grant_types_supported),
    response_types_supported = COALESCE(sqlc.narg('response_types_supported')::text[], response_types_supported),
    token_endpoint_auth_methods_supported = COALESCE(sqlc.narg('token_endpoint_auth_methods_supported')::text[], token_endpoint_auth_methods_supported),
    oidc = COALESCE(sqlc.narg('oidc'), oidc),
    passthrough = COALESCE(sqlc.narg('passthrough'), passthrough),
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteRemoteSessionIssuer :one
UPDATE remote_session_issuers
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: CountRemoteSessionClientsByIssuerID :one
SELECT COUNT(*)
FROM remote_session_clients
WHERE remote_session_issuer_id = @remote_session_issuer_id AND deleted IS FALSE;

-- Remote session clients — credentials Gram uses when acting as an OAuth
-- client of a remote_session_issuer. client_secret_encrypted is stored
-- encrypted via the project encryption key.

-- name: CreateRemoteSessionClient :one
INSERT INTO remote_session_clients (
    project_id,
    remote_session_issuer_id,
    user_session_issuer_id,
    client_id,
    client_secret_encrypted,
    client_id_issued_at,
    client_secret_expires_at
)
VALUES (
    @project_id,
    @remote_session_issuer_id,
    @user_session_issuer_id,
    @client_id,
    @client_secret_encrypted,
    @client_id_issued_at,
    @client_secret_expires_at
)
RETURNING *;

-- name: GetRemoteSessionClientByID :one
SELECT *
FROM remote_session_clients
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ListRemoteSessionClientsByProjectID :many
SELECT *
FROM remote_session_clients
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg('remote_session_issuer_id')::uuid IS NULL OR remote_session_issuer_id = sqlc.narg('remote_session_issuer_id')::uuid)
  AND (sqlc.narg('user_session_issuer_id')::uuid IS NULL OR user_session_issuer_id = sqlc.narg('user_session_issuer_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
ORDER BY id DESC
LIMIT sqlc.arg('limit_value');

-- name: UpdateRemoteSessionClient :one
UPDATE remote_session_clients
SET
    client_secret_encrypted = COALESCE(sqlc.narg('client_secret_encrypted'), client_secret_encrypted),
    client_secret_expires_at = COALESCE(sqlc.narg('client_secret_expires_at'), client_secret_expires_at),
    user_session_issuer_id = COALESCE(sqlc.narg('user_session_issuer_id'), user_session_issuer_id),
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteRemoteSessionClient :one
UPDATE remote_session_clients
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteRemoteSessionsByClientID :execrows
UPDATE remote_sessions
SET deleted_at = clock_timestamp()
WHERE remote_session_client_id = @remote_session_client_id AND deleted IS FALSE;

-- name: CountActiveRemoteSessionsByClientID :one
SELECT COUNT(*)
FROM remote_sessions
WHERE remote_session_client_id = @remote_session_client_id AND deleted IS FALSE;

-- name: InsertRemoteSession :one
INSERT INTO remote_sessions (
    subject_urn,
    user_session_issuer_id,
    remote_session_client_id,
    access_token_encrypted,
    access_expires_at
)
VALUES (
    @subject_urn,
    @user_session_issuer_id,
    @remote_session_client_id,
    @access_token_encrypted,
    @access_expires_at
)
RETURNING *;

-- name: UpsertRemoteSession :one
-- Used by /mcp/remote_login_callback to materialise (or refresh) the
-- remote_session for a (subject, client) pair. Conflict target matches the
-- partial unique index on (subject_urn, remote_session_client_id) WHERE
-- deleted IS FALSE; on conflict we overwrite every token field. A
-- soft-deleted row falls outside the partial index, so a re-auth after
-- revocation inserts a fresh active row alongside the tombstone.
INSERT INTO remote_sessions (
    subject_urn,
    user_session_issuer_id,
    remote_session_client_id,
    access_token_encrypted,
    access_expires_at,
    refresh_token_encrypted,
    refresh_expires_at,
    scopes
)
VALUES (
    @subject_urn,
    @user_session_issuer_id,
    @remote_session_client_id,
    @access_token_encrypted,
    @access_expires_at,
    @refresh_token_encrypted,
    @refresh_expires_at,
    @scopes
)
ON CONFLICT (subject_urn, remote_session_client_id) WHERE deleted IS FALSE
DO UPDATE SET
    access_token_encrypted = EXCLUDED.access_token_encrypted,
    access_expires_at = EXCLUDED.access_expires_at,
    refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
    refresh_expires_at = EXCLUDED.refresh_expires_at,
    scopes = EXCLUDED.scopes,
    updated_at = clock_timestamp()
RETURNING *;

-- name: GetActiveRemoteSession :one
-- Look up the active remote_session for a (subject, client) binding.
-- Single-row exact lookup; uniqueness enforced by the partial unique
-- index on (subject_urn, remote_session_client_id) WHERE deleted IS FALSE.
SELECT *
FROM remote_sessions
WHERE subject_urn = @subject_urn
  AND remote_session_client_id = @remote_session_client_id
  AND deleted IS FALSE;

-- name: ListConnectedClientIDsForSubject :many
-- Bulk lookup for the consent renderer: returns the set of
-- remote_session_client_ids that have an active remote_sessions row for
-- the given subject under a single user_session_issuer. Folds the N
-- per-card IsConnected lookups into one round-trip. The partial unique
-- index on (subject_urn, remote_session_client_id) WHERE deleted IS
-- FALSE means at most one row per (subject, client), so the result set
-- doubles as a membership set without DISTINCT.
SELECT remote_session_client_id
FROM remote_sessions
WHERE subject_urn = @subject_urn
  AND user_session_issuer_id = @user_session_issuer_id
  AND deleted IS FALSE;

-- name: GetRemoteSessionClientWithIssuerByID :one
-- Joined client + issuer view scoped to a single client_id. Used by
-- the runtime token resolver to find the upstream token endpoint when
-- refreshing an expired access token. Callers establish that the
-- client belongs to the request's project upstream
-- (ListRemoteSessionClientsForUserSessionIssuer); this lookup itself
-- needs only the id since client ids are globally unique.
SELECT
    c.id                                   AS client_id,
    c.client_id                            AS external_client_id,
    c.client_secret_encrypted              AS client_secret_encrypted,
    c.remote_session_issuer_id             AS remote_session_issuer_id,
    c.user_session_issuer_id               AS user_session_issuer_id,
    i.slug                                 AS issuer_slug,
    i.issuer                               AS issuer_url,
    i.authorization_endpoint               AS authorization_endpoint,
    i.token_endpoint                       AS token_endpoint,
    i.scopes_supported                     AS scopes_supported,
    i.passthrough                          AS passthrough,
    i.oidc                                 AS oidc
FROM remote_session_clients AS c
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id
WHERE c.id = @id
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE;

-- name: ListRemoteSessionClientsForUserSessionIssuer :many
-- Joined client + issuer view used by the consent renderer and the
-- ChallengeManager. Returns one row per remote_session_client linked to
-- the given user_session_issuer.
SELECT
    c.id                                   AS client_id,
    c.client_id                            AS external_client_id,
    c.client_secret_encrypted              AS client_secret_encrypted,
    c.remote_session_issuer_id             AS remote_session_issuer_id,
    c.user_session_issuer_id               AS user_session_issuer_id,
    i.slug                                 AS issuer_slug,
    i.issuer                               AS issuer_url,
    i.authorization_endpoint               AS authorization_endpoint,
    i.token_endpoint                       AS token_endpoint,
    i.scopes_supported                     AS scopes_supported,
    i.passthrough                          AS passthrough,
    i.oidc                                 AS oidc
FROM remote_session_clients AS c
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id
WHERE c.user_session_issuer_id = @user_session_issuer_id
  AND c.project_id              = @project_id
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE
ORDER BY c.id ASC;

-- name: ListRemoteSessionsByProjectID :many
SELECT s.*
FROM remote_sessions AS s
JOIN remote_session_clients AS c ON c.id = s.remote_session_client_id
WHERE c.project_id = @project_id
  AND s.deleted IS FALSE
  AND c.deleted IS FALSE
  AND (sqlc.narg('subject_urn')::text IS NULL OR s.subject_urn = sqlc.narg('subject_urn')::text)
  AND (sqlc.narg('remote_session_client_id')::uuid IS NULL OR s.remote_session_client_id = sqlc.narg('remote_session_client_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR s.id < sqlc.narg('cursor')::uuid)
ORDER BY s.id DESC
LIMIT sqlc.arg('limit_value');

-- name: GetRemoteSessionByID :one
SELECT s.*
FROM remote_sessions AS s
JOIN remote_session_clients AS c ON c.id = s.remote_session_client_id
WHERE s.id = @id AND c.project_id = @project_id AND s.deleted IS FALSE AND c.deleted IS FALSE;

-- name: RevokeRemoteSession :one
UPDATE remote_sessions AS s
SET deleted_at = clock_timestamp()
FROM remote_session_clients AS c
WHERE s.id = @id
  AND s.remote_session_client_id = c.id
  AND c.project_id = @project_id
  AND s.deleted IS FALSE
  AND c.deleted IS FALSE
RETURNING s.*;
