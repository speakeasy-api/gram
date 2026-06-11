-- Remote session issuers — upstream Authorization Server identity records
-- that Gram talks to as an OAuth client.

-- name: CreateRemoteSessionIssuer :one
-- Serves both creation paths: a project-level issuer passes a valid project_id
-- plus its organization_id; an organization-level (cross-project) issuer passes
-- a NULL project_id plus organization_id.
INSERT INTO remote_session_issuers (
    project_id,
    organization_id,
    slug,
    issuer,
    name,
    logo_asset_id,
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
    @organization_id,
    @slug,
    @issuer,
    @name,
    @logo_asset_id,
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
-- Project-scoped read that also resolves organization-level issuers belonging
-- to the project's org, so projects inherit cross-project issuers.
SELECT *
FROM remote_session_issuers
WHERE id = @id
  AND (project_id = @project_id OR (project_id IS NULL AND organization_id = @organization_id))
  AND deleted IS FALSE;

-- name: GetRemoteSessionIssuerBySlug :one
-- Slug lookups are strictly project-scoped: the (project_id, slug) unique index
-- makes this deterministic. Organization-level issuers are not slug-addressable
-- (their slugs are not uniqueness-constrained); fetch them by id instead.
SELECT *
FROM remote_session_issuers
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: ListRemoteSessionIssuersByProjectID :many
-- Lists the project's own issuers plus organization-level issuers inherited
-- from the project's org.
SELECT *
FROM remote_session_issuers
WHERE (project_id = @project_id OR (project_id IS NULL AND organization_id = @organization_id))
  AND deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
ORDER BY id DESC
LIMIT sqlc.arg('limit_value');

-- name: UpdateRemoteSessionIssuer :one
-- Three-state semantics on the nullable endpoint columns: an omitted narg
-- (NULL) keeps the existing value, an explicit empty string clears the
-- column to NULL, any other value sets it. Operators need the clear path
-- to disable DCR or remove stale discovery results on already-saved
-- issuers. slug and issuer are NOT NULL; the handler rejects an explicit
-- empty for those before reaching this query.
UPDATE remote_session_issuers
SET
    slug = COALESCE(sqlc.narg('slug'), slug),
    issuer = COALESCE(sqlc.narg('issuer'), issuer),
    name = CASE
        WHEN sqlc.narg('name')::text = '' THEN NULL
        ELSE COALESCE(sqlc.narg('name'), name)
    END,
    logo_asset_id = COALESCE(sqlc.narg('logo_asset_id'), logo_asset_id),
    authorization_endpoint = CASE
        WHEN sqlc.narg('authorization_endpoint')::text = '' THEN NULL
        ELSE COALESCE(sqlc.narg('authorization_endpoint'), authorization_endpoint)
    END,
    token_endpoint = CASE
        WHEN sqlc.narg('token_endpoint')::text = '' THEN NULL
        ELSE COALESCE(sqlc.narg('token_endpoint'), token_endpoint)
    END,
    registration_endpoint = CASE
        WHEN sqlc.narg('registration_endpoint')::text = '' THEN NULL
        ELSE COALESCE(sqlc.narg('registration_endpoint'), registration_endpoint)
    END,
    jwks_uri = CASE
        WHEN sqlc.narg('jwks_uri')::text = '' THEN NULL
        ELSE COALESCE(sqlc.narg('jwks_uri'), jwks_uri)
    END,
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

-- Organization-level remote session issuers — cross-project issuers scoped to
-- an organization (project_id IS NULL). Managed via the
-- organizationRemoteSessionIssuers service and accessed by id.

-- name: GetOrganizationRemoteSessionIssuerByID :one
SELECT *
FROM remote_session_issuers
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE;

-- name: ListOrganizationRemoteSessionIssuers :many
SELECT *
FROM remote_session_issuers
WHERE organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
ORDER BY id DESC
LIMIT sqlc.arg('limit_value');

-- name: UpdateOrganizationRemoteSessionIssuer :one
-- Same three-state narg semantics on the nullable endpoint columns as
-- UpdateRemoteSessionIssuer; scoped to organization-level rows (project_id IS NULL).
UPDATE remote_session_issuers
SET
    slug = COALESCE(sqlc.narg('slug'), slug),
    issuer = COALESCE(sqlc.narg('issuer'), issuer),
    name = CASE
        WHEN sqlc.narg('name')::text = '' THEN NULL
        ELSE COALESCE(sqlc.narg('name'), name)
    END,
    logo_asset_id = COALESCE(sqlc.narg('logo_asset_id'), logo_asset_id),
    authorization_endpoint = CASE
        WHEN sqlc.narg('authorization_endpoint')::text = '' THEN NULL
        ELSE COALESCE(sqlc.narg('authorization_endpoint'), authorization_endpoint)
    END,
    token_endpoint = CASE
        WHEN sqlc.narg('token_endpoint')::text = '' THEN NULL
        ELSE COALESCE(sqlc.narg('token_endpoint'), token_endpoint)
    END,
    registration_endpoint = CASE
        WHEN sqlc.narg('registration_endpoint')::text = '' THEN NULL
        ELSE COALESCE(sqlc.narg('registration_endpoint'), registration_endpoint)
    END,
    jwks_uri = CASE
        WHEN sqlc.narg('jwks_uri')::text = '' THEN NULL
        ELSE COALESCE(sqlc.narg('jwks_uri'), jwks_uri)
    END,
    scopes_supported = COALESCE(sqlc.narg('scopes_supported')::text[], scopes_supported),
    grant_types_supported = COALESCE(sqlc.narg('grant_types_supported')::text[], grant_types_supported),
    response_types_supported = COALESCE(sqlc.narg('response_types_supported')::text[], response_types_supported),
    token_endpoint_auth_methods_supported = COALESCE(sqlc.narg('token_endpoint_auth_methods_supported')::text[], token_endpoint_auth_methods_supported),
    oidc = COALESCE(sqlc.narg('oidc'), oidc),
    passthrough = COALESCE(sqlc.narg('passthrough'), passthrough),
    updated_at = clock_timestamp()
WHERE id = @id AND organization_id = @organization_id AND project_id IS NULL AND deleted IS FALSE
RETURNING *;

-- name: DeleteOrganizationRemoteSessionIssuer :one
UPDATE remote_session_issuers
SET deleted_at = clock_timestamp()
WHERE id = @id AND organization_id = @organization_id AND project_id IS NULL AND deleted IS FALSE
RETURNING *;

-- name: CountRemoteSessionClientsByIssuerID :one
SELECT COUNT(*)
FROM remote_session_clients
WHERE remote_session_issuer_id = @remote_session_issuer_id AND deleted IS FALSE;

-- Remote session clients — credentials Gram uses when acting as an OAuth
-- client of a remote_session_issuer. client_secret_encrypted is stored
-- encrypted via the project encryption key.

-- name: GetOAuthProxyProviderForClone :one
-- Read just the fields cloneOAuthProxyProvider needs: project scoping for
-- isolation, provider_type to refuse non-custom providers, and the secrets
-- JSONB so the handler can extract client_id / client_secret server-side.
SELECT id, project_id, provider_type, secrets
FROM oauth_proxy_providers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: CreateRemoteSessionClient :one
INSERT INTO remote_session_clients (
    project_id,
    remote_session_issuer_id,
    user_session_issuer_id,
    client_id,
    client_secret_encrypted,
    client_id_issued_at,
    client_secret_expires_at,
    token_endpoint_auth_method,
    scope,
    audience,
    legacy_callback_url
)
VALUES (
    @project_id,
    @remote_session_issuer_id,
    @user_session_issuer_id,
    @client_id,
    @client_secret_encrypted,
    @client_id_issued_at,
    @client_secret_expires_at,
    @token_endpoint_auth_method,
    sqlc.narg('scope')::text[],
    @audience,
    @legacy_callback_url
)
RETURNING *;

-- name: AttachRemoteSessionClientToUserSessionIssuer :exec
INSERT INTO remote_session_client_user_session_issuers (
    remote_session_client_id,
    user_session_issuer_id
) VALUES (
    @remote_session_client_id,
    @user_session_issuer_id
)
ON CONFLICT (remote_session_client_id, user_session_issuer_id) DO NOTHING;

-- name: CountRemoteSessionClientUserSessionIssuerBindings :one
SELECT COUNT(remote_session_client_id)
FROM remote_session_client_user_session_issuers
WHERE remote_session_client_id = @remote_session_client_id
  AND user_session_issuer_id = @user_session_issuer_id;

-- name: DeleteUserSessionIssuerAttachmentsForRemoteSessionClient :exec
DELETE FROM remote_session_client_user_session_issuers AS link
USING remote_session_clients AS c
WHERE link.remote_session_client_id = c.id
  AND c.id = @remote_session_client_id
  AND c.project_id = @project_id;

-- name: GetRemoteSessionClientByID :one
SELECT
    id,
    project_id,
    remote_session_issuer_id,
    user_session_issuer_id,
    client_id,
    client_secret_encrypted,
    client_id_issued_at,
    client_secret_expires_at,
    token_endpoint_auth_method,
    scope,
    audience,
    legacy_callback_url,
    created_at,
    updated_at,
    deleted_at,
    deleted
FROM remote_session_clients
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetUserSessionIssuerForProject :one
SELECT id
FROM user_session_issuers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ListRemoteSessionClientsByProjectID :many
SELECT
    id,
    project_id,
    remote_session_issuer_id,
    user_session_issuer_id,
    client_id,
    client_secret_encrypted,
    client_id_issued_at,
    client_secret_expires_at,
    token_endpoint_auth_method,
    scope,
    audience,
    legacy_callback_url,
    created_at,
    updated_at,
    deleted_at,
    deleted
FROM remote_session_clients
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg('remote_session_issuer_id')::uuid IS NULL OR remote_session_issuer_id = sqlc.narg('remote_session_issuer_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
ORDER BY id DESC
LIMIT sqlc.arg('limit_value');

-- name: ListRemoteSessionClientsByProjectIDForUserSessionIssuer :many
SELECT
    c.id,
    c.project_id,
    c.remote_session_issuer_id,
    c.user_session_issuer_id,
    c.client_id,
    c.client_secret_encrypted,
    c.client_id_issued_at,
    c.client_secret_expires_at,
    c.token_endpoint_auth_method,
    c.scope,
    c.audience,
    c.legacy_callback_url,
    c.created_at,
    c.updated_at,
    c.deleted_at,
    c.deleted
FROM remote_session_client_user_session_issuers AS link
JOIN remote_session_clients AS c ON c.id = link.remote_session_client_id
JOIN user_session_issuers AS usi ON usi.id = link.user_session_issuer_id
WHERE link.user_session_issuer_id = @user_session_issuer_id
  AND usi.project_id = @project_id
  AND usi.deleted IS FALSE
  AND c.project_id = @project_id
  AND c.deleted IS FALSE
  AND (sqlc.narg('remote_session_issuer_id')::uuid IS NULL OR c.remote_session_issuer_id = sqlc.narg('remote_session_issuer_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR c.id < sqlc.narg('cursor')::uuid)
ORDER BY c.id DESC
LIMIT sqlc.arg('limit_value');

-- name: ListRemoteSessionClientsByProjectIDForUserSessionIssuerLegacy :many
SELECT
    c.id,
    c.project_id,
    c.remote_session_issuer_id,
    c.user_session_issuer_id,
    c.client_id,
    c.client_secret_encrypted,
    c.client_id_issued_at,
    c.client_secret_expires_at,
    c.token_endpoint_auth_method,
    c.scope,
    c.audience,
    c.legacy_callback_url,
    c.created_at,
    c.updated_at,
    c.deleted_at,
    c.deleted
FROM remote_session_clients AS c
JOIN user_session_issuers AS usi ON usi.id = c.user_session_issuer_id
WHERE c.user_session_issuer_id = @user_session_issuer_id
  AND usi.project_id = @project_id
  AND usi.deleted IS FALSE
  AND c.project_id = @project_id
  AND c.deleted IS FALSE
  AND (sqlc.narg('remote_session_issuer_id')::uuid IS NULL OR c.remote_session_issuer_id = sqlc.narg('remote_session_issuer_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR c.id < sqlc.narg('cursor')::uuid)
ORDER BY c.id DESC
LIMIT sqlc.arg('limit_value');

-- name: CountLegacyRemoteSessionClientsForUserSessionIssuer :one
SELECT COUNT(c.id)
FROM remote_session_clients AS c
JOIN user_session_issuers AS usi ON usi.id = c.user_session_issuer_id
WHERE c.user_session_issuer_id = @user_session_issuer_id
  AND usi.project_id = @project_id
  AND usi.deleted IS FALSE
  AND c.project_id = @project_id
  AND c.deleted IS FALSE;

-- name: UpdateRemoteSessionClient :one
UPDATE remote_session_clients
SET
    client_secret_encrypted = COALESCE(sqlc.narg('client_secret_encrypted'), client_secret_encrypted),
    client_secret_expires_at = COALESCE(sqlc.narg('client_secret_expires_at'), client_secret_expires_at),
    user_session_issuer_id = COALESCE(sqlc.narg('user_session_issuer_id'), user_session_issuer_id),
    token_endpoint_auth_method = COALESCE(sqlc.narg('token_endpoint_auth_method'), token_endpoint_auth_method),
    scope = COALESCE(sqlc.narg('scope')::text[], scope),
    audience = COALESCE(sqlc.narg('audience'), audience),
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

-- name: ListRemoteSessionStatusesForSubject :many
-- Bulk lookup for the consent renderer: returns each non-deleted
-- remote_session for the given subject under a single user_session_issuer,
-- tagged with whether it is still usable. Folds the N per-card lookups into
-- one round-trip. The partial unique index on (subject_urn,
-- remote_session_client_id) WHERE deleted IS FALSE means at most one row per
-- (subject, client), so the result doubles as a per-client map without
-- DISTINCT. A soft-deleted row is absent here entirely (truly disconnected).
--
-- The 'active' predicate mirrors validateAndRefresh in tokenservice.go: a
-- session is usable only when its access token is unexpired, or it carries a
-- refresh token that is not itself known-expired to renew with. A
-- refresh_expires_at of NULL means no known expiry (non-expiring refresh
-- token), so it still counts as usable. A present-but-unusable row is
-- 'expired' rather than dropped, so the consent UI can distinguish "reconnect
-- this expired link" from "never connected" — and so the runtime gate (which
-- rejects the same row as ErrNoValidToken) stops disagreeing with a green
-- "Connected" badge.
SELECT
  remote_session_client_id,
  (CASE
    WHEN access_expires_at > now()
      OR (refresh_token_encrypted IS NOT NULL
          AND (refresh_expires_at IS NULL OR refresh_expires_at > now())) THEN 'active'
    ELSE 'expired'
  END)::text AS status
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
    c.token_endpoint_auth_method           AS token_endpoint_auth_method,
    c.scope                                AS client_scope,
    c.audience                             AS client_audience,
    c.legacy_callback_url                  AS legacy_callback_url,
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
-- the given user_session_issuer through the join table.
SELECT
    c.id                                   AS client_id,
    c.client_id                            AS external_client_id,
    c.client_secret_encrypted              AS client_secret_encrypted,
    c.token_endpoint_auth_method           AS token_endpoint_auth_method,
    c.scope                                AS client_scope,
    c.audience                             AS client_audience,
    c.legacy_callback_url                  AS legacy_callback_url,
    c.remote_session_issuer_id             AS remote_session_issuer_id,
    c.user_session_issuer_id               AS user_session_issuer_id,
    i.slug                                 AS issuer_slug,
    i.issuer                               AS issuer_url,
    i.authorization_endpoint               AS authorization_endpoint,
    i.token_endpoint                       AS token_endpoint,
    i.scopes_supported                     AS scopes_supported,
    i.passthrough                          AS passthrough,
    i.oidc                                 AS oidc
FROM remote_session_client_user_session_issuers AS link
JOIN remote_session_clients AS c ON c.id = link.remote_session_client_id
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id
JOIN user_session_issuers AS usi ON usi.id = link.user_session_issuer_id
WHERE link.user_session_issuer_id = @user_session_issuer_id
  AND c.project_id = @project_id
  AND usi.project_id = @project_id
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE
  AND usi.deleted IS FALSE
ORDER BY c.id ASC;

-- name: ListRemoteSessionClientsForUserSessionIssuerLegacy :many
-- Legacy-column fallback used during AGE-2520 while untouched rows may not
-- have a join-table binding yet.
SELECT
    c.id                                   AS client_id,
    c.client_id                            AS external_client_id,
    c.client_secret_encrypted              AS client_secret_encrypted,
    c.token_endpoint_auth_method           AS token_endpoint_auth_method,
    c.scope                                AS client_scope,
    c.audience                             AS client_audience,
    c.legacy_callback_url                  AS legacy_callback_url,
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
JOIN user_session_issuers AS usi ON usi.id = c.user_session_issuer_id
WHERE c.user_session_issuer_id = @user_session_issuer_id
  AND c.project_id = @project_id
  AND usi.project_id = @project_id
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE
  AND usi.deleted IS FALSE
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
