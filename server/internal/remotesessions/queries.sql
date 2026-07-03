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
    client_id_metadata_document_supported,
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
    @client_id_metadata_document_supported,
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
    client_id_metadata_document_supported = COALESCE(sqlc.narg('client_id_metadata_document_supported'), client_id_metadata_document_supported),
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

-- name: GetOAuthProxyProviderForClone :one
-- Read just the fields cloneOAuthProxyProvider needs: project scoping for
-- isolation, provider_type to refuse non-custom providers, the secrets
-- JSONB so the handler can extract client_id / client_secret server-side,
-- and oauth_proxy_server_id so the handler can find the MCP servers whose
-- legacy client registrations need migrating.
SELECT id, project_id, provider_type, secrets, oauth_proxy_server_id
FROM oauth_proxy_providers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ListToolsetMCPEndpointsForOAuthProxyServer :many
-- Finds every MCP server attached to an oauth_proxy_server so the clone
-- handler can derive the public URLs legacy client registrations were keyed
-- under. A toolset with a custom domain is reachable on both the default
-- domain and the custom domain, so the handler scans both variants.
SELECT t.mcp_slug, cd.domain AS custom_domain
FROM toolsets AS t
LEFT JOIN custom_domains AS cd ON cd.id = t.custom_domain_id AND cd.deleted IS FALSE
WHERE t.oauth_proxy_server_id = @oauth_proxy_server_id
  AND t.project_id = @project_id
  AND t.mcp_slug IS NOT NULL
  AND t.deleted IS FALSE;

-- name: MigrateLegacyUserSessionClient :execrows
-- Lifts one legacy OAuth proxy client registration (Redis) into
-- user_session_clients, preserving the original client_id so already-known
-- MCP clients skip re-registration after cutover. The conflict target
-- matches the partial unique index on (user_session_issuer_id, client_id)
-- WHERE deleted IS FALSE, so re-running a clone neither duplicates nor
-- clobbers an existing active row.
INSERT INTO user_session_clients (
    project_id,
    user_session_issuer_id,
    client_id,
    client_secret_hash,
    client_name,
    redirect_uris,
    client_secret_expires_at
)
SELECT usi.project_id, usi.id, @client_id, @client_secret_hash, @client_name, @redirect_uris::text[], NULL
FROM user_session_issuers AS usi
WHERE usi.id = @user_session_issuer_id
  AND usi.project_id = @project_id
  AND usi.deleted IS FALSE
ON CONFLICT (user_session_issuer_id, client_id) WHERE deleted IS FALSE DO NOTHING;

-- name: CreateRemoteSessionClient :one
INSERT INTO remote_session_clients (
    project_id,
    organization_id,
    remote_session_issuer_id,
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
    @organization_id,
    @remote_session_issuer_id,
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

-- name: GetRemoteSessionClientForClientMetadataDocument :one
-- Public CIMD document endpoint lookup. Intentionally NOT project-scoped: the
-- endpoint is unauthenticated and addresses clients by their globally unique
-- primary key, and the served document exposes only the client identity Gram
-- already sends the upstream AS as client_id (CIMD rows never carry a secret).
-- Mirrors GetRemoteSessionClientWithIssuerByID's id-only justification. A NULL
-- client_id_metadata_uri (non-CIMD client) yields no row, so the handler 404s.
SELECT client_id_metadata_uri, scope
FROM remote_session_clients
WHERE id = @id
  AND client_id_metadata_uri IS NOT NULL
  AND deleted IS FALSE;

-- name: GetRemoteSessionClientByID :one
SELECT
    sqlc.embed(c),
    (
        SELECT COALESCE(array_agg(link.user_session_issuer_id ORDER BY link.user_session_issuer_id), '{}'::uuid[])
        FROM remote_session_client_user_session_issuers AS link
        JOIN user_session_issuers AS usi ON usi.id = link.user_session_issuer_id
        WHERE link.remote_session_client_id = c.id
          AND usi.project_id = @project_id
    )::uuid[] AS user_session_issuer_ids
FROM remote_session_clients AS c
WHERE c.id = @id
  AND (c.project_id = @project_id OR (c.project_id IS NULL AND c.organization_id = @organization_id))
  AND c.deleted IS FALSE;

-- name: GetUserSessionIssuerForProject :one
SELECT id
FROM user_session_issuers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ListRemoteSessionClientsByProjectID :many
SELECT
    sqlc.embed(c),
    (
        SELECT COALESCE(array_agg(link.user_session_issuer_id ORDER BY link.user_session_issuer_id), '{}'::uuid[])
        FROM remote_session_client_user_session_issuers AS link
        JOIN user_session_issuers AS usi ON usi.id = link.user_session_issuer_id
        WHERE link.remote_session_client_id = c.id
          AND usi.project_id = @project_id
    )::uuid[] AS user_session_issuer_ids
FROM remote_session_clients AS c
WHERE (c.project_id = @project_id OR (c.project_id IS NULL AND c.organization_id = @organization_id))
  AND c.deleted IS FALSE
  AND (sqlc.narg('remote_session_issuer_id')::uuid IS NULL OR c.remote_session_issuer_id = sqlc.narg('remote_session_issuer_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR c.id < sqlc.narg('cursor')::uuid)
ORDER BY c.id DESC
LIMIT sqlc.arg('limit_value');

-- name: ListRemoteSessionClientsByProjectIDForUserSessionIssuer :many
-- Filters to clients bound to the given user_session_issuer through the join
-- table, while user_session_issuer_ids reports every issuer each client is
-- attached to (a correlated subquery independent of the filter join).
-- Includes both the project's own clients and organization-level clients
-- (project_id NULL) belonging to the project's org.
SELECT
    sqlc.embed(c),
    (
        SELECT COALESCE(array_agg(all_link.user_session_issuer_id ORDER BY all_link.user_session_issuer_id), '{}'::uuid[])
        FROM remote_session_client_user_session_issuers AS all_link
        JOIN user_session_issuers AS all_usi ON all_usi.id = all_link.user_session_issuer_id
        WHERE all_link.remote_session_client_id = c.id
          AND all_usi.project_id = @project_id
    )::uuid[] AS user_session_issuer_ids
FROM remote_session_client_user_session_issuers AS link
JOIN remote_session_clients AS c ON c.id = link.remote_session_client_id
JOIN user_session_issuers AS usi ON usi.id = link.user_session_issuer_id
WHERE link.user_session_issuer_id = @user_session_issuer_id
  AND usi.project_id = @project_id
  AND usi.deleted IS FALSE
  AND (c.project_id = @project_id OR (c.project_id IS NULL AND c.organization_id = @organization_id))
  AND c.deleted IS FALSE
  AND (sqlc.narg('remote_session_issuer_id')::uuid IS NULL OR c.remote_session_issuer_id = sqlc.narg('remote_session_issuer_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR c.id < sqlc.narg('cursor')::uuid)
ORDER BY c.id DESC
LIMIT sqlc.arg('limit_value');

-- name: UpdateRemoteSessionClient :one
UPDATE remote_session_clients
SET
    client_secret_encrypted = COALESCE(sqlc.narg('client_secret_encrypted'), client_secret_encrypted),
    client_secret_expires_at = COALESCE(sqlc.narg('client_secret_expires_at'), client_secret_expires_at),
    token_endpoint_auth_method = COALESCE(sqlc.narg('token_endpoint_auth_method'), token_endpoint_auth_method),
    scope = COALESCE(sqlc.narg('scope')::text[], scope),
    audience = COALESCE(sqlc.narg('audience'), audience),
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: CreateRemoteSessionClientCIMD :one
-- Create a client directly in Client ID Metadata Document (CIMD) mode. The
-- createCimd handler generates the row id and the document URL up front, so
-- client_id and client_id_metadata_uri are both set to that URL in a single
-- INSERT (kept equal by the remote_session_clients_client_id_metadata_uri_check
-- constraint). The row carries no secret and token_endpoint_auth_method is none.
INSERT INTO remote_session_clients (
    id,
    project_id,
    organization_id,
    remote_session_issuer_id,
    client_id,
    client_id_metadata_uri,
    client_id_issued_at,
    token_endpoint_auth_method,
    scope,
    audience
)
VALUES (
    @id,
    @project_id,
    @organization_id,
    @remote_session_issuer_id,
    @client_id_metadata_uri,
    @client_id_metadata_uri,
    @client_id_issued_at,
    'none',
    sqlc.narg('scope')::text[],
    @audience
)
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
-- session is usable when its access token is unexpired, or it is a NULL-expiry
-- token with no refresh path (non-expiring, e.g. Slack non-rotating xoxp), or
-- it carries a refresh token that is not itself known-expired to renew with. A
-- NULL access_expires_at counts as usable on its own ONLY when there is no
-- refresh token: with a refresh token present the gate re-validates on an
-- hourly cadence, so usability defers to the refresh-token clause. A
-- refresh_expires_at of NULL is a non-expiring refresh token. A
-- present-but-unusable row is 'expired' rather than dropped, so the consent UI
-- can distinguish "reconnect this expired link" from "never connected" — and
-- so the runtime gate (which rejects the same row as ErrNoValidToken) stops
-- disagreeing with a green "Connected" badge.
SELECT
  remote_session_client_id,
  (CASE
    WHEN access_expires_at > now()
      OR (access_expires_at IS NULL AND refresh_token_encrypted IS NULL)
      OR (refresh_token_encrypted IS NOT NULL
          AND (refresh_expires_at IS NULL OR refresh_expires_at > now())) THEN 'active'
    ELSE 'expired'
  END)::text AS status
FROM remote_sessions
WHERE subject_urn = @subject_urn
  AND user_session_issuer_id = @user_session_issuer_id
  AND deleted IS FALSE;

-- name: SetRemoteSessionUpdatedAt :exec
-- Sets updated_at on a remote session. Scoped through the owning
-- remote_session_client's project so the write cannot cross tenant boundaries.
-- Currently used by tests to backdate updated_at and exercise the
-- application-layer refresh cadence in validateAndRefresh (NULL
-- access_expires_at with a refresh token) without waiting wall-clock time.
UPDATE remote_sessions s
SET updated_at = @updated_at
FROM remote_session_clients c
WHERE s.id = @id
  AND s.remote_session_client_id = c.id
  AND c.project_id = @project_id;

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
-- the given user_session_issuer through the join table. Resolves both the
-- project's own clients and organization-level clients (project_id NULL)
-- belonging to the project's org, so an org-level client attached to this
-- project's user_session_issuer is honored at runtime.
SELECT
    c.id                                   AS client_id,
    c.client_id                            AS external_client_id,
    c.client_secret_encrypted              AS client_secret_encrypted,
    c.token_endpoint_auth_method           AS token_endpoint_auth_method,
    c.scope                                AS client_scope,
    c.audience                             AS client_audience,
    c.legacy_callback_url                  AS legacy_callback_url,
    c.remote_session_issuer_id             AS remote_session_issuer_id,
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
  AND (c.project_id = @project_id OR (c.project_id IS NULL AND c.organization_id = @organization_id))
  AND usi.project_id = @project_id
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE
  AND usi.deleted IS FALSE
ORDER BY c.id ASC;

-- name: ListRemoteSessionsByProjectID :many
-- Scoped by the session's user_session_issuer project, not the client's project:
-- a remote_session belongs to the project whose user_session_issuer minted it,
-- so sessions established through an organization-level client (project_id NULL)
-- bound to this project's user_session_issuer are listed here, while another
-- project's sessions on the same shared org-level client are not.
SELECT sqlc.embed(s),
  u.display_name AS subject_display_name,
  u.email AS subject_email
FROM remote_sessions AS s
JOIN remote_session_clients AS c ON c.id = s.remote_session_client_id
JOIN user_session_issuers AS usi ON usi.id = s.user_session_issuer_id
LEFT JOIN users AS u ON s.subject_urn = 'user:' || u.id AND u.deleted_at IS NULL
WHERE usi.project_id = @project_id
  AND s.deleted IS FALSE
  AND c.deleted IS FALSE
  AND (sqlc.narg('subject_urn')::text IS NULL OR s.subject_urn = sqlc.narg('subject_urn')::text)
  AND (sqlc.narg('remote_session_client_id')::uuid IS NULL OR s.remote_session_client_id = sqlc.narg('remote_session_client_id')::uuid)
  AND (sqlc.narg('cursor')::uuid IS NULL OR s.id < sqlc.narg('cursor')::uuid)
ORDER BY s.id DESC
LIMIT sqlc.arg('limit_value');

-- name: GetRemoteSessionByID :one
-- Scoped by the session's user_session_issuer project (see
-- ListRemoteSessionsByProjectID), so an organization-level client's session is
-- reachable from the project whose user_session_issuer minted it.
SELECT s.*
FROM remote_sessions AS s
JOIN remote_session_clients AS c ON c.id = s.remote_session_client_id
JOIN user_session_issuers AS usi ON usi.id = s.user_session_issuer_id
WHERE s.id = @id AND usi.project_id = @project_id AND s.deleted IS FALSE AND c.deleted IS FALSE;

-- name: RevokeRemoteSession :one
-- Scoped by the session's user_session_issuer project (see
-- ListRemoteSessionsByProjectID), so a project admin can revoke a session
-- established through an organization-level client bound to their own
-- user_session_issuer, but not another project's session on a shared one.
UPDATE remote_sessions AS s
SET deleted_at = clock_timestamp()
FROM remote_session_clients AS c, user_session_issuers AS usi
WHERE s.id = @id
  AND s.remote_session_client_id = c.id
  AND usi.id = s.user_session_issuer_id
  AND usi.project_id = @project_id
  AND s.deleted IS FALSE
  AND c.deleted IS FALSE
RETURNING s.*;

-- Organization administrator surface (AIS-119) — cross-project visibility into
-- remote_session_issuers, their clients, and sessions for an org. Every query is
-- scoped by organization_id (issuers carry it for both organizational and
-- project-specific rows); client/session queries reach the org through their
-- issuer, the sole cross-tenant guard since these endpoints carry no project
-- header.

-- name: ListOrganizationRemoteSessionIssuers :many
-- All issuers in the org (organizational and project-specific), each with its
-- associated non-deleted client count and, for project-specific issuers, the
-- owning project name.
SELECT
    sqlc.embed(i),
    COALESCE(p.name, '')::text AS project_name,
    (
        SELECT COUNT(*)
        FROM remote_session_clients AS c
        WHERE c.remote_session_issuer_id = i.id AND c.deleted IS FALSE
    )::bigint AS client_count
FROM remote_session_issuers AS i
LEFT JOIN projects AS p ON p.id = i.project_id
WHERE i.organization_id = @organization_id
  AND i.deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR i.id < sqlc.narg('cursor')::uuid)
ORDER BY i.id DESC
LIMIT sqlc.arg('limit_value');

-- name: GetOrganizationRemoteSessionIssuerByID :one
-- Any issuer in the org by id — organizational or project-specific.
SELECT *
FROM remote_session_issuers
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: UpdateOrganizationRemoteSessionIssuer :one
-- Same three-state narg semantics as UpdateRemoteSessionIssuer; scoped to any
-- issuer in the org (organizational or project-specific).
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
    client_id_metadata_document_supported = COALESCE(sqlc.narg('client_id_metadata_document_supported'), client_id_metadata_document_supported),
    oidc = COALESCE(sqlc.narg('oidc'), oidc),
    passthrough = COALESCE(sqlc.narg('passthrough'), passthrough),
    updated_at = clock_timestamp()
WHERE id = @id AND organization_id = @organization_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteOrganizationRemoteSessionIssuer :one
-- Soft-delete any issuer in the org (organizational or project-specific).
UPDATE remote_session_issuers
SET deleted_at = clock_timestamp()
WHERE id = @id AND organization_id = @organization_id AND deleted IS FALSE
RETURNING *;

-- name: SetOrganizationRemoteSessionIssuerProject :one
-- Re-scope an issuer by setting (project-specific) or clearing (organizational)
-- its project_id. A NULL narg clears to organization-level; a value moves it
-- into that project. Scoped to any issuer in the org. May raise a unique
-- violation on (project_id, slug) when moving into a project that already has an
-- issuer with the same slug.
UPDATE remote_session_issuers
SET project_id = sqlc.narg('project_id')::uuid,
    updated_at = clock_timestamp()
WHERE id = @id AND organization_id = @organization_id AND deleted IS FALSE
RETURNING *;

-- name: ListOrganizationRemoteSessionClientsByIssuerID :many
-- Clients registered with a given issuer in the org, each with the list of
-- user_session_issuers it is attached to (from the join table), its count of
-- attached MCP servers, and its active remote_sessions. The mcp_server_count
-- counts DISTINCT mcp_servers reachable through the client's join-table
-- attachments. The active_session_count counts non-deleted remote_sessions
-- minted against the client, matching CountActiveRemoteSessionsByClientID and
-- the delete preflight.
SELECT
    sqlc.embed(c),
    (
        SELECT COALESCE(array_agg(link.user_session_issuer_id ORDER BY link.user_session_issuer_id), '{}'::uuid[])
        FROM remote_session_client_user_session_issuers AS link
        WHERE link.remote_session_client_id = c.id
    )::uuid[] AS user_session_issuer_ids,
    (
        SELECT COUNT(DISTINCT m.id)
        FROM mcp_servers AS m
        WHERE m.deleted IS FALSE
          AND m.user_session_issuer_id IN (
              SELECT link.user_session_issuer_id
              FROM remote_session_client_user_session_issuers AS link
              WHERE link.remote_session_client_id = c.id
          )
    )::bigint AS mcp_server_count,
    (
        SELECT COUNT(*)
        FROM remote_sessions AS s
        WHERE s.remote_session_client_id = c.id
          AND s.deleted IS FALSE
    )::bigint AS active_session_count
FROM remote_session_clients AS c
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id
WHERE c.remote_session_issuer_id = @remote_session_issuer_id
  AND i.organization_id = @organization_id
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR c.id < sqlc.narg('cursor')::uuid)
ORDER BY c.id DESC
LIMIT sqlc.arg('limit_value');

-- name: GetOrganizationRemoteSessionClientByID :one
-- A client in the org by id, scoped through its issuer's organization_id.
SELECT
    sqlc.embed(c),
    (
        SELECT COALESCE(array_agg(link.user_session_issuer_id ORDER BY link.user_session_issuer_id), '{}'::uuid[])
        FROM remote_session_client_user_session_issuers AS link
        WHERE link.remote_session_client_id = c.id
    )::uuid[] AS user_session_issuer_ids
FROM remote_session_clients AS c
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id
WHERE c.id = @id
  AND i.organization_id = @organization_id
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE;

-- name: UpdateOrganizationRemoteSessionClient :one
-- Patch a client's fields, scoped through its issuer's organization_id. The
-- handler encrypts a rotated client_secret before passing it as
-- client_secret_encrypted; an omitted narg keeps the existing secret.
UPDATE remote_session_clients AS c
SET
    client_secret_encrypted = COALESCE(sqlc.narg('client_secret_encrypted'), c.client_secret_encrypted),
    token_endpoint_auth_method = COALESCE(sqlc.narg('token_endpoint_auth_method'), c.token_endpoint_auth_method),
    scope = COALESCE(sqlc.narg('scope')::text[], c.scope),
    audience = CASE
        WHEN sqlc.narg('audience')::text = '' THEN NULL
        ELSE COALESCE(sqlc.narg('audience'), c.audience)
    END,
    updated_at = clock_timestamp()
FROM remote_session_issuers AS i
WHERE c.id = @id
  AND c.remote_session_issuer_id = i.id
  AND i.organization_id = @organization_id
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE
RETURNING c.*;

-- name: DeleteOrganizationRemoteSessionClient :one
-- Soft-delete a client, scoped through its issuer's organization_id. The
-- handler cascades the client's remote_sessions via SoftDeleteRemoteSessionsByClientID.
UPDATE remote_session_clients AS c
SET deleted_at = clock_timestamp()
FROM remote_session_issuers AS i
WHERE c.id = @id
  AND c.remote_session_issuer_id = i.id
  AND i.organization_id = @organization_id
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE
RETURNING c.*;

-- name: ListOrganizationMcpServersForClient :many
-- MCP servers attached to a client through its user_session_issuer(s). Callers
-- establish the client belongs to the org upstream
-- (GetOrganizationRemoteSessionClientByID).
SELECT DISTINCT
    m.id,
    m.project_id,
    p.slug AS project_slug,
    m.name,
    m.slug,
    COALESCE(rms.url, '')::text AS url
FROM mcp_servers AS m
JOIN projects AS p ON p.id = m.project_id
LEFT JOIN remote_mcp_servers AS rms ON rms.id = m.remote_mcp_server_id
WHERE m.deleted IS FALSE
  AND m.user_session_issuer_id IN (
      SELECT link.user_session_issuer_id
      FROM remote_session_client_user_session_issuers AS link
      WHERE link.remote_session_client_id = @remote_session_client_id
  )
ORDER BY m.id DESC;

-- name: ListOrganizationMcpServerNamesForIssuer :many
-- Display names (and URL fallbacks) of MCP servers attached to any client of a
-- given issuer. Used to populate the issuer delete-confirmation dialog.
SELECT DISTINCT
    m.id,
    m.name,
    COALESCE(rms.url, '')::text AS url
FROM mcp_servers AS m
LEFT JOIN remote_mcp_servers AS rms ON rms.id = m.remote_mcp_server_id
WHERE m.deleted IS FALSE
  AND m.user_session_issuer_id IN (
      SELECT link.user_session_issuer_id
      FROM remote_session_client_user_session_issuers AS link
      JOIN remote_session_clients AS c ON c.id = link.remote_session_client_id
      WHERE c.remote_session_issuer_id = @remote_session_issuer_id AND c.deleted IS FALSE
  );

-- name: DetachRemoteSessionClientFromUserSessionIssuer :execrows
-- Remove the join-table binding between a remote_session_client and a
-- user_session_issuer. Used by the org-admin "remove client from MCP server"
-- action, where the user_session_issuer is the one the MCP server uses. Returns
-- the number of rows removed (0 means the client was not bound to that issuer).
-- Callers establish org ownership of the client upstream.
DELETE FROM remote_session_client_user_session_issuers
WHERE remote_session_client_id = @remote_session_client_id
  AND user_session_issuer_id = @user_session_issuer_id;

-- name: ListOrganizationRemoteSessionsByClientID :many
-- Sessions minted against a client, scoped through the client's issuer's
-- organization_id.
SELECT sqlc.embed(s),
  u.display_name AS subject_display_name,
  u.email AS subject_email
FROM remote_sessions AS s
JOIN remote_session_clients AS c ON c.id = s.remote_session_client_id
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id
LEFT JOIN users AS u ON s.subject_urn = 'user:' || u.id AND u.deleted_at IS NULL
WHERE s.remote_session_client_id = @remote_session_client_id
  AND i.organization_id = @organization_id
  AND s.deleted IS FALSE
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR s.id < sqlc.narg('cursor')::uuid)
ORDER BY s.id DESC
LIMIT sqlc.arg('limit_value');

-- name: RevokeOrganizationRemoteSession :one
-- Soft-delete a single session, scoped through the client's issuer's
-- organization_id. Returns the owning client's project_id so the handler can
-- attribute the audit event to the right project (NULL for org-level issuers).
UPDATE remote_sessions AS s
SET deleted_at = clock_timestamp()
FROM remote_session_clients AS c, remote_session_issuers AS i
WHERE s.id = @id
  AND s.remote_session_client_id = c.id
  AND c.remote_session_issuer_id = i.id
  AND i.organization_id = @organization_id
  AND s.deleted IS FALSE
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE
RETURNING s.*, c.project_id AS client_project_id;

-- name: GetOrganizationRemoteSessionByID :one
-- Load a single active session by id, scoped through the client's issuer's
-- organization_id. Returns the full embedded session row (including the
-- encrypted refresh token, which the org-admin refresh handler needs but the
-- API view never exposes), the owning client's project_id for audit
-- attribution, and the resolved subject identity for the returned view.
SELECT sqlc.embed(s),
  c.project_id AS client_project_id,
  u.display_name AS subject_display_name,
  u.email AS subject_email
FROM remote_sessions AS s
JOIN remote_session_clients AS c ON c.id = s.remote_session_client_id
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id
LEFT JOIN users AS u ON s.subject_urn = 'user:' || u.id AND u.deleted_at IS NULL
WHERE s.id = @id
  AND i.organization_id = @organization_id
  AND s.deleted IS FALSE
  AND c.deleted IS FALSE
  AND i.deleted IS FALSE;
