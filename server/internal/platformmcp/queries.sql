-- The OAuth client registry is global because dynamic registration happens before
-- browser authentication and organization selection. Every other Platform MCP-owned
-- state transition below receives an explicit organization_id predicate.

-- name: CreatePlatformMCPOAuthClient :one
INSERT INTO platform_mcp_oauth_clients (
    client_id,
    client_secret_hash,
    client_name,
    redirect_uris,
    client_secret_expires_at
) VALUES (
    @client_id,
    @client_secret_hash,
    @client_name,
    @redirect_uris,
    @client_secret_expires_at
)
RETURNING *;

-- name: GetActivePlatformMCPOAuthClientByClientID :one
SELECT *
FROM platform_mcp_oauth_clients
WHERE client_id = @client_id
  AND revoked_at IS NULL;

-- name: UpsertPlatformMCPOAuthClientFromCIMD :one
-- Lazy upsert for a client resolved from a Client ID Metadata Document at
-- authorize time. For CIMD rows the document URL IS the client_id, so the
-- conflict target is the same unique index that serves DCR lookups. On
-- refresh the mutable metadata (client_name, redirect_uris) and every cache
-- column are replaced wholesale, including the ETag, which is set to NULL
-- when the response carried no usable validator so the next refresh is
-- unconditional rather than replaying a stale one.
--
-- The cache expiry is derived from the database clock rather than the
-- application's, so it can never land before the client_id_metadata_fetched_at
-- written in the same statement.
--
-- The DO UPDATE is guarded so it can never touch a secret-bearing DCR row
-- that happens to share the client_id, nor resurrect a revoked one:
-- rewriting the former would trip the client_id_metadata_uri CHECK
-- constraints with an opaque 500, and the latter would undo an operator's
-- revocation. Either collision surfaces as no-rows, which the resolver maps
-- to invalid_client.
INSERT INTO platform_mcp_oauth_clients (
    client_id,
    client_secret_hash,
    client_name,
    redirect_uris,
    client_secret_expires_at,
    client_id_metadata_uri,
    client_id_metadata_fetched_at,
    client_id_metadata_cache_expires_at,
    client_id_metadata_etag
) VALUES (
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
ON CONFLICT (client_id)
DO UPDATE SET
    client_name = EXCLUDED.client_name,
    redirect_uris = EXCLUDED.redirect_uris,
    client_id_metadata_uri = EXCLUDED.client_id_metadata_uri,
    client_id_metadata_fetched_at = EXCLUDED.client_id_metadata_fetched_at,
    client_id_metadata_cache_expires_at = EXCLUDED.client_id_metadata_cache_expires_at,
    client_id_metadata_etag = EXCLUDED.client_id_metadata_etag,
    updated_at = clock_timestamp()
WHERE platform_mcp_oauth_clients.client_secret_hash IS NULL
  AND platform_mcp_oauth_clients.revoked_at IS NULL
RETURNING *;

-- name: UpdatePlatformMCPOAuthClientCIMDCache :one
-- Refreshes the cache bookkeeping on a CIMD-resolved client whose document
-- host answered 304 Not Modified. The stored client_name and redirect_uris
-- are current by definition of the 304, so they are deliberately untouched;
-- only the fetch stamp, the expiry, and the validator move.
--
-- The guards mirror UpsertPlatformMCPOAuthClientFromCIMD's, so this statement
-- can never push a row into violating the client_id_metadata_uri CHECK
-- constraints; such a collision surfaces as no-rows, which the resolver maps
-- to invalid_client.
UPDATE platform_mcp_oauth_clients
SET client_id_metadata_fetched_at = clock_timestamp(),
    client_id_metadata_cache_expires_at = clock_timestamp() + make_interval(secs => @cache_ttl_seconds::double precision),
    client_id_metadata_etag = sqlc.narg('client_id_metadata_etag'),
    updated_at = clock_timestamp()
WHERE client_id = @client_id
  AND client_id_metadata_uri IS NOT NULL
  AND client_secret_hash IS NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: GetPlatformMCPOAuthClientForUpdate :one
SELECT *
FROM platform_mcp_oauth_clients
WHERE client_id = @client_id
FOR UPDATE;

-- name: ListPlatformMCPClientConnectionsForUpdate :many
SELECT *
FROM platform_mcp_connections
WHERE oauth_client_id = @oauth_client_id
  AND revoked_at IS NULL
FOR UPDATE;

-- name: RevokePlatformMCPOAuthClient :one
UPDATE platform_mcp_oauth_clients
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE client_id = @client_id
  AND revoked_at IS NULL
RETURNING id;

-- name: LockPlatformMCPConnectionAuthorization :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        format('%s:%s:%s', @organization_id::text, @subject_urn::text, @oauth_client_id::text),
        0
    )
);

-- name: CreatePlatformMCPConnection :one
INSERT INTO platform_mcp_connections (
    id,
    organization_id,
    subject_urn,
    oauth_client_id,
    active_generation,
    authorization_expires_at
) VALUES (
    @id,
    @organization_id,
    @subject_urn,
    @oauth_client_id,
    @active_generation,
    @authorization_expires_at
)
RETURNING *;

-- name: GetActivePlatformMCPConnection :one
SELECT *
FROM platform_mcp_connections
WHERE organization_id = @organization_id
  AND subject_urn = @subject_urn
  AND oauth_client_id = @oauth_client_id
  AND revoked_at IS NULL;

-- name: GetActivePlatformMCPConnectionByID :one
SELECT connection.*, client.client_id
FROM platform_mcp_connections AS connection
JOIN platform_mcp_oauth_clients AS client
  ON client.id = connection.oauth_client_id
WHERE connection.id = @id
  AND connection.organization_id = @organization_id
  AND connection.revoked_at IS NULL
  AND client.revoked_at IS NULL;

-- name: GetActivePlatformMCPConnectionForFeedbackForUpdate :one
SELECT connection.*, client.client_id
FROM platform_mcp_connections AS connection
JOIN platform_mcp_oauth_clients AS client
  ON client.id = connection.oauth_client_id
WHERE connection.id = @id
  AND connection.organization_id = @organization_id
  AND connection.revoked_at IS NULL
  AND client.revoked_at IS NULL
FOR UPDATE OF connection;

-- name: GetPlatformMCPConnectionForUpdate :one
SELECT connection.*, client.client_id, client.revoked_at AS client_revoked_at
FROM platform_mcp_connections AS connection
JOIN platform_mcp_oauth_clients AS client
  ON client.id = connection.oauth_client_id
WHERE connection.id = @id
  AND connection.organization_id = @organization_id
FOR UPDATE OF connection;

-- name: RevokePlatformMCPConnection :one
UPDATE platform_mcp_connections
SET revoked_at = @revoked_at,
    reauthorization_required_at = @revoked_at,
    reauthorization_reason = 'connection_revoked',
    updated_at = @revoked_at
WHERE id = @id
  AND organization_id = @organization_id
  AND revoked_at IS NULL
RETURNING *;

-- name: MarkPlatformMCPConnectionReauthorizationRequired :one
UPDATE platform_mcp_connections
SET reauthorization_required_at = @reauthorization_required_at,
    reauthorization_reason = @reauthorization_reason,
    updated_at = @reauthorization_required_at
WHERE id = @connection_id
  AND organization_id = @organization_id
  AND active_generation = @connection_generation
  AND revoked_at IS NULL
RETURNING *;

-- name: RotatePlatformMCPConnectionGeneration :one
UPDATE platform_mcp_connections
SET active_generation = @active_generation,
    reauthorized_at = @reauthorized_at,
    authorization_expires_at = @authorization_expires_at,
    reauthorization_required_at = NULL,
    reauthorization_reason = NULL,
    updated_at = @reauthorized_at
WHERE id = @connection_id
  AND organization_id = @organization_id
  AND revoked_at IS NULL
RETURNING *;

-- name: CreatePlatformMCPAuthorizationGrant :one
INSERT INTO platform_mcp_authorization_grants (
    organization_id,
    authorization_code_hash,
    oauth_client_id,
    connection_id,
    connection_generation,
    redirect_uri,
    code_challenge,
    expires_at
) VALUES (
    @organization_id,
    @authorization_code_hash,
    @oauth_client_id,
    @connection_id,
    @connection_generation,
    @redirect_uri,
    @code_challenge,
    @expires_at
)
RETURNING *;

-- name: GetPlatformMCPAuthorizationGrantForValidation :one
SELECT
    auth_grant.*,
    connection.subject_urn,
    connection.active_generation,
    client.client_id,
    COALESCE(
        connection.authorization_expires_at,
        COALESCE(connection.reauthorized_at, connection.authorized_at) + INTERVAL '90 days'
    )::timestamptz AS effective_authorization_expires_at
FROM platform_mcp_authorization_grants AS auth_grant
JOIN platform_mcp_connections AS connection
  ON connection.id = auth_grant.connection_id
  AND connection.organization_id = auth_grant.organization_id
  AND connection.oauth_client_id = auth_grant.oauth_client_id
JOIN platform_mcp_oauth_clients AS client
  ON client.id = auth_grant.oauth_client_id
WHERE auth_grant.organization_id = @organization_id
  AND auth_grant.authorization_code_hash = @authorization_code_hash
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND client.revoked_at IS NULL;

-- name: GetPlatformMCPAuthorizationGrantForConsume :one
SELECT
    auth_grant.*,
    connection.subject_urn,
    connection.active_generation,
    client.client_id,
    COALESCE(
        connection.authorization_expires_at,
        COALESCE(connection.reauthorized_at, connection.authorized_at) + INTERVAL '90 days'
    )::timestamptz AS effective_authorization_expires_at
FROM platform_mcp_authorization_grants AS auth_grant
JOIN platform_mcp_connections AS connection
  ON connection.id = auth_grant.connection_id
  AND connection.organization_id = auth_grant.organization_id
  AND connection.oauth_client_id = auth_grant.oauth_client_id
JOIN platform_mcp_oauth_clients AS client
  ON client.id = auth_grant.oauth_client_id
WHERE auth_grant.organization_id = @organization_id
  AND auth_grant.authorization_code_hash = @authorization_code_hash
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND client.revoked_at IS NULL
FOR UPDATE OF auth_grant, connection;

-- name: ConsumePlatformMCPAuthorizationGrant :one
UPDATE platform_mcp_authorization_grants
SET consumed_at = @consumed_at,
    updated_at = @consumed_at
WHERE id = @id
  AND organization_id = @organization_id
  AND consumed_at IS NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: CreatePlatformMCPSession :one
INSERT INTO platform_mcp_sessions (
    id,
    organization_id,
    connection_id,
    oauth_client_id,
    connection_generation,
    jti,
    refresh_token_hash,
    expires_at,
    refresh_expires_at
) VALUES (
    @id,
    @organization_id,
    @connection_id,
    @oauth_client_id,
    @connection_generation,
    @jti,
    @refresh_token_hash,
    @expires_at,
    @refresh_expires_at
)
RETURNING *;

-- name: GetPlatformMCPSessionForRefresh :one
SELECT session.*, connection.subject_urn, connection.active_generation, client.client_id
FROM platform_mcp_sessions AS session
JOIN platform_mcp_connections AS connection
  ON connection.id = session.connection_id
  AND connection.organization_id = session.organization_id
  AND connection.oauth_client_id = session.oauth_client_id
JOIN platform_mcp_oauth_clients AS client
  ON client.id = session.oauth_client_id
WHERE session.organization_id = @organization_id
  AND session.refresh_token_hash = @refresh_token_hash
  AND session.revoked_at IS NULL
  AND session.refresh_expires_at > clock_timestamp()
  AND connection.revoked_at IS NULL
  AND connection.active_generation = session.connection_generation
  AND client.revoked_at IS NULL;

-- name: GetPlatformMCPSessionForRefreshForUpdate :one
-- Lock the connection before its session so refresh, connection revocation, and
-- client revocation all use the same connection -> session lock order.
WITH target_session AS MATERIALIZED (
    SELECT session.connection_id, session.oauth_client_id
    FROM platform_mcp_sessions AS session
    WHERE session.organization_id = @organization_id
      AND session.refresh_token_hash = @refresh_token_hash
),
locked_connection AS MATERIALIZED (
    SELECT connection.*
    FROM platform_mcp_connections AS connection
    JOIN target_session
      ON target_session.connection_id = connection.id
      AND target_session.oauth_client_id = connection.oauth_client_id
    WHERE connection.organization_id = @organization_id
    FOR UPDATE OF connection
),
locked_session AS MATERIALIZED (
    SELECT session.*
    FROM platform_mcp_sessions AS session
    JOIN locked_connection AS connection
      ON connection.id = session.connection_id
      AND connection.organization_id = session.organization_id
      AND connection.oauth_client_id = session.oauth_client_id
    WHERE session.organization_id = @organization_id
      AND session.refresh_token_hash = @refresh_token_hash
    FOR UPDATE OF session
)
SELECT
    session.*,
    connection.subject_urn,
    connection.active_generation,
    connection.revoked_at AS connection_revoked_at,
    connection.reauthorization_required_at,
    connection.reauthorization_reason,
    client.client_id,
    client.revoked_at AS client_revoked_at,
    COALESCE(
        connection.authorization_expires_at,
        COALESCE(connection.reauthorized_at, connection.authorized_at) + INTERVAL '90 days'
    )::timestamptz AS effective_authorization_expires_at
FROM locked_session AS session
JOIN locked_connection AS connection
  ON connection.id = session.connection_id
  AND connection.organization_id = session.organization_id
  AND connection.oauth_client_id = session.oauth_client_id
JOIN platform_mcp_oauth_clients AS client
  ON client.id = session.oauth_client_id;

-- name: RotatePlatformMCPSession :one
UPDATE platform_mcp_sessions
SET revoked_at = @rotated_at,
    rotated_at = @rotated_at,
    replaced_by_session_id = @replaced_by_session_id,
    updated_at = @rotated_at
WHERE id = @id
  AND organization_id = @organization_id
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokePlatformMCPSession :one
UPDATE platform_mcp_sessions
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE id = @id
  AND organization_id = @organization_id
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokePlatformMCPSessionByJTI :one
UPDATE platform_mcp_sessions
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE organization_id = @organization_id
  AND jti = @jti
  AND oauth_client_id = @oauth_client_id
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokePlatformMCPSessionFamily :exec
UPDATE platform_mcp_sessions
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE organization_id = @organization_id
  AND connection_id = @connection_id
  AND connection_generation = @connection_generation
  AND revoked_at IS NULL;

-- name: GetActivePlatformMCPSessionByJTI :one
SELECT
    session.connection_id,
    session.oauth_client_id,
    session.connection_generation,
    session.organization_id,
    connection.subject_urn,
    connection.active_generation,
    client.client_id
FROM platform_mcp_sessions AS session
JOIN platform_mcp_connections AS connection
  ON connection.id = session.connection_id
  AND connection.organization_id = session.organization_id
  AND connection.oauth_client_id = session.oauth_client_id
JOIN platform_mcp_oauth_clients AS client
  ON client.id = session.oauth_client_id
WHERE session.organization_id = @organization_id
  AND session.jti = @jti
  AND session.expires_at > clock_timestamp()
  AND session.revoked_at IS NULL
  AND connection.revoked_at IS NULL
  AND connection.active_generation = session.connection_generation
  AND client.revoked_at IS NULL;

-- name: GetPlatformMCPLifecycle :one
WITH default_project AS (
    SELECT id
    FROM projects
    WHERE organization_id = @organization_id
      AND slug = 'default'
      AND deleted IS FALSE
    LIMIT 1
)
SELECT
    default_project.id AS default_project_id,
    EXISTS (
        SELECT 1
        FROM plugin_github_connections
        WHERE project_id = default_project.id
    ) AS marketplace_published
FROM (VALUES (1)) AS root(value)
LEFT JOIN default_project ON TRUE;

-- name: ListPlatformMCPConnections :many
SELECT
    connection.id,
    connection.authorized_at,
    connection.reauthorized_at,
    EXISTS (
        SELECT 1
        FROM platform_mcp_onboarding_milestones AS milestone
        WHERE milestone.organization_id = connection.organization_id
          AND milestone.milestone = 'connection_ready'
          AND milestone.connection_id = connection.id
          AND milestone.connection_generation = connection.active_generation
    ) AS ready
FROM platform_mcp_connections AS connection
JOIN platform_mcp_oauth_clients AS client
  ON client.id = connection.oauth_client_id
WHERE connection.organization_id = @organization_id
  AND connection.revoked_at IS NULL
  AND client.revoked_at IS NULL
ORDER BY COALESCE(connection.reauthorized_at, connection.authorized_at) DESC, connection.id DESC;

-- name: RecordPlatformMCPConnectionReady :exec
INSERT INTO platform_mcp_onboarding_milestones (
    organization_id,
    milestone,
    connection_id,
    connection_generation
) VALUES (
    @organization_id,
    'connection_ready',
    @connection_id,
    @connection_generation
)
ON CONFLICT (milestone, connection_id, connection_generation)
WHERE connection_id IS NOT NULL
  AND connection_generation IS NOT NULL
  AND milestone IN (
    'authorization_succeeded',
    'authorization_failed',
    'connection_ready',
    'first_read_succeeded',
    'first_write_succeeded',
    'read_only_cohort'
)
DO NOTHING;

-- name: IsPlatformMCPNewModelEligible :one
-- Package admission requires at least one active issuer-backed MCP server with
-- an active endpoint. Platform runtime authorization deliberately does not use
-- this condition: a later project-model change must not invalidate an existing
-- organization-bound connection.
SELECT EXISTS (
    SELECT 1
    FROM mcp_servers AS server
    JOIN projects AS project
      ON project.id = server.project_id
     AND project.organization_id = @organization_id
     AND project.deleted IS FALSE
    JOIN user_session_issuers AS issuer
      ON issuer.id = server.user_session_issuer_id
     AND (issuer.project_id = project.id
          OR (issuer.project_id IS NULL AND issuer.organization_id = project.organization_id))
     AND issuer.deleted IS FALSE
    JOIN mcp_endpoints AS endpoint
      ON endpoint.mcp_server_id = server.id
     AND endpoint.project_id = project.id
     AND endpoint.deleted IS FALSE
    WHERE server.deleted IS FALSE
      AND server.visibility <> 'disabled'
);

-- name: ListPlatformMCPProjects :many
SELECT id, name, slug
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY id ASC
LIMIT @limit_value;

-- name: ListPlatformMCPServers :many
SELECT server.id, server.project_id, server.name, server.slug, server.visibility
FROM mcp_servers AS server
JOIN projects
  ON projects.id = server.project_id
WHERE server.project_id = @project_id
  AND projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND server.deleted IS FALSE
ORDER BY server.id ASC
LIMIT @limit_value;

-- name: GetPlatformMCPServer :one
SELECT server.id, server.project_id, server.name, server.slug, server.visibility
FROM mcp_servers AS server
JOIN projects
  ON projects.id = server.project_id
WHERE server.id = @mcp_server_id
  AND server.project_id = @project_id
  AND projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND server.deleted IS FALSE;

-- Slice 5B lifecycle state. Every query below is tenant-qualified; callers must
-- still perform live Platform authorization and mutation-gate checks before use.

-- name: ResolvePlatformMCPProjectBySlug :one
SELECT id, name, slug
FROM projects
WHERE organization_id = @organization_id
  AND slug = @slug
  AND deleted IS FALSE;

-- name: ResolvePlatformMCPProjectByID :one
SELECT id, name, slug
FROM projects
WHERE organization_id = @organization_id
  AND id = @project_id
  AND deleted IS FALSE;

-- name: IsPlatformMCPCatalogRegistrationTargetEligible :one
-- Registration may add a separately managed MCP server to any live project in
-- the active organization. Existing toolset-backed servers can coexist because
-- registration identity, component ownership, and active caps are enforced on
-- the Platform registration and its own component rows.
SELECT EXISTS (
    SELECT 1
    FROM projects AS target
    WHERE target.id = @project_id
      AND target.organization_id = @organization_id
      AND target.deleted IS FALSE
);

-- name: LockPlatformMCPOperationReceipt :exec
-- The receipt table records connection attribution, but RFC idempotency belongs
-- to the real user and exact target across connection generation/client changes.
-- Lock before lookup/reclaim/create to serialize that stronger contract.
SELECT pg_advisory_xact_lock(
    hashtextextended(
        jsonb_build_array('platform-mcp-receipt', @organization_id::text, @subject_urn::text, @project_id::text, @operation::text, @idempotency_key::text)::text,
        0
    )
);

-- name: LockPlatformMCPRemoteIssuerAttachment :exec
-- Serialize browser-catalog attachment for one project/upstream issuer before
-- checking for an existing remote-session issuer or registering a new client.
-- The remote_session_issuers table intentionally allows multiple project rows
-- for one issuer, so a unique constraint cannot express this narrower workflow
-- invariant without changing existing remote-session semantics.
SELECT pg_advisory_xact_lock(
    hashtextextended(
        jsonb_build_array('platform-mcp-remote-issuer-attachment', @organization_id::text, @project_id::text, @issuer::text)::text,
        0
    )
);

-- name: GetPlatformMCPOperationReceipt :one
-- Idempotency belongs to the real user, not to a connection: reauthorization
-- mints a new connection generation and must not let the same key replay a
-- create. Receipts written before user_id existed carry only a connection, so
-- they are still matched through its subject.
SELECT receipt.*
FROM platform_mcp_operation_receipts AS receipt
LEFT JOIN platform_mcp_connections AS connection
  ON connection.id = receipt.connection_id
 AND connection.organization_id = receipt.organization_id
WHERE receipt.organization_id = @organization_id
  AND receipt.project_id = @project_id
  AND receipt.operation = @operation
  AND receipt.idempotency_key = @idempotency_key
  AND (
    receipt.user_id = @user_id
    OR (receipt.user_id IS NULL AND connection.subject_urn = @subject_urn)
  )
ORDER BY receipt.created_at DESC, receipt.id DESC
LIMIT 1;

-- name: GetPlatformMCPProjectCreationReceipt :one
-- An operation that creates its own project has no project to key a receipt
-- on before it runs, so its replay lookup spans the organization: the receipt
-- is written against the project the operation created, and a retry finds it
-- by user, operation and key alone. Callers hold the advisory lock taken by
-- LockPlatformMCPOperationReceipt with an empty project id. Expired receipts
-- are ignored rather than reclaimed: each is pinned to the project it made,
-- so a fresh run under the same key can never collide with it.
SELECT *
FROM platform_mcp_operation_receipts
WHERE organization_id = @organization_id
  AND user_id = @user_id
  AND operation = @operation
  AND idempotency_key = @idempotency_key
  AND expires_at > clock_timestamp()
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: DeleteExpiredPlatformMCPOperationReceipt :execrows
-- Matches GetPlatformMCPOperationReceipt exactly. A receipt this cannot reach
-- never expires, and its idempotency key stays unusable for that user.
DELETE FROM platform_mcp_operation_receipts AS receipt
WHERE receipt.organization_id = @organization_id
  AND receipt.project_id = @project_id
  AND receipt.operation = @operation
  AND receipt.idempotency_key = @idempotency_key
  AND receipt.expires_at <= clock_timestamp()
  AND (
    receipt.user_id = @user_id
    OR (
      receipt.user_id IS NULL
      AND EXISTS (
        SELECT 1
        FROM platform_mcp_connections AS connection
        WHERE connection.id = receipt.connection_id
          AND connection.organization_id = receipt.organization_id
          AND connection.subject_urn = @subject_urn
      )
    )
  );

-- name: CreatePlatformMCPOperationReceipt :one
INSERT INTO platform_mcp_operation_receipts (
    organization_id,
    project_id,
    registration_id,
    connection_id,
    connection_generation,
    user_id,
    acting_surface,
    operation,
    idempotency_key,
    input_hash,
    status,
    result_code,
    result_payload,
    expires_at
) VALUES (
    @organization_id,
    @project_id,
    @registration_id,
    @connection_id,
    @connection_generation,
    @user_id,
    @acting_surface,
    @operation,
    @idempotency_key,
    @input_hash,
    @status,
    @result_code,
    @result_payload,
    @expires_at
)
RETURNING *;

-- name: AttachPlatformMCPOperationReceiptRegistration :one
UPDATE platform_mcp_operation_receipts
SET registration_id = @registration_id,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND status = 'pending'
RETURNING *;

-- name: CompletePlatformMCPOperationReceipt :one
UPDATE platform_mcp_operation_receipts
SET registration_id = @registration_id,
    status = @status,
    result_code = @result_code,
    result_payload = @result_payload,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
RETURNING *;

-- name: LockLivePlatformMCPProjectForRegistration :one
SELECT id
FROM projects
WHERE id = @project_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: LockPlatformMCPCatalogRegistration :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        jsonb_build_array('platform-mcp-registration', @organization_id::text, @project_id::text, @source_kind::text, @catalog_provider::text, @catalog_reference::text)::text,
        0
    )
);

-- name: GetActivePlatformMCPCatalogRegistration :one
SELECT *
FROM platform_mcp_catalog_registrations
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND source_kind = @source_kind
  AND catalog_provider = @catalog_provider
  AND catalog_reference = @catalog_reference
  AND deleted IS FALSE;

-- name: GetPlatformMCPCatalogRegistrationByID :one
SELECT *
FROM platform_mcp_catalog_registrations
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: ListPlatformMCPCatalogRegistrationsByRemoteMcpServer :many
SELECT id, user_session_issuer_id
FROM platform_mcp_catalog_registrations
WHERE remote_mcp_server_id = @remote_mcp_server_id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
ORDER BY id;

-- name: ListPlatformMCPInventoryAuthorizationCandidates :many
SELECT m.id, m.project_id
FROM mcp_servers AS m
JOIN projects AS project
  ON project.id = m.project_id
 AND project.organization_id = @organization_id
 AND project.deleted IS FALSE
WHERE m.deleted IS FALSE
ORDER BY m.id;

-- name: ListPlatformMCPInventoryAuthorizationCandidatePage :many
-- Lightweight, bounded candidate selection before live RBAC evaluation. Match
-- the inventory projection's safe project/cursor/query filters and search order
-- so hidden resources do not consume caller-visible result pages.
SELECT m.id, m.project_id
FROM mcp_servers AS m
JOIN projects AS project
  ON project.id = m.project_id
 AND project.organization_id = @organization_id
 AND project.deleted IS FALSE
WHERE m.deleted IS FALSE
  AND (sqlc.narg(project_id)::uuid IS NULL OR m.project_id = sqlc.narg(project_id)::uuid)
  AND (sqlc.narg(after_mcp_id)::uuid IS NULL OR m.id > sqlc.narg(after_mcp_id)::uuid)
  AND (
      @query_text::text = ''
      OR m.id::text ILIKE '%' || @query_text::text || '%'
      OR COALESCE(m.name, '') ILIKE '%' || @query_text::text || '%'
      OR COALESCE(m.slug, '') ILIKE '%' || @query_text::text || '%'
  )
ORDER BY
    CASE
        WHEN @query_text::text <> ''
         AND (m.id::text = @query_text::text OR LOWER(COALESCE(m.name, '')) = LOWER(@query_text::text) OR LOWER(COALESCE(m.slug, '')) = LOWER(@query_text::text))
        THEN 0
        ELSE 1
    END,
    m.id ASC
LIMIT LEAST(GREATEST(@limit_value::integer, 1), 1001);

-- name: ListPlatformMCPInventory :many
-- One bounded, tenant-qualified inventory projection for every Platform MCP
-- read surface. Callers supply the live RBAC-filtered MCP IDs so authorization
-- is applied before LIMIT/cursor pagination. It reads persisted readiness and
-- distribution state only; it never contacts a remote MCP or provider.
-- skip_authorization_filter is reserved for trusted internal services whose own
-- authorization boundary is broader than this member-facing read path.
SELECT
    m.id AS mcp_server_id,
    m.project_id,
    project.name AS project_name,
    project.slug AS project_slug,
    m.name AS mcp_name,
    m.slug AS mcp_slug,
    m.visibility,
    m.remote_mcp_server_id,
    m.tunneled_mcp_server_id,
    m.toolset_id,
    m.unproxied_mcp_server_id,
    COALESCE(remote.url, '') AS upstream_url,
    COALESCE(registration.id, '00000000-0000-0000-0000-000000000000'::uuid) AS registration_id,
    COALESCE(registration.source_kind, '') AS source_kind,
    COALESCE(registration.catalog_provider, '') AS catalog_provider,
    COALESCE(registration.catalog_reference, '') AS catalog_reference,
    COALESCE(registration.status, '') AS registration_status,
    COALESCE(registration.remote_mcp_server_id, '00000000-0000-0000-0000-000000000000'::uuid) AS registration_remote_mcp_server_id,
    COALESCE(registration.user_session_issuer_id, '00000000-0000-0000-0000-000000000000'::uuid) AS registration_user_session_issuer_id,
    COALESCE(registration.mcp_server_id, '00000000-0000-0000-0000-000000000000'::uuid) AS registration_mcp_server_id,
    COALESCE(registration.mcp_endpoint_id, '00000000-0000-0000-0000-000000000000'::uuid) AS registration_mcp_endpoint_id,
    COALESCE(readiness.state, '') AS readiness_state,
    readiness.checked_at AS readiness_checked_at,
    readiness.expires_at AS readiness_expires_at
FROM mcp_servers AS m
JOIN projects AS project
  ON project.id = m.project_id
 AND project.organization_id = @organization_id
 AND project.deleted IS FALSE
LEFT JOIN remote_mcp_servers AS remote
  ON remote.id = m.remote_mcp_server_id
 AND remote.project_id = m.project_id
 AND remote.deleted IS FALSE
LEFT JOIN LATERAL (
    SELECT registration.*
    FROM platform_mcp_catalog_registrations AS registration
    WHERE registration.organization_id = @organization_id
      AND registration.project_id = m.project_id
      AND registration.mcp_server_id = m.id
      AND registration.deleted IS FALSE
    ORDER BY registration.created_at DESC, registration.id DESC
    LIMIT 1
) AS registration ON TRUE
LEFT JOIN LATERAL (
    SELECT readiness.*
    FROM platform_mcp_readiness AS readiness
    WHERE readiness.organization_id = @organization_id
      AND readiness.project_id = m.project_id
      AND readiness.registration_id = registration.id
      AND (
          (sqlc.narg(connection_id)::uuid IS NOT NULL
              AND readiness.connection_id = sqlc.narg(connection_id)::uuid
              AND readiness.connection_generation = sqlc.narg(connection_generation)::uuid)
          OR
          (sqlc.narg(connection_id)::uuid IS NULL
              AND readiness.connection_id IS NULL
              AND readiness.user_id = @user_id
              AND readiness.acting_surface = @acting_surface)
      )
    ORDER BY readiness.checked_at DESC, readiness.id DESC
    LIMIT 1
) AS readiness ON TRUE
WHERE m.deleted IS FALSE
  AND (@skip_authorization_filter::boolean OR m.id = ANY(@allowed_mcp_ids::uuid[]))
  AND (sqlc.narg(project_id)::uuid IS NULL OR m.project_id = sqlc.narg(project_id)::uuid)
  AND (sqlc.narg(after_mcp_id)::uuid IS NULL OR m.id > sqlc.narg(after_mcp_id)::uuid)
  AND (
      @query_text::text = ''
      OR m.id::text ILIKE '%' || @query_text::text || '%'
      OR COALESCE(m.name, '') ILIKE '%' || @query_text::text || '%'
      OR COALESCE(m.slug, '') ILIKE '%' || @query_text::text || '%'
  )
  AND (
      sqlc.narg(readiness_state)::text IS NULL
      OR COALESCE(
          NULLIF(readiness.state, ''),
          CASE
              WHEN registration.id IS NOT NULL THEN 'unknown'
              ELSE 'unsupported'
          END
      ) = sqlc.narg(readiness_state)::text
  )
ORDER BY
    CASE
        WHEN @query_text::text <> ''
         AND (m.id::text = @query_text::text OR LOWER(COALESCE(m.name, '')) = LOWER(@query_text::text) OR LOWER(COALESCE(m.slug, '')) = LOWER(@query_text::text))
        THEN 0
        ELSE 1
    END,
    m.id ASC
LIMIT @limit_value;

-- name: GetPlatformMCPInventoryItem :one
SELECT *
FROM (
    SELECT
        m.id AS mcp_server_id,
        m.project_id,
        project.name AS project_name,
        project.slug AS project_slug,
        m.name AS mcp_name,
        m.slug AS mcp_slug,
        m.visibility,
        m.remote_mcp_server_id,
        m.tunneled_mcp_server_id,
        m.toolset_id,
        m.unproxied_mcp_server_id,
        COALESCE(remote.url, '') AS upstream_url,
        COALESCE(registration.id, '00000000-0000-0000-0000-000000000000'::uuid) AS registration_id,
        COALESCE(registration.source_kind, '') AS source_kind,
        COALESCE(registration.catalog_provider, '') AS catalog_provider,
        COALESCE(registration.catalog_reference, '') AS catalog_reference,
        COALESCE(registration.status, '') AS registration_status,
        COALESCE(registration.remote_mcp_server_id, '00000000-0000-0000-0000-000000000000'::uuid) AS registration_remote_mcp_server_id,
        COALESCE(registration.user_session_issuer_id, '00000000-0000-0000-0000-000000000000'::uuid) AS registration_user_session_issuer_id,
        COALESCE(registration.mcp_server_id, '00000000-0000-0000-0000-000000000000'::uuid) AS registration_mcp_server_id,
        COALESCE(registration.mcp_endpoint_id, '00000000-0000-0000-0000-000000000000'::uuid) AS registration_mcp_endpoint_id,
        COALESCE(readiness.state, '') AS readiness_state,
        readiness.checked_at AS readiness_checked_at,
        readiness.expires_at AS readiness_expires_at
    FROM mcp_servers AS m
    JOIN projects AS project
      ON project.id = m.project_id
     AND project.organization_id = @organization_id
     AND project.deleted IS FALSE
    LEFT JOIN remote_mcp_servers AS remote
      ON remote.id = m.remote_mcp_server_id
     AND remote.project_id = m.project_id
     AND remote.deleted IS FALSE
    LEFT JOIN LATERAL (
        SELECT registration.*
        FROM platform_mcp_catalog_registrations AS registration
        WHERE registration.organization_id = @organization_id
          AND registration.project_id = m.project_id
          AND registration.mcp_server_id = m.id
          AND registration.deleted IS FALSE
        ORDER BY registration.created_at DESC, registration.id DESC
        LIMIT 1
    ) AS registration ON TRUE
    LEFT JOIN LATERAL (
        SELECT readiness.*
        FROM platform_mcp_readiness AS readiness
        WHERE readiness.organization_id = @organization_id
          AND readiness.project_id = m.project_id
          AND readiness.registration_id = registration.id
          AND (
              (sqlc.narg(connection_id)::uuid IS NOT NULL
                  AND readiness.connection_id = sqlc.narg(connection_id)::uuid
                  AND readiness.connection_generation = sqlc.narg(connection_generation)::uuid)
              OR
              (sqlc.narg(connection_id)::uuid IS NULL
                  AND readiness.connection_id IS NULL
                  AND readiness.user_id = @user_id
                  AND readiness.acting_surface = @acting_surface)
          )
        ORDER BY readiness.checked_at DESC, readiness.id DESC
        LIMIT 1
    ) AS readiness ON TRUE
    WHERE m.id = @mcp_server_id
      AND m.project_id = @project_id
      AND m.deleted IS FALSE
) AS inventory;

-- plugin_servers is the attachment authority, so plugin membership is read from
-- it and keyed by MCP server. platform_mcp_distributions only records the
-- lifecycle of memberships this flow created, so it joins in for state and
-- leaves a dashboard-created membership with no lifecycle of its own.
-- name: ListPlatformMCPInventoryPluginMemberships :many
SELECT
    plugin_server.mcp_server_id,
    plugin.id AS plugin_id,
    plugin.name AS plugin_name,
    plugin.slug AS plugin_slug,
    distribution.state,
    distribution.publication_state
FROM plugin_servers AS plugin_server
JOIN plugins AS plugin
  ON plugin.id = plugin_server.plugin_id
 AND plugin.deleted IS FALSE
LEFT JOIN platform_mcp_distributions AS distribution
  ON distribution.plugin_server_id = plugin_server.id
 AND distribution.organization_id = plugin.organization_id
 AND distribution.project_id = plugin.project_id
WHERE plugin_server.deleted IS FALSE
  AND plugin_server.mcp_server_id = ANY(@mcp_server_ids::uuid[])
  AND plugin.organization_id = @organization_id
  AND (sqlc.narg(project_id)::uuid IS NULL OR plugin.project_id = sqlc.narg(project_id)::uuid)
ORDER BY plugin_server.mcp_server_id, plugin_server.id ASC;

-- name: GetPlatformMCPCatalogRegistrationForLifecycle :one
-- Registrations are project desired state, not permanently owned by the OAuth
-- client that originally created them. Lifecycle actions require the caller to
-- be the same user that created the registration.
--
-- A caller acting through an OAuth connection must additionally present a
-- live, unrevoked generation. A surface that holds no connection — the project
-- assistant acts under assistant identity — passes a null connection and is
-- authorized on every call upstream instead. Ownership still matches on the
-- real user, so a null connection widens nothing.
SELECT registration.*
FROM platform_mcp_catalog_registrations AS registration
LEFT JOIN platform_mcp_connections AS created_connection
  ON created_connection.id = registration.connection_id
 AND created_connection.organization_id = registration.organization_id
LEFT JOIN platform_mcp_connections AS current_connection
  ON current_connection.id = sqlc.narg(connection_id)
 AND current_connection.organization_id = registration.organization_id
JOIN projects AS project
  ON project.id = registration.project_id
 AND project.organization_id = registration.organization_id
 AND project.deleted IS FALSE
WHERE registration.id = @registration_id
  AND registration.organization_id = @organization_id
  AND registration.project_id = @project_id
  AND registration.deleted IS FALSE
  AND (
    registration.user_id = @user_id
    OR (registration.user_id IS NULL AND created_connection.subject_urn = @subject_urn)
  )
  AND (
    sqlc.narg(connection_id) IS NULL
    OR (
      current_connection.id IS NOT NULL
      AND current_connection.subject_urn = @subject_urn
      AND current_connection.active_generation = sqlc.narg(connection_generation)
      AND current_connection.revoked_at IS NULL
    )
  );

-- name: CreatePlatformMCPCatalogRegistration :one
INSERT INTO platform_mcp_catalog_registrations (
    organization_id,
    project_id,
    source_kind,
    catalog_provider,
    catalog_reference,
    status,
    connection_id,
    connection_generation,
    user_id,
    acting_surface
) VALUES (
    @organization_id,
    @project_id,
    @source_kind,
    @catalog_provider,
    @catalog_reference,
    @status,
    @connection_id,
    @connection_generation,
    @user_id,
    @acting_surface
)
RETURNING *;

-- name: UpdatePlatformMCPCatalogRegistrationComponents :one
UPDATE platform_mcp_catalog_registrations
SET status = @status,
    remote_mcp_server_id = @remote_mcp_server_id,
    remote_mcp_server_owned = @remote_mcp_server_owned,
    user_session_issuer_id = @user_session_issuer_id,
    user_session_issuer_owned = @user_session_issuer_owned,
    mcp_server_id = @mcp_server_id,
    mcp_server_owned = @mcp_server_owned,
    mcp_endpoint_id = @mcp_endpoint_id,
    mcp_endpoint_owned = @mcp_endpoint_owned,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: LockPlatformMCPSetupHandoff :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        jsonb_build_array('platform-mcp-handoff', @registration_id::text, @connection_id::text, @connection_generation::text, @intent::text)::text,
        0
    )
);

-- name: CreatePlatformMCPSetupHandoff :one
INSERT INTO platform_mcp_setup_handoffs (
    organization_id,
    project_id,
    registration_id,
    connection_id,
    connection_generation,
    user_id,
    acting_surface,
    provider_key,
    intent,
    handoff_hash,
    expires_at
)
SELECT
    @organization_id,
    @project_id,
    @registration_id,
    sqlc.narg(connection_id),
    sqlc.narg(connection_generation),
    @user_id,
    @acting_surface,
    @provider_key,
    @intent,
    @handoff_hash,
    @expires_at
WHERE EXISTS (
    SELECT 1
    FROM platform_mcp_catalog_registrations AS registration
    JOIN projects AS project
      ON project.id = registration.project_id
     AND project.organization_id = registration.organization_id
     AND project.deleted IS FALSE
    WHERE registration.id = @registration_id
      AND registration.organization_id = @organization_id
      AND registration.project_id = @project_id
      AND registration.deleted IS FALSE
)
RETURNING *;

-- name: InvalidateActivePlatformMCPSetupHandoffs :execrows
UPDATE platform_mcp_setup_handoffs
SET invalidated_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND registration_id = @registration_id
  AND (
    (
      connection_id = sqlc.narg(connection_id)
      AND connection_generation = sqlc.narg(connection_generation)
    )
    OR (connection_id IS NULL AND user_id = @user_id)
  )
  AND intent = @intent
  AND redeemed_at IS NULL
  AND invalidated_at IS NULL;

-- A handoff issued by a surface with no OAuth connection is redeemed by the
-- same user from the dashboard, so identity comes from the handoff's own user
-- attribution. A handoff that does carry a connection still has that
-- connection's liveness checked.
-- name: GetPlatformMCPSetupHandoffForDashboardStart :one
SELECT
    handoff.id,
    handoff.project_id,
    handoff.registration_id,
    handoff.provider_key,
    handoff.intent,
    handoff.connection_id,
    handoff.connection_generation,
    handoff.user_id,
    registration.catalog_reference,
    project.slug AS project_slug
FROM platform_mcp_setup_handoffs AS handoff
JOIN platform_mcp_catalog_registrations AS registration
  ON registration.id = handoff.registration_id
 AND registration.organization_id = handoff.organization_id
 AND registration.project_id = handoff.project_id
 AND registration.deleted IS FALSE
JOIN projects AS project
  ON project.id = registration.project_id
 AND project.organization_id = registration.organization_id
 AND project.deleted IS FALSE
LEFT JOIN platform_mcp_connections AS connection
  ON connection.id = handoff.connection_id
 AND connection.organization_id = handoff.organization_id
WHERE handoff.handoff_hash = @handoff_hash
  AND handoff.organization_id = @organization_id
  AND (
    handoff.user_id = @user_id
    OR (handoff.user_id IS NULL AND connection.subject_urn = @subject_urn)
  )
  AND (
    handoff.connection_id IS NULL
    OR (
      connection.subject_urn = @subject_urn
      AND connection.active_generation = handoff.connection_generation
      AND connection.revoked_at IS NULL
    )
  )
  AND handoff.redeemed_at IS NULL
  AND handoff.invalidated_at IS NULL
  AND handoff.expires_at > clock_timestamp();

-- name: ConsumePlatformMCPSetupHandoff :one
UPDATE platform_mcp_setup_handoffs AS handoff
SET redeemed_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE handoff.handoff_hash = @handoff_hash
  AND handoff.organization_id = @organization_id
  AND handoff.project_id = @project_id
  AND handoff.registration_id = @registration_id
  -- A handoff issued by a connection-less surface is matched by its user, the
  -- same way the dashboard-start lookup above matches it.
  AND (
    (
      handoff.connection_id = sqlc.narg(connection_id)
      AND handoff.connection_generation = sqlc.narg(connection_generation)
    )
    OR (handoff.connection_id IS NULL AND handoff.user_id = @user_id)
  )
  AND handoff.provider_key = @provider_key
  AND handoff.intent = @intent
  AND handoff.redeemed_at IS NULL
  AND handoff.invalidated_at IS NULL
  AND handoff.expires_at > clock_timestamp()
  AND EXISTS (
      SELECT 1
      FROM platform_mcp_catalog_registrations AS registration
      JOIN projects AS project
        ON project.id = registration.project_id
       AND project.organization_id = registration.organization_id
       AND project.deleted IS FALSE
      LEFT JOIN platform_mcp_connections AS connection
        ON connection.id = handoff.connection_id
       AND connection.organization_id = handoff.organization_id
      WHERE registration.id = handoff.registration_id
        AND registration.organization_id = handoff.organization_id
        AND registration.project_id = handoff.project_id
        AND registration.deleted IS FALSE
        AND (
          handoff.connection_id IS NULL
          OR (
            connection.subject_urn = @subject_urn
            AND connection.active_generation = handoff.connection_generation
            AND connection.revoked_at IS NULL
          )
        )
  )
RETURNING handoff.*;

-- name: GetLatestRedeemedPlatformMCPSetupHandoff :one
SELECT handoff.*
FROM platform_mcp_setup_handoffs AS handoff
JOIN platform_mcp_catalog_registrations AS registration
  ON registration.id = handoff.registration_id
 AND registration.organization_id = handoff.organization_id
 AND registration.project_id = handoff.project_id
 AND registration.deleted IS FALSE
JOIN projects AS project
  ON project.id = registration.project_id
 AND project.organization_id = registration.organization_id
 AND project.deleted IS FALSE
JOIN platform_mcp_connections AS connection
  ON connection.id = handoff.connection_id
  AND connection.organization_id = handoff.organization_id
WHERE handoff.organization_id = @organization_id
  AND handoff.project_id = @project_id
  AND handoff.registration_id = @registration_id
  AND handoff.connection_id = @connection_id
  AND handoff.connection_generation = @connection_generation
  AND connection.subject_urn = @subject_urn
  AND connection.active_generation = handoff.connection_generation
  AND connection.revoked_at IS NULL
  AND handoff.redeemed_at IS NOT NULL
  AND handoff.invalidated_at IS NULL
ORDER BY handoff.redeemed_at DESC, handoff.id DESC
LIMIT 1;

-- name: DeleteExpiredPlatformMCPReadiness :execrows
-- Retain the newest expired projection as stale repair evidence. Only an older
-- expired row that has been superseded by later evidence is safe to remove.
-- A connectionless assistant projection is keyed by its real user and surface.
DELETE FROM platform_mcp_readiness AS stale
WHERE stale.organization_id = @organization_id
  AND stale.project_id = @project_id
  AND stale.registration_id = @registration_id
  AND (
      (sqlc.narg(connection_id)::uuid IS NOT NULL
          AND stale.connection_id = sqlc.narg(connection_id)::uuid
          AND stale.connection_generation = sqlc.narg(connection_generation)::uuid)
      OR
      (sqlc.narg(connection_id)::uuid IS NULL
          AND stale.connection_id IS NULL
          AND stale.user_id = @user_id
          AND stale.acting_surface = @acting_surface)
  )
  AND stale.expires_at <= clock_timestamp()
  AND EXISTS (
      SELECT 1
      FROM platform_mcp_readiness AS newer
      WHERE newer.organization_id = stale.organization_id
        AND newer.project_id = stale.project_id
        AND newer.registration_id = stale.registration_id
        AND (
            (stale.connection_id IS NOT NULL
                AND newer.connection_id = stale.connection_id
                AND newer.connection_generation = stale.connection_generation)
            OR
            (stale.connection_id IS NULL
                AND newer.connection_id IS NULL
                AND newer.user_id = stale.user_id
                AND newer.acting_surface = stale.acting_surface)
        )
        AND (newer.checked_at, newer.id) > (stale.checked_at, stale.id)
  );

-- name: GetPlatformMCPReadiness :one
SELECT readiness.*
FROM platform_mcp_readiness AS readiness
JOIN platform_mcp_catalog_registrations AS registration
  ON registration.id = readiness.registration_id
 AND registration.organization_id = readiness.organization_id
 AND registration.project_id = readiness.project_id
 AND registration.deleted IS FALSE
JOIN projects AS project
  ON project.id = readiness.project_id
 AND project.organization_id = readiness.organization_id
 AND project.deleted IS FALSE
WHERE readiness.organization_id = @organization_id
  AND readiness.project_id = @project_id
  AND readiness.registration_id = @registration_id
  AND readiness.provider_authorization_fingerprint = @provider_authorization_fingerprint
  AND (
      (sqlc.narg(connection_id)::uuid IS NOT NULL
          AND readiness.connection_id = sqlc.narg(connection_id)::uuid
          AND readiness.connection_generation = sqlc.narg(connection_generation)::uuid)
      OR
      (sqlc.narg(connection_id)::uuid IS NULL
          AND readiness.connection_id IS NULL
          AND readiness.user_id = @user_id
          AND readiness.acting_surface = @acting_surface)
  );

-- name: GetLatestPlatformMCPReadinessForLifecycle :one
-- External callers retain live connection/generation checks. A connectionless
-- caller may only read evidence attributed to that same real user and surface.
SELECT readiness.*
FROM platform_mcp_readiness AS readiness
JOIN platform_mcp_catalog_registrations AS registration
  ON registration.id = readiness.registration_id
 AND registration.organization_id = readiness.organization_id
 AND registration.project_id = readiness.project_id
 AND registration.deleted IS FALSE
JOIN projects AS project
  ON project.id = readiness.project_id
 AND project.organization_id = readiness.organization_id
 AND project.deleted IS FALSE
LEFT JOIN platform_mcp_connections AS connection
  ON connection.id = readiness.connection_id
 AND connection.organization_id = readiness.organization_id
WHERE readiness.organization_id = @organization_id
  AND readiness.project_id = @project_id
  AND readiness.registration_id = @registration_id
  AND (
      (sqlc.narg(connection_id)::uuid IS NOT NULL
          AND readiness.connection_id = sqlc.narg(connection_id)::uuid
          AND readiness.connection_generation = sqlc.narg(connection_generation)::uuid
          AND connection.subject_urn = @subject_urn
          AND connection.active_generation = readiness.connection_generation
          AND connection.revoked_at IS NULL)
      OR
      (sqlc.narg(connection_id)::uuid IS NULL
          AND readiness.connection_id IS NULL
          AND readiness.user_id = @user_id
          AND readiness.acting_surface = @acting_surface)
  )
ORDER BY readiness.checked_at DESC, readiness.id DESC
LIMIT 1;

-- name: UpsertPlatformMCPReadinessExternal :one
-- External evidence remains connection/generation scoped. The predicate names
-- the expand-phase external partial index explicitly, so it never races with
-- connectionless assistant evidence on the legacy full binding index.
INSERT INTO platform_mcp_readiness (
    organization_id,
    project_id,
    registration_id,
    connection_id,
    connection_generation,
    user_id,
    acting_surface,
    provider_authorization_fingerprint,
    state,
    evidence_code,
    checked_at,
    expires_at
)
SELECT
    @organization_id,
    @project_id,
    @registration_id,
    @connection_id,
    @connection_generation,
    @user_id,
    @acting_surface,
    @provider_authorization_fingerprint,
    @state,
    @evidence_code,
    @checked_at,
    @expires_at
WHERE EXISTS (
    SELECT 1
     FROM platform_mcp_catalog_registrations AS registration
     JOIN projects AS project
       ON project.id = registration.project_id
      AND project.organization_id = registration.organization_id
      AND project.deleted IS FALSE
     WHERE registration.id = @registration_id
       AND registration.organization_id = @organization_id
       AND registration.project_id = @project_id
       AND registration.deleted IS FALSE
)
ON CONFLICT (registration_id, connection_id, connection_generation, provider_authorization_fingerprint)
WHERE connection_id IS NOT NULL
DO UPDATE SET
    state = EXCLUDED.state,
    evidence_code = EXCLUDED.evidence_code,
    checked_at = EXCLUDED.checked_at,
    expires_at = EXCLUDED.expires_at,
    updated_at = clock_timestamp()
WHERE platform_mcp_readiness.checked_at <= EXCLUDED.checked_at
RETURNING *;

-- name: UpsertPlatformMCPReadinessAssistant :one
-- A connectionless assistant is an actor, not an empty connection. Its unique
-- binding includes the real user and trusted surface, preventing different
-- assistants from overwriting or hiding each other's evidence.
INSERT INTO platform_mcp_readiness (
    organization_id,
    project_id,
    registration_id,
    connection_id,
    connection_generation,
    user_id,
    acting_surface,
    provider_authorization_fingerprint,
    state,
    evidence_code,
    checked_at,
    expires_at
)
SELECT
    @organization_id,
    @project_id,
    @registration_id,
    @connection_id,
    @connection_generation,
    @user_id,
    @acting_surface,
    @provider_authorization_fingerprint,
    @state,
    @evidence_code,
    @checked_at,
    @expires_at
WHERE EXISTS (
    SELECT 1
     FROM platform_mcp_catalog_registrations AS registration
     JOIN projects AS project
       ON project.id = registration.project_id
      AND project.organization_id = registration.organization_id
      AND project.deleted IS FALSE
     WHERE registration.id = @registration_id
       AND registration.organization_id = @organization_id
       AND registration.project_id = @project_id
       AND registration.deleted IS FALSE
)
ON CONFLICT (registration_id, user_id, acting_surface, provider_authorization_fingerprint)
WHERE connection_id IS NULL
DO UPDATE SET
    user_id = EXCLUDED.user_id,
    acting_surface = EXCLUDED.acting_surface,
    state = EXCLUDED.state,
    evidence_code = EXCLUDED.evidence_code,
    checked_at = EXCLUDED.checked_at,
    expires_at = EXCLUDED.expires_at,
    updated_at = clock_timestamp()
WHERE platform_mcp_readiness.checked_at <= EXCLUDED.checked_at
RETURNING *;

-- name: LockPlatformMCPDistribution :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        jsonb_build_array(@organization_id::text, @project_id::text, @registration_id::text, @plugin_id::text)::text,
        0
    )
);

-- name: GetPlatformMCPDistribution :one
-- This is a neutral desired-state lookup used by inventory and write paths.
-- Default-only mutations revalidate plugins.is_default in their write queries;
-- inventory intentionally projects COALESCE(plugin_id, default_plugin_id).
SELECT distribution.*
FROM platform_mcp_distributions AS distribution
JOIN projects AS project
  ON project.id = distribution.project_id
 AND project.organization_id = distribution.organization_id
 AND project.deleted IS FALSE
WHERE distribution.organization_id = @organization_id
  AND distribution.project_id = @project_id
  AND distribution.registration_id = @registration_id
  AND distribution.default_plugin_id = @plugin_id;

-- name: CreatePlatformMCPDistribution :one
INSERT INTO platform_mcp_distributions (
    organization_id,
    project_id,
    registration_id,
    -- Dual-written during expand: default_plugin_id is the legacy column name
    -- and no longer implies the project's default plugin, plugin_id is the
    -- column exact-plugin readers move to. Both carry the exact target.
    default_plugin_id,
    plugin_id,
    plugin_server_id,
    state,
    version,
    attachment_was_created,
    connection_id,
    connection_generation
)
SELECT
    @organization_id,
    @project_id,
    @registration_id,
    @plugin_id,
    @plugin_id,
    @plugin_server_id,
    @state,
    @version,
    @attachment_was_created,
    @connection_id,
    @connection_generation
WHERE EXISTS (
    SELECT 1
    FROM projects AS project
    JOIN platform_mcp_catalog_registrations AS registration
      ON registration.id = @registration_id
     AND registration.organization_id = project.organization_id
     AND registration.project_id = project.id
     AND registration.deleted IS FALSE
    JOIN plugins AS plugin
      ON plugin.id = @plugin_id
     AND plugin.organization_id = project.organization_id
     AND plugin.project_id = project.id
     AND plugin.deleted IS FALSE
    JOIN platform_mcp_connections AS connection
      ON connection.id = @connection_id
     AND connection.organization_id = project.organization_id
     AND connection.active_generation = @connection_generation
     AND connection.revoked_at IS NULL
    WHERE project.id = @project_id
      AND project.organization_id = @organization_id
      AND project.deleted IS FALSE
)
RETURNING *;

-- name: UpdatePlatformMCPDistribution :one
UPDATE platform_mcp_distributions
SET plugin_server_id = @plugin_server_id,
    plugin_id = @plugin_id,
    state = @state,
    version = @version,
    attachment_was_created = @attachment_was_created,
    publication_state = 'pending',
    publication_updated_at = NULL,
    connection_id = @connection_id,
    connection_generation = @connection_generation,
    updated_at = clock_timestamp()
WHERE platform_mcp_distributions.id = @id
  AND platform_mcp_distributions.organization_id = @organization_id
  AND platform_mcp_distributions.project_id = @project_id
  AND platform_mcp_distributions.registration_id = @registration_id
  AND platform_mcp_distributions.default_plugin_id = @plugin_id
  AND EXISTS (
      SELECT 1
      FROM projects AS project
      JOIN platform_mcp_catalog_registrations AS registration
        ON registration.id = platform_mcp_distributions.registration_id
       AND registration.organization_id = project.organization_id
       AND registration.project_id = project.id
       AND registration.deleted IS FALSE
      JOIN plugins AS plugin
        ON plugin.id = platform_mcp_distributions.default_plugin_id
       AND plugin.organization_id = project.organization_id
       AND plugin.project_id = project.id
       AND plugin.deleted IS FALSE
      JOIN platform_mcp_connections AS connection
        ON connection.id = @connection_id
       AND connection.organization_id = project.organization_id
       AND connection.active_generation = @connection_generation
       AND connection.revoked_at IS NULL
      WHERE project.id = platform_mcp_distributions.project_id
        AND project.organization_id = platform_mcp_distributions.organization_id
        AND project.deleted IS FALSE
  )
RETURNING *;

-- name: UpdatePlatformMCPDistributionPublication :one
UPDATE platform_mcp_distributions
SET publication_state = @publication_state,
    publication_updated_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE platform_mcp_distributions.id = @id
  AND platform_mcp_distributions.organization_id = @organization_id
  AND platform_mcp_distributions.project_id = @project_id
  AND platform_mcp_distributions.registration_id = @registration_id
  AND platform_mcp_distributions.default_plugin_id = @plugin_id
  AND platform_mcp_distributions.version = @version
  AND EXISTS (
      SELECT 1
      FROM projects AS project
      JOIN platform_mcp_catalog_registrations AS registration
        ON registration.id = platform_mcp_distributions.registration_id
       AND registration.organization_id = project.organization_id
       AND registration.project_id = project.id
       AND registration.deleted IS FALSE
      JOIN plugins AS plugin
        ON plugin.id = platform_mcp_distributions.default_plugin_id
       AND plugin.organization_id = project.organization_id
       AND plugin.project_id = project.id
       AND plugin.deleted IS FALSE
      JOIN platform_mcp_connections AS connection
        ON connection.id = platform_mcp_distributions.connection_id
       AND connection.organization_id = project.organization_id
       AND connection.active_generation = platform_mcp_distributions.connection_generation
       AND connection.revoked_at IS NULL
      WHERE project.id = platform_mcp_distributions.project_id
        AND project.organization_id = platform_mcp_distributions.organization_id
        AND project.deleted IS FALSE
  )
RETURNING *;

-- name: HasPlatformMCPSelectedUseEvidence :one
-- Selected-use credit follows the plugin the distribution actually targets. The
-- plugin join stays so evidence from a deleted plugin does not count; the
-- Default-only restriction it carried during the compatibility rollout is gone
-- now that named-plugin distribution is live.
SELECT EXISTS (
    SELECT 1
    FROM platform_mcp_selected_use_evidence AS evidence
    JOIN platform_mcp_distributions AS distribution
      ON distribution.id = evidence.distribution_id
     AND distribution.project_id = evidence.project_id
     AND distribution.registration_id = evidence.registration_id
     AND distribution.version = evidence.distribution_version
     AND distribution.state = 'attached'
     JOIN projects AS project
       ON project.id = distribution.project_id
      AND project.organization_id = distribution.organization_id
      AND project.deleted IS FALSE
     JOIN plugins AS plugin
       ON plugin.id = COALESCE(distribution.plugin_id, distribution.default_plugin_id)
      AND plugin.organization_id = distribution.organization_id
      AND plugin.project_id = distribution.project_id
      AND plugin.deleted IS FALSE
     JOIN platform_mcp_connections AS connection
      ON connection.id = distribution.connection_id
     AND connection.organization_id = distribution.organization_id
     AND connection.active_generation = distribution.connection_generation
     AND connection.revoked_at IS NULL
    WHERE evidence.organization_id = @organization_id
      AND evidence.project_id = @project_id
      AND evidence.registration_id = @registration_id
      AND connection.subject_urn = @initiating_subject_urn
);

-- name: GetPlatformMCPSelectedUseTarget :one
-- Resolve the target through the plugin the distribution names, which is the
-- default plugin only when that is what the caller asked for.
SELECT
    distribution.id AS distribution_id,
    distribution.version AS distribution_version,
    distribution.default_plugin_id,
    distribution.registration_id,
    (registration.catalog_provider || ':' || registration.catalog_reference)::text AS mcp_key,
    workflow.id AS workflow_id,
    distribution.connection_id,
    distribution.connection_generation
FROM platform_mcp_distributions AS distribution
JOIN projects AS project
  ON project.id = distribution.project_id
 AND project.organization_id = distribution.organization_id
 AND project.deleted IS FALSE
 JOIN platform_mcp_catalog_registrations AS registration
   ON registration.id = distribution.registration_id
  AND registration.project_id = distribution.project_id
  AND registration.organization_id = distribution.organization_id
  AND registration.deleted IS FALSE
 JOIN plugins AS plugin
   ON plugin.id = COALESCE(distribution.plugin_id, distribution.default_plugin_id)
  AND plugin.organization_id = distribution.organization_id
  AND plugin.project_id = distribution.project_id
  AND plugin.deleted IS FALSE
 JOIN plugin_servers AS plugin_server
  ON plugin_server.id = distribution.plugin_server_id
  AND plugin_server.plugin_id = distribution.default_plugin_id
  AND plugin_server.deleted IS FALSE
JOIN platform_mcp_connections AS connection
  ON connection.id = distribution.connection_id
 AND connection.organization_id = distribution.organization_id
 AND connection.active_generation = distribution.connection_generation
 AND connection.revoked_at IS NULL
LEFT JOIN platform_mcp_onboarding_workflows AS workflow
  ON workflow.organization_id = distribution.organization_id
 AND workflow.initiating_subject_urn = @initiating_subject_urn
 AND workflow.selected_project_id = distribution.project_id
 AND workflow.selected_registration_id = distribution.registration_id
 AND workflow.status = 'active'
WHERE distribution.organization_id = @organization_id
  AND distribution.project_id = @project_id
  AND connection.subject_urn = @initiating_subject_urn
  AND registration.mcp_server_id = @mcp_server_id
  AND registration.status = 'registered'
  AND distribution.state = 'attached'
  AND EXISTS (
      SELECT 1
      FROM platform_mcp_readiness AS readiness
      WHERE readiness.organization_id = distribution.organization_id
        AND readiness.project_id = distribution.project_id
        AND readiness.registration_id = distribution.registration_id
        AND readiness.connection_id = distribution.connection_id
        AND readiness.connection_generation = distribution.connection_generation
        AND readiness.state = 'ready'
        AND readiness.expires_at > clock_timestamp()
  )
ORDER BY distribution.updated_at DESC, distribution.id DESC
LIMIT 1;

-- name: CreatePlatformMCPSelectedUseEvidence :exec
INSERT INTO platform_mcp_selected_use_evidence (
    organization_id,
    project_id,
    registration_id,
    distribution_id,
    distribution_version,
    workflow_id,
    tool_name,
    tool_category,
    succeeded_at
) VALUES (
    @organization_id,
    @project_id,
    @registration_id,
    @distribution_id,
    @distribution_version,
    @workflow_id,
    @tool_name,
    @tool_category,
    @succeeded_at
)
ON CONFLICT (distribution_id, distribution_version)
DO NOTHING;

-- name: LockPlatformMCPFeedbackOrganization :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(@organization_id::text, 0)
);

-- name: LockPlatformMCPFeedbackSubmission :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        jsonb_build_array('platform-mcp-feedback', @organization_id::text, @subject_urn::text, @idempotency_key::text)::text,
        0
    )
);

-- name: DeleteExpiredPlatformMCPFeedback :execrows
DELETE FROM platform_mcp_feedback
WHERE organization_id = @organization_id
  AND expires_at <= clock_timestamp();

-- name: GetPlatformMCPFeedbackByIdempotencyKey :one
SELECT id, delivery_state, expires_at, input_hash
FROM platform_mcp_feedback
WHERE organization_id = @organization_id
  AND subject_urn = @subject_urn
  AND idempotency_key = @idempotency_key;

-- name: CountRecentPlatformMCPFeedbackByConnection :one
SELECT COUNT(*)::bigint
FROM platform_mcp_feedback
WHERE organization_id = @organization_id
  AND connection_id = @connection_id
  AND created_at >= @since;

-- name: CountRecentPlatformMCPFeedbackByOrganization :one
SELECT COUNT(*)::bigint
FROM platform_mcp_feedback
WHERE organization_id = @organization_id
  AND created_at >= @since;

-- name: CreatePlatformMCPFeedback :one
INSERT INTO platform_mcp_feedback (
    organization_id,
    subject_urn,
    connection_id,
    connection_generation,
    category,
    rating,
    success,
    tool_name,
    failure_category,
    note,
    delivery_state,
    idempotency_key,
    input_hash,
    expires_at
)
SELECT
    @organization_id,
    @subject_urn,
    @connection_id,
    @connection_generation,
    @category,
    @rating,
    @success,
    @tool_name,
    @failure_category,
    @note,
    'queued',
    @idempotency_key,
    @input_hash,
    @expires_at
WHERE EXISTS (
    SELECT 1
    FROM platform_mcp_connections AS connection
    WHERE connection.organization_id = @organization_id
      AND connection.id = @connection_id
      AND connection.subject_urn = @subject_urn
      AND connection.active_generation = @connection_generation
      AND connection.revoked_at IS NULL
)
RETURNING id, delivery_state, expires_at;

-- name: RecordPlatformMCPFirstValueAchieved :exec
INSERT INTO platform_mcp_onboarding_milestones (
    organization_id,
    milestone,
    connection_id,
    connection_generation,
    project_id,
    mcp_key
)
SELECT
    @organization_id,
    'first_value_achieved',
    @connection_id,
    @connection_generation,
    @project_id,
    @mcp_key
WHERE EXISTS (
    SELECT 1
    FROM projects AS project
    JOIN platform_mcp_connections AS connection
      ON connection.id = @connection_id
     AND connection.organization_id = project.organization_id
     AND connection.active_generation = @connection_generation
     AND connection.revoked_at IS NULL
    WHERE project.id = @project_id
      AND project.organization_id = @organization_id
      AND project.deleted IS FALSE
)
ON CONFLICT (organization_id, project_id, mcp_key)
WHERE milestone = 'first_value_achieved'
DO NOTHING;

-- name: GetDirectRemoteAdmissionTargetForMCPServer :one
-- Distribution admission follows durable Platform MCP provenance, including a
-- soft-deleted registration while its exact MCP server still exists. The live
-- remote URL comes from the MCP server's current backend, never from the
-- historical catalog reference.
SELECT
    registration.id AS registration_id,
    server.remote_mcp_server_id,
    remote.url AS remote_url
FROM platform_mcp_catalog_registrations AS registration
JOIN mcp_servers AS server
  ON server.id = registration.mcp_server_id
 AND server.project_id = registration.project_id
 AND server.deleted IS FALSE
LEFT JOIN remote_mcp_servers AS remote
  ON remote.id = server.remote_mcp_server_id
 AND remote.project_id = server.project_id
 AND remote.deleted IS FALSE
WHERE registration.organization_id = @organization_id
  AND registration.project_id = @project_id
  AND registration.catalog_provider = 'direct-remote-url-v1'
  AND registration.mcp_server_id = @mcp_server_id
ORDER BY registration.created_at DESC, registration.id DESC
LIMIT 1;

-- name: ListDirectRemoteAdmissionTargetsForPlugin :many
-- Return every distinct in-scope MCP target attached to one exact live plugin.
-- Assignment callers supply the complete desired audience separately, so no
-- presentation limit is allowed here.
SELECT DISTINCT
    server.id AS mcp_server_id,
    remote.url AS remote_url
FROM plugins AS plugin
JOIN plugin_servers AS attachment
  ON attachment.plugin_id = plugin.id
  AND attachment.deleted IS FALSE
LEFT JOIN meta_mcp_servers AS gateway
  ON gateway.id = attachment.meta_mcp_server_id
 AND gateway.project_id = plugin.project_id
 AND gateway.organization_id = plugin.organization_id
 AND gateway.deleted IS FALSE
LEFT JOIN meta_mcp_server_members AS member
  ON member.meta_mcp_server_id = gateway.id
 AND member.project_id = plugin.project_id
 AND member.deleted IS FALSE
JOIN mcp_servers AS server
  ON server.id = COALESCE(attachment.mcp_server_id, member.mcp_server_id)
 AND server.project_id = plugin.project_id
 AND server.deleted IS FALSE
JOIN platform_mcp_catalog_registrations AS registration
  ON registration.mcp_server_id = server.id
 AND registration.organization_id = plugin.organization_id
 AND registration.project_id = plugin.project_id
 AND registration.catalog_provider = 'direct-remote-url-v1'
LEFT JOIN remote_mcp_servers AS remote
  ON remote.id = server.remote_mcp_server_id
 AND remote.project_id = server.project_id
 AND remote.deleted IS FALSE
WHERE plugin.id = @plugin_id
  AND plugin.organization_id = @organization_id
  AND plugin.project_id = @project_id
  AND plugin.deleted IS FALSE
ORDER BY server.id;

-- name: ListDirectRemoteAdmissionTargetsForGateway :many
-- Return every distinct direct-remote member target reached by one exact live
-- gateway. The gateway and its members are bound to the caller's organization
-- and project, and traversal stops at the gateway's immediate members.
SELECT DISTINCT
    member_server.id AS mcp_server_id,
    remote.url AS remote_url
FROM plugins AS plugin
JOIN meta_mcp_servers AS gateway
  ON gateway.id = @gateway_id
 AND gateway.project_id = plugin.project_id
 AND gateway.organization_id = plugin.organization_id
 AND gateway.deleted IS FALSE
JOIN meta_mcp_server_members AS member
  ON member.meta_mcp_server_id = gateway.id
 AND member.project_id = gateway.project_id
 AND member.deleted IS FALSE
JOIN mcp_servers AS member_server
  ON member_server.id = member.mcp_server_id
 AND member_server.project_id = gateway.project_id
 AND member_server.deleted IS FALSE
JOIN platform_mcp_catalog_registrations AS registration
  ON registration.mcp_server_id = member_server.id
 AND registration.organization_id = plugin.organization_id
 AND registration.project_id = gateway.project_id
 AND registration.catalog_provider = 'direct-remote-url-v1'
LEFT JOIN remote_mcp_servers AS remote
  ON remote.id = member_server.remote_mcp_server_id
 AND remote.project_id = member_server.project_id
 AND remote.deleted IS FALSE
WHERE plugin.id = @plugin_id
  AND plugin.organization_id = @organization_id
  AND plugin.project_id = @project_id
  AND plugin.deleted IS FALSE
ORDER BY member_server.id;

-- name: ListDirectRemoteAdmissionAudiencesForGateway :many
SELECT DISTINCT plugin.id AS plugin_id, assignment.principal_urn
FROM meta_mcp_servers gateway
JOIN plugin_servers attachment ON attachment.meta_mcp_server_id = gateway.id
  AND attachment.project_id = gateway.project_id AND attachment.deleted IS FALSE
JOIN plugins plugin ON plugin.id = attachment.plugin_id AND plugin.project_id = gateway.project_id
  AND plugin.organization_id = gateway.organization_id AND plugin.deleted IS FALSE
LEFT JOIN plugin_assignments assignment ON assignment.plugin_id = plugin.id
  AND assignment.organization_id = plugin.organization_id
WHERE gateway.id = @gateway_id AND gateway.project_id = @project_id
  AND gateway.organization_id = @organization_id AND gateway.deleted IS FALSE
ORDER BY plugin.id, assignment.principal_urn NULLS FIRST;

-- name: ListDirectRemoteAdmissionAudiencesForMCPServer :many
-- Return one row per live attachment/principal for a provenance-bound MCP. LEFT
-- joins preserve attached plugins with an empty audience and provenance-bound
-- MCPs with no attachment, which callers must distinguish from incomplete data.
SELECT
    plugin.id AS plugin_id,
    assignment.principal_urn
FROM platform_mcp_catalog_registrations AS registration
JOIN mcp_servers AS server
  ON server.id = registration.mcp_server_id
 AND server.project_id = registration.project_id
 AND server.deleted IS FALSE
LEFT JOIN meta_mcp_server_members AS member
  ON member.mcp_server_id = server.id
 AND member.project_id = registration.project_id
 AND member.deleted IS FALSE
LEFT JOIN meta_mcp_servers AS gateway
  ON gateway.id = member.meta_mcp_server_id
 AND gateway.project_id = member.project_id
 AND gateway.organization_id = registration.organization_id
 AND gateway.deleted IS FALSE
LEFT JOIN plugin_servers AS attachment
  ON (attachment.mcp_server_id = server.id OR attachment.meta_mcp_server_id = gateway.id)
 AND attachment.deleted IS FALSE
LEFT JOIN plugins AS plugin
  ON plugin.id = attachment.plugin_id
 AND plugin.organization_id = registration.organization_id
 AND plugin.project_id = registration.project_id
 AND plugin.deleted IS FALSE
LEFT JOIN plugin_assignments AS assignment
  ON assignment.plugin_id = plugin.id
 AND assignment.organization_id = registration.organization_id
WHERE registration.organization_id = @organization_id
  AND registration.project_id = @project_id
  AND registration.catalog_provider = 'direct-remote-url-v1'
  AND registration.mcp_server_id = @mcp_server_id
ORDER BY attachment.plugin_id NULLS FIRST, assignment.principal_urn NULLS FIRST;

-- name: ListDirectRemoteAdmissionTargetCandidates :many
-- Return provenance-bound direct-remote targets with live distributions for
-- exact canonical matching in Go. Registration lifecycle changes do not erase
-- durable provenance while the MCP and attachment remain live. Dashboard URL
-- edits can preserve noncanonical spelling that SQL must not reinterpret.
-- Callers page by the last mcp_server_id they read.
SELECT DISTINCT
    server.id AS mcp_server_id,
    remote.url AS remote_url
FROM platform_mcp_catalog_registrations AS registration
JOIN mcp_servers AS server
  ON server.id = registration.mcp_server_id
 AND server.project_id = registration.project_id
 AND server.deleted IS FALSE
JOIN remote_mcp_servers AS remote
  ON remote.id = server.remote_mcp_server_id
 AND remote.project_id = server.project_id
 AND remote.deleted IS FALSE
LEFT JOIN meta_mcp_server_members AS member
  ON member.mcp_server_id = server.id
 AND member.project_id = registration.project_id
 AND member.deleted IS FALSE
LEFT JOIN meta_mcp_servers AS gateway
  ON gateway.id = member.meta_mcp_server_id
 AND gateway.project_id = member.project_id
 AND gateway.organization_id = registration.organization_id
 AND gateway.deleted IS FALSE
JOIN plugin_servers AS attachment
  ON (attachment.mcp_server_id = server.id OR attachment.meta_mcp_server_id = gateway.id)
 AND attachment.deleted IS FALSE
JOIN plugins AS plugin
  ON plugin.id = attachment.plugin_id
 AND plugin.organization_id = registration.organization_id
 AND plugin.project_id = registration.project_id
 AND plugin.deleted IS FALSE
WHERE registration.organization_id = @organization_id
  AND registration.project_id = @project_id
  AND registration.catalog_provider = 'direct-remote-url-v1'
  AND (sqlc.narg(after_mcp_server_id)::uuid IS NULL OR server.id > sqlc.narg(after_mcp_server_id)::uuid)
ORDER BY server.id
LIMIT @page_limit;

-- name: ListDirectRemoteAdmissionMCPServersForRemote :many
-- A remote URL edit affects every provenance-bound MCP server currently backed
-- by that remote source. Audience expansion is evaluated separately for each
-- server using ListDirectRemoteAdmissionAudiencesForMCPServer.
SELECT DISTINCT server.id AS mcp_server_id
FROM platform_mcp_catalog_registrations AS registration
JOIN mcp_servers AS server
  ON server.id = registration.mcp_server_id
 AND server.project_id = registration.project_id
 AND server.deleted IS FALSE
WHERE registration.organization_id = @organization_id
  AND registration.project_id = @project_id
  AND registration.catalog_provider = 'direct-remote-url-v1'
  AND server.remote_mcp_server_id = @remote_mcp_server_id
ORDER BY server.id;

-- name: GetPlatformMCPOnboardingDistributionTarget :one
SELECT
    workflow.id AS workflow_id,
    workflow.selected_registration_id AS registration_id,
    project.id AS project_id,
    project.name AS project_name,
    project.slug AS project_slug,
    registration.mcp_server_id
FROM platform_mcp_onboarding_workflows AS workflow
JOIN projects AS project
  ON project.id = workflow.selected_project_id
 AND project.organization_id = workflow.organization_id
 AND project.deleted IS FALSE
JOIN platform_mcp_catalog_registrations AS registration
  ON registration.id = workflow.selected_registration_id
 AND registration.organization_id = workflow.organization_id
 AND registration.project_id = project.id
 AND registration.deleted IS FALSE
WHERE workflow.organization_id = @organization_id
  AND workflow.initiating_subject_urn = @initiating_subject_urn
  AND workflow.status = 'active'
  AND workflow.expires_at > clock_timestamp()
  AND registration.status = 'registered'
  AND registration.mcp_server_id IS NOT NULL;

-- name: HasPlatformMCPOrganizationSetupComplete :one
-- Setup completion counts an attached distribution to any live plugin. A
-- distribution whose plugin has since been deleted correctly does not count.
SELECT EXISTS (
    SELECT 1
    FROM platform_mcp_onboarding_milestones AS milestone
    WHERE milestone.organization_id = @organization_id
      AND milestone.milestone = 'first_value_achieved'
    UNION ALL
    SELECT 1
    FROM platform_mcp_distributions AS distribution
     JOIN platform_mcp_catalog_registrations AS registration
       ON registration.id = distribution.registration_id
      AND registration.organization_id = distribution.organization_id
      AND registration.project_id = distribution.project_id
      AND registration.deleted IS FALSE
     JOIN plugins AS plugin
       ON plugin.id = COALESCE(distribution.plugin_id, distribution.default_plugin_id)
      AND plugin.organization_id = distribution.organization_id
      AND plugin.project_id = distribution.project_id
      AND plugin.deleted IS FALSE
     WHERE distribution.organization_id = @organization_id
       AND distribution.state = 'attached'
) AS setup_complete;

-- name: HasAttachedPlatformMCPOnboardingDistributionForProject :one
-- An attached distribution to any live plugin in the project satisfies
-- onboarding distribution; the plugin must still exist.
SELECT EXISTS (
    SELECT 1
    FROM platform_mcp_distributions AS distribution
    JOIN platform_mcp_catalog_registrations AS registration
      ON registration.id = distribution.registration_id
     AND registration.organization_id = distribution.organization_id
     AND registration.project_id = distribution.project_id
     AND registration.deleted IS FALSE
     JOIN projects AS project
       ON project.id = distribution.project_id
      AND project.organization_id = distribution.organization_id
      AND project.deleted IS FALSE
     JOIN plugins AS plugin
       ON plugin.id = COALESCE(distribution.plugin_id, distribution.default_plugin_id)
      AND plugin.organization_id = distribution.organization_id
      AND plugin.project_id = distribution.project_id
      AND plugin.deleted IS FALSE
     WHERE distribution.organization_id = @organization_id
       AND distribution.project_id = @project_id
       AND distribution.state = 'attached'
      AND registration.status = 'registered'
      AND registration.mcp_server_id IS NOT NULL
);

-- name: LockPlatformMCPOnboardingWorkflow :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        format('%s:%s', @organization_id::text, @initiating_subject_urn::text),
        0
    )
);

-- name: ExpireActivePlatformMCPOnboardingWorkflow :execrows
UPDATE platform_mcp_onboarding_workflows
SET status = 'expired',
    closed_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND initiating_subject_urn = @initiating_subject_urn
  AND status = 'active'
  AND expires_at <= clock_timestamp();

-- name: GetActivePlatformMCPOnboardingWorkflow :one
SELECT *
FROM platform_mcp_onboarding_workflows
WHERE organization_id = @organization_id
  AND initiating_subject_urn = @initiating_subject_urn
  AND status = 'active'
  AND expires_at > clock_timestamp()
ORDER BY updated_at DESC, id DESC
LIMIT 1;

-- name: CreatePlatformMCPOnboardingWorkflow :one
INSERT INTO platform_mcp_onboarding_workflows (
    organization_id,
    initiating_subject_urn,
    source_surface,
    client_family,
    expires_at
) VALUES (
    @organization_id,
    @initiating_subject_urn,
    @source_surface,
    @client_family,
    @expires_at
)
RETURNING *;

-- name: RecordPlatformMCPOnboardingInstallIntent :one
UPDATE platform_mcp_onboarding_workflows
SET client_family = @client_family,
    expires_at = @expires_at,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND initiating_subject_urn = @initiating_subject_urn
  AND status = 'active'
  AND expires_at > clock_timestamp()
RETURNING *;

-- name: RecordPlatformMCPOnboardingInstallStarted :execrows
INSERT INTO platform_mcp_onboarding_milestones (
    organization_id,
    milestone,
    attempt_id
)
SELECT
    @organization_id,
    'install_started',
    @attempt_id
WHERE EXISTS (
    SELECT 1
    FROM platform_mcp_onboarding_workflows AS workflow
    WHERE workflow.id = @attempt_id
      AND workflow.organization_id = @organization_id
      AND workflow.initiating_subject_urn = @initiating_subject_urn
      AND workflow.status = 'active'
      AND workflow.expires_at > clock_timestamp()
)
ON CONFLICT DO NOTHING;

-- name: HasPlatformMCPOnboardingInstallStarted :one
SELECT EXISTS (
    SELECT 1
    FROM platform_mcp_onboarding_milestones AS milestone
    JOIN platform_mcp_onboarding_workflows AS workflow
      ON workflow.organization_id = milestone.organization_id
     AND workflow.id = milestone.attempt_id
    WHERE milestone.organization_id = @organization_id
      AND milestone.milestone = 'install_started'
      AND milestone.attempt_id = @attempt_id
      AND workflow.initiating_subject_urn = @initiating_subject_urn
      AND workflow.status = 'active'
      AND workflow.expires_at > clock_timestamp()
);

-- name: RecordPlatformMCPDashboardCtaEvent :execrows
INSERT INTO platform_mcp_onboarding_milestones (
    organization_id,
    milestone,
    attempt_id
) VALUES (
    @organization_id,
    @milestone,
    @attempt_id
)
ON CONFLICT DO NOTHING;

-- name: RecordPlatformMCPOnboardingAgentConfigurationCopied :one
UPDATE platform_mcp_onboarding_workflows
SET agent_configuration_copied_at = COALESCE(agent_configuration_copied_at, clock_timestamp()),
    expires_at = @expires_at,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND initiating_subject_urn = @initiating_subject_urn
  AND status = 'active'
  AND expires_at > clock_timestamp()
RETURNING *;

-- name: HasPlatformMCPOnboardingCatalogExplored :one
SELECT EXISTS (
    SELECT 1
    FROM platform_mcp_onboarding_milestones AS milestone
    WHERE milestone.organization_id = @organization_id
      AND milestone.milestone = 'catalog_explored'
      AND milestone.connection_id = @connection_id
      AND milestone.connection_generation = @connection_generation
);

-- name: RecordPlatformMCPCatalogExplored :execrows
INSERT INTO platform_mcp_onboarding_milestones (
    organization_id,
    milestone,
    connection_id,
    connection_generation
)
SELECT
    @organization_id,
    'catalog_explored',
    @connection_id,
    @connection_generation
WHERE EXISTS (
    SELECT 1
    FROM platform_mcp_connections AS connection
    WHERE connection.id = @connection_id
      AND connection.organization_id = @organization_id
      AND connection.active_generation = @connection_generation
      AND connection.revoked_at IS NULL
)
ON CONFLICT (milestone, connection_id, connection_generation)
WHERE connection_id IS NOT NULL
  AND connection_generation IS NOT NULL
  AND milestone IN (
    'authorization_succeeded',
    'authorization_failed',
    'connection_ready',
    'catalog_explored',
    'first_read_succeeded',
    'first_write_succeeded',
    'read_only_cohort'
)
DO NOTHING;

-- A surface acting under assistant identity holds no connection, so its
-- evidence is keyed by the acting user and dedupes on the user grain instead of
-- the connection generation.
-- name: RecordPlatformMCPCatalogExploredForUser :execrows
INSERT INTO platform_mcp_onboarding_milestones (
    organization_id,
    milestone,
    user_id,
    acting_surface
)
VALUES (
    @organization_id,
    'catalog_explored',
    @user_id,
    @acting_surface
)
ON CONFLICT (organization_id, milestone, user_id)
WHERE connection_id IS NULL
  AND connection_generation IS NULL
  AND user_id IS NOT NULL
  AND milestone IN (
    'authorization_succeeded',
    'authorization_failed',
    'connection_ready',
    'catalog_explored',
    'first_read_succeeded',
    'first_write_succeeded',
    'read_only_cohort'
)
DO NOTHING;

-- name: BindPlatformMCPOnboardingRegistration :one
UPDATE platform_mcp_onboarding_workflows
SET selected_project_id = @selected_project_id,
    selected_registration_id = @selected_registration_id,
    updated_at = clock_timestamp()
WHERE platform_mcp_onboarding_workflows.id = @id
  AND platform_mcp_onboarding_workflows.organization_id = @organization_id
  AND platform_mcp_onboarding_workflows.initiating_subject_urn = @initiating_subject_urn
  AND platform_mcp_onboarding_workflows.status = 'active'
  AND platform_mcp_onboarding_workflows.expires_at > clock_timestamp()
  AND EXISTS (
      SELECT 1
      FROM projects AS project
      JOIN platform_mcp_catalog_registrations AS registration
        ON registration.id = @selected_registration_id
       AND registration.organization_id = project.organization_id
       AND registration.project_id = project.id
       AND registration.deleted IS FALSE
      WHERE project.id = @selected_project_id
        AND project.organization_id = @organization_id
        AND project.deleted IS FALSE
  )
RETURNING *;

-- name: GetPlatformMCPOnboardingSelectedProject :one
SELECT project.id, project.name, project.slug
FROM platform_mcp_onboarding_workflows AS workflow
JOIN projects AS project
  ON project.id = workflow.selected_project_id
 AND project.organization_id = workflow.organization_id
 AND project.deleted IS FALSE
WHERE workflow.id = @workflow_id
  AND workflow.organization_id = @organization_id
  AND workflow.initiating_subject_urn = @initiating_subject_urn
  AND workflow.status = 'active'
  AND workflow.expires_at > clock_timestamp();

-- name: CloseActivePlatformMCPOnboardingWorkflow :one
UPDATE platform_mcp_onboarding_workflows
SET status = @status,
    closed_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND initiating_subject_urn = @initiating_subject_urn
  AND status = 'active'
RETURNING *;

-- name: ListPlatformMCPSubjectConnections :many
SELECT
    connection.id,
    connection.active_generation,
    connection.authorized_at,
    connection.reauthorized_at,
    EXISTS (
        SELECT 1
        FROM platform_mcp_onboarding_milestones AS milestone
        WHERE milestone.organization_id = connection.organization_id
          AND milestone.milestone = 'connection_ready'
          AND milestone.connection_id = connection.id
          AND milestone.connection_generation = connection.active_generation
    ) AS ready
FROM platform_mcp_connections AS connection
JOIN platform_mcp_oauth_clients AS client
  ON client.id = connection.oauth_client_id
JOIN LATERAL (
    SELECT session.refresh_expires_at, session.revoked_at
    FROM platform_mcp_sessions AS session
    WHERE session.organization_id = connection.organization_id
      AND session.connection_id = connection.id
      AND session.connection_generation = connection.active_generation
    ORDER BY session.created_at DESC, session.id DESC
    LIMIT 1
) AS latest_session ON TRUE
WHERE connection.organization_id = @organization_id
  AND connection.subject_urn = @subject_urn
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND client.revoked_at IS NULL
  AND COALESCE(
      connection.authorization_expires_at,
      COALESCE(connection.reauthorized_at, connection.authorized_at) + INTERVAL '90 days'
  ) > @now
  AND latest_session.revoked_at IS NULL
  AND latest_session.refresh_expires_at > @now
ORDER BY COALESCE(connection.reauthorized_at, connection.authorized_at) DESC, connection.id DESC;

-- name: GetPlatformMCPSubjectConnectionAuthState :one
SELECT
    connection.id,
    connection.active_generation,
    connection.authorized_at,
    connection.reauthorized_at,
    connection.reauthorization_required_at,
    connection.reauthorization_reason,
    connection.revoked_at,
    client.revoked_at AS client_revoked_at,
    COALESCE(
        connection.authorization_expires_at,
        COALESCE(connection.reauthorized_at, connection.authorized_at) + INTERVAL '90 days'
    )::timestamptz AS effective_authorization_expires_at,
    latest_session.refresh_expires_at AS latest_refresh_expires_at,
    latest_session.revoked_at AS latest_session_revoked_at,
    EXISTS (
        SELECT 1
        FROM platform_mcp_onboarding_milestones AS milestone
        WHERE milestone.organization_id = connection.organization_id
          AND milestone.milestone = 'connection_ready'
          AND milestone.connection_id = connection.id
          AND milestone.connection_generation = connection.active_generation
    ) AS ready
FROM platform_mcp_connections AS connection
JOIN platform_mcp_oauth_clients AS client
  ON client.id = connection.oauth_client_id
LEFT JOIN LATERAL (
    SELECT session.refresh_expires_at, session.revoked_at
    FROM platform_mcp_sessions AS session
    WHERE session.organization_id = connection.organization_id
      AND session.connection_id = connection.id
      AND session.connection_generation = connection.active_generation
    ORDER BY session.created_at DESC, session.id DESC
    LIMIT 1
) AS latest_session ON TRUE
WHERE connection.organization_id = @organization_id
  AND connection.subject_urn = @subject_urn
ORDER BY COALESCE(connection.reauthorized_at, connection.authorized_at) DESC, connection.id DESC
LIMIT 1;

-- name: RecordPlatformMCPSetupMilestone :exec
INSERT INTO platform_mcp_onboarding_milestones (
    organization_id,
    milestone,
    connection_id,
    connection_generation,
    project_id,
    mcp_key,
    attempt_id
) VALUES (
    @organization_id,
    @milestone,
    @connection_id,
    @connection_generation,
    @project_id,
    @mcp_key,
    @attempt_id
)
ON CONFLICT DO NOTHING;

-- name: HasPlatformMCPOnboardingRegistrationSucceeded :one
SELECT EXISTS (
    SELECT 1
    FROM platform_mcp_onboarding_milestones AS milestone
    JOIN platform_mcp_onboarding_workflows AS workflow
      ON workflow.organization_id = milestone.organization_id
     AND workflow.selected_project_id = milestone.project_id
     AND workflow.selected_registration_id = milestone.attempt_id
     AND workflow.initiating_subject_urn = @initiating_subject_urn
     AND workflow.status = 'active'
     AND workflow.expires_at > clock_timestamp()
    WHERE milestone.organization_id = @organization_id
      AND milestone.milestone = 'registration_succeeded'
      AND milestone.connection_id = @connection_id
      AND milestone.connection_generation = @connection_generation
);

-- name: HasPlatformMCPOnboardingDistributionSucceeded :one
SELECT EXISTS (
    SELECT 1
    FROM platform_mcp_onboarding_milestones AS milestone
    JOIN platform_mcp_onboarding_workflows AS workflow
      ON workflow.organization_id = milestone.organization_id
     AND workflow.selected_project_id = milestone.project_id
     AND workflow.selected_registration_id = milestone.attempt_id
     AND workflow.initiating_subject_urn = @initiating_subject_urn
     AND workflow.status = 'active'
     AND workflow.expires_at > clock_timestamp()
    WHERE milestone.organization_id = @organization_id
      AND milestone.milestone = 'distribution_succeeded'
      AND milestone.connection_id = @connection_id
      AND milestone.connection_generation = @connection_generation
);

-- name: HasPlatformMCPOnboardingReadinessVerified :one
SELECT EXISTS (
    SELECT 1
    FROM platform_mcp_onboarding_milestones AS milestone
    JOIN platform_mcp_onboarding_workflows AS workflow
      ON workflow.organization_id = milestone.organization_id
     AND workflow.selected_project_id = milestone.project_id
     AND workflow.selected_registration_id = milestone.attempt_id
     AND workflow.initiating_subject_urn = @initiating_subject_urn
     AND workflow.status = 'active'
     AND workflow.expires_at > clock_timestamp()
    WHERE milestone.organization_id = @organization_id
      AND milestone.milestone = 'readiness_verified'
      AND milestone.connection_id = @connection_id
      AND milestone.connection_generation = @connection_generation
);

-- name: HasPlatformMCPOnboardingLifecycleMilestone :one
SELECT EXISTS (
    SELECT 1
    FROM platform_mcp_onboarding_milestones AS milestone
    WHERE milestone.organization_id = @organization_id
      AND milestone.milestone = @milestone
      AND milestone.connection_id = @connection_id
      AND milestone.connection_generation = @connection_generation
      AND milestone.project_id = @project_id
      AND milestone.attempt_id = @attempt_id
);

-- name: RecordPlatformMCPOnboardingLifecycleMilestone :execrows
INSERT INTO platform_mcp_onboarding_milestones (
    organization_id,
    milestone,
    connection_id,
    connection_generation,
    project_id,
    attempt_id
)
SELECT
    @organization_id,
    @milestone,
    @connection_id,
    @connection_generation,
    @project_id,
    @attempt_id
WHERE EXISTS (
    SELECT 1
    FROM projects AS project
    JOIN platform_mcp_catalog_registrations AS registration
      ON registration.id = @attempt_id
     AND registration.organization_id = project.organization_id
     AND registration.project_id = project.id
     AND registration.deleted IS FALSE
    JOIN platform_mcp_connections AS connection
      ON connection.id = @connection_id
     AND connection.organization_id = project.organization_id
     AND connection.active_generation = @connection_generation
     AND connection.revoked_at IS NULL
    WHERE project.id = @project_id
      AND project.organization_id = @organization_id
      AND project.deleted IS FALSE
)
ON CONFLICT DO NOTHING;

-- name: RecordPlatformMCPRegistrationSucceeded :exec
INSERT INTO platform_mcp_onboarding_milestones (
    organization_id,
    milestone,
    connection_id,
    connection_generation,
    project_id,
    mcp_key,
    attempt_id
) VALUES (
    @organization_id,
    'registration_succeeded',
    @connection_id,
    @connection_generation,
    @project_id,
    @mcp_key,
    @attempt_id
)
ON CONFLICT DO NOTHING;

-- Skill distribution targets. A skill is distributed to an exact existing
-- plugin or assistant in one project; these reads name what exists so the
-- resolver can refuse a target that does not, rather than falling back to the
-- default plugin.

-- name: ListPlatformMCPProjectPlugins :many
SELECT
    plugins.id,
    plugins.name,
    plugins.slug,
    COALESCE(plugins.is_default, FALSE) AS is_default
FROM plugins
JOIN projects
  ON projects.id = plugins.project_id
WHERE plugins.project_id = @project_id
  AND plugins.organization_id = @organization_id
  AND projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND plugins.deleted IS FALSE
ORDER BY plugins.is_default DESC NULLS LAST, plugins.name ASC
LIMIT @result_limit;

-- name: ListPlatformMCPProjectAssistants :many
SELECT
    assistants.id,
    assistants.name
FROM assistants
JOIN projects
  ON projects.id = assistants.project_id
WHERE assistants.project_id = @project_id
  AND assistants.organization_id = @organization_id
  AND projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND assistants.deleted IS FALSE
  AND assistants.status = 'active'
ORDER BY assistants.name ASC
LIMIT @result_limit;

-- name: ListPlatformMCPServerIdentities :many
-- Lists the names an agent hook can report one configured MCP server under,
-- for every live MCP server in one of the organization's projects: its id,
-- slug, name, hosted toolset slug, and each plugin membership's plugin slug and
-- display name (the key the plugin's mcp.json ships it under). One row per
-- (server, membership); a server with no membership yields one row with empty
-- plugin columns. Hosted servers are also reached through memberships attached
-- by toolset, so those memberships are included for the server fronting that
-- toolset. Memberships are gathered from the project's live plugins through the
-- (plugin_id, backend) indexes, one arm per backend kind: a live membership has
-- exactly one backend, so the arms never repeat a row.
WITH server AS (
    SELECT
        m.id,
        m.toolset_id,
        COALESCE(m.name, '')::text AS mcp_name,
        COALESCE(m.slug, '')::text AS mcp_slug,
        COALESCE(toolset.slug, '')::text AS toolset_slug
    FROM mcp_servers AS m
    JOIN projects AS project
      ON project.id = m.project_id
     AND project.organization_id = @organization_id
     AND project.deleted IS FALSE
    LEFT JOIN toolsets AS toolset
      ON toolset.id = m.toolset_id
     AND toolset.project_id = m.project_id
     AND toolset.deleted IS FALSE
    WHERE m.project_id = @project_id
      AND m.deleted IS FALSE
),
membership AS (
    SELECT
        plugin_server.id AS membership_id,
        plugin_server.mcp_server_id,
        plugin_server.toolset_id,
        plugin.slug AS plugin_slug,
        plugin_server.display_name
    FROM plugins AS plugin
    JOIN plugin_servers AS plugin_server
      ON plugin_server.plugin_id = plugin.id
     AND plugin_server.deleted IS FALSE
    WHERE plugin.project_id = @project_id
      AND plugin.deleted IS FALSE
),
server_membership AS (
    SELECT
        membership.mcp_server_id AS server_id,
        membership.membership_id,
        membership.plugin_slug,
        membership.display_name
    FROM membership
    WHERE membership.mcp_server_id IS NOT NULL
    UNION ALL
    SELECT
        server.id AS server_id,
        membership.membership_id,
        membership.plugin_slug,
        membership.display_name
    FROM server
    JOIN membership
      ON membership.toolset_id = server.toolset_id
    WHERE server.toolset_id IS NOT NULL
)
SELECT
    server.id AS mcp_server_id,
    server.mcp_name,
    server.mcp_slug,
    server.toolset_slug,
    COALESCE(server_membership.plugin_slug, '')::text AS plugin_slug,
    COALESCE(server_membership.display_name, '')::text AS plugin_display_name
FROM server
LEFT JOIN server_membership
  ON server_membership.server_id = server.id
ORDER BY server.id, server_membership.membership_id;

-- Session recall (list_my_sessions / continue_session). Every read below
-- fuses tenancy and ownership into the row filter — organization, owner
-- user_id, not-deleted, and the personal-account exclusion — rather than
-- fetching then authorizing. An unresolved actor (empty user_id) matches no
-- rows because chats.user_id is never the empty string: fail-closed.
-- Personal-account sessions are excluded from BOTH list and continue:
-- personal-account ownership attribution is partly device-bridge-inferred,
-- which is acceptable for titles but not for transcripts (see the
-- ListOwnedChatSessionMeta warning in agent/queries.sql). The predicate
-- (ua.id IS NULL OR ua.account_type <> 'personal') also drops rows whose
-- account_type is NULL — the comparison evaluates to NULL — and that is
-- deliberate: an unclassified account might be personal, so it gets the
-- same fail-closed treatment.

-- name: ListOwnedChatSessionsForRecall :many
SELECT c.id, c.external_chat_id, c.title, c.summary, c.cwd, c.updated_at, c.project_id, p.name AS project_name, p.slug AS project_slug
FROM chats c
JOIN projects p ON p.id = c.project_id
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE c.organization_id = @organization_id
  AND c.user_id = @user_id::text
  AND c.deleted IS FALSE
  AND (ua.id IS NULL OR ua.account_type <> 'personal')
ORDER BY c.updated_at DESC
LIMIT @row_limit;

-- name: GetOwnedChatForRecall :one
SELECT c.id, c.external_chat_id, c.title, c.cwd, c.updated_at, c.project_id
FROM chats c
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE c.id = @chat_id
  AND c.organization_id = @organization_id
  AND c.user_id = @user_id::text
  AND c.deleted IS FALSE
  AND (ua.id IS NULL OR ua.account_type <> 'personal');

-- name: ListOwnedChatTranscriptMessagesForRecall :many
-- Latest generation only: compaction/edit rewrites bump chat_messages.generation
-- and the digest must reflect the current conversation view, not superseded
-- rows. chat_messages.project_id is NULL on old rows — always filtered, so
-- pre-project-stamp rows fail closed rather than leaking across tenants.
-- Newest rows first under @row_limit so the cap keeps the end of a long
-- session — the part a handoff digest is about — and the service restores
-- chronological order. Content is never truncated here: finding-span
-- verification compares exact bytes, so per-message bounding happens after
-- masking, not at the read.
SELECT cm.id, cm.seq, cm.created_at, cm.role, cm.content, cm.content_asset_url, cm.tool_calls, cm.tool_call_id, cm.tool_urn, cm.source, cm.risk_analyzed_at
FROM chat_messages cm
JOIN chats c ON c.id = cm.chat_id
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE cm.chat_id = @chat_id
  AND cm.project_id = @project_id
  AND c.organization_id = @organization_id
  AND c.user_id = @user_id::text
  AND c.deleted IS FALSE
  AND (ua.id IS NULL OR ua.account_type <> 'personal')
  AND cm.generation = (
    SELECT COALESCE(MAX(generation), 0)
    FROM chat_messages
    WHERE chat_id = @chat_id
      AND project_id = @project_id
  )
ORDER BY cm.created_at DESC, cm.seq DESC
LIMIT @row_limit;

-- name: ListRiskFindingSpansForRecall :many
-- Findings that drive inline masking of the recall digest. Message-anchored
-- rows only (the digest does not render content parts): found, not excluded,
-- not swept as false positive, policy still enabled and not deleted.
-- Latest generation only, matching the transcript read: findings on
-- superseded generations mask nothing the digest renders, so loading them
-- would only let long, repeatedly compacted sessions inflate the scan.
SELECT rr.chat_message_id, rr.source, rr.rule_id, rr.match, rr.spans, rr.start_pos, rr.end_pos
FROM risk_results rr
JOIN chat_messages cm ON cm.id = rr.chat_message_id
JOIN chats c ON c.id = cm.chat_id
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
JOIN risk_policies rp ON rp.id = rr.risk_policy_id AND rp.deleted IS FALSE AND rp.enabled IS TRUE
WHERE cm.chat_id = @chat_id
  AND rr.project_id = @project_id
  AND c.organization_id = @organization_id
  AND c.user_id = @user_id::text
  AND c.deleted IS FALSE
  AND (ua.id IS NULL OR ua.account_type <> 'personal')
  AND cm.generation = (
    SELECT COALESCE(MAX(generation), 0)
    FROM chat_messages
    WHERE chat_id = @chat_id
      AND project_id = @project_id
  )
  AND rr.found IS TRUE AND rr.excluded_at IS NULL AND rr.false_positive_at IS NULL
ORDER BY cm.created_at ASC, cm.seq ASC, rr.id ASC;

-- name: InsertChatSessionRecallLink :exec
-- Sibling of agent's InsertChatSessionLink with kind='recall'. A v1 recall
-- edge always has a NULL child: the OAuth principal carries no harness
-- session id, so the continuation is unknowable at recall time and each
-- recall records a distinct event. The ON CONFLICT clause is therefore inert
-- today (the partial unique index only covers non-NULL children) and kept
-- verbatim for forward safety.
INSERT INTO chat_session_links (
  project_id, organization_id, parent_chat_id, child_chat_id,
  parent_session_id, child_session_id, kind, target_harness, source_surface,
  actor_email, device_serial, device_hostname
) VALUES (
  @project_id, @organization_id, @parent_chat_id, @child_chat_id,
  @parent_session_id, @child_session_id, 'recall', @target_harness, @source_surface,
  @actor_email, @device_serial, @device_hostname
)
ON CONFLICT (project_id, parent_chat_id, child_chat_id) WHERE child_chat_id IS NOT NULL DO NOTHING;
-- Plugin inventory. Plugins are the unit an administrator installs and reasons
-- about, so this surface reads them directly rather than inferring them from
-- distribution targets. Membership is derived from plugin_servers and
-- skill_distributions, which are the attachment authority; nothing here is a
-- stored projection that could drift from them.

-- name: ListPlatformMCPPluginInventory :many
-- Keyset page over a project's plugins. Assignment principals are counted by
-- kind and never projected: a principal URN embeds a user id, which this
-- surface must not carry.
SELECT
    p.id,
    p.name,
    p.slug,
    p.description,
    p.auto_created,
    COALESCE(p.is_default, FALSE) AS is_default,
    (SELECT count(*) FROM plugin_servers ps WHERE ps.plugin_id = p.id AND ps.deleted IS FALSE) AS server_count,
    (
      SELECT count(*)
      FROM skill_distributions sd
      JOIN skills sk
        ON sk.id = sd.skill_id
        AND sk.project_id = sd.project_id
        AND sk.archived_at IS NULL
      WHERE sd.plugin_id = p.id
        AND sd.project_id = p.project_id
        AND sd.channel = 'plugin'
        AND sd.assistant_id IS NULL
        AND sd.revoked_at IS NULL
    ) AS skill_count,
    COALESCE(assignment_counts.wildcard_count, 0)::bigint AS wildcard_assignment_count,
    COALESCE(assignment_counts.role_count, 0)::bigint AS role_assignment_count,
    COALESCE(assignment_counts.user_count, 0)::bigint AS user_assignment_count,
    (gc.id IS NOT NULL)::boolean AS repository_connected,
    (COALESCE(gc.published_mcp_fingerprints ->> p.slug, '') <> '')::boolean AS published
FROM plugins p
JOIN projects
  ON projects.id = p.project_id
LEFT JOIN plugin_github_connections gc
  ON gc.project_id = p.project_id
LEFT JOIN LATERAL (
  SELECT
    count(*) FILTER (WHERE pa.principal_urn = '*') AS wildcard_count,
    count(*) FILTER (WHERE pa.principal_urn LIKE 'role:%') AS role_count,
    count(*) FILTER (WHERE pa.principal_urn LIKE 'user:%') AS user_count
  FROM plugin_assignments pa
  JOIN plugins assignment_plugin
    ON assignment_plugin.id = pa.plugin_id
    AND assignment_plugin.organization_id = pa.organization_id
    AND assignment_plugin.project_id = @project_id
    AND assignment_plugin.deleted IS FALSE
  JOIN projects assignment_project
    ON assignment_project.id = assignment_plugin.project_id
    AND assignment_project.organization_id = assignment_plugin.organization_id
    AND assignment_project.deleted IS FALSE
  WHERE pa.plugin_id = p.id
    AND pa.organization_id = @organization_id
) assignment_counts ON TRUE
WHERE p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND p.deleted IS FALSE
  AND (NOT @use_after::boolean OR p.id > @after_id)
ORDER BY p.id ASC
LIMIT @result_limit;

-- name: ListPlatformMCPAssignedPluginInventory :many
-- Member-facing plugin inventory. A row is visible only when the package is
-- published and at least one current assignment matches the authenticated
-- caller's server-resolved principals. Assignment identities and counts never
-- cross this query boundary.
SELECT
    p.id,
    p.name,
    p.slug,
    p.description,
    p.auto_created,
    COALESCE(p.is_default, FALSE) AS is_default,
    (SELECT count(*) FROM plugin_servers ps WHERE ps.plugin_id = p.id AND ps.deleted IS FALSE) AS server_count,
    (
      SELECT count(*)
      FROM skill_distributions sd
      JOIN skills sk
        ON sk.id = sd.skill_id
        AND sk.project_id = sd.project_id
        AND sk.archived_at IS NULL
      WHERE sd.plugin_id = p.id
        AND sd.project_id = p.project_id
        AND sd.channel = 'plugin'
        AND sd.assistant_id IS NULL
        AND sd.revoked_at IS NULL
    ) AS skill_count,
    (gc.id IS NOT NULL)::boolean AS repository_connected,
    TRUE::boolean AS published
FROM plugins p
JOIN projects
  ON projects.id = p.project_id
  AND projects.organization_id = p.organization_id
  AND projects.deleted IS FALSE
JOIN plugin_github_connections gc
  ON gc.project_id = p.project_id
  AND gc.marketplace_token IS NOT NULL
WHERE p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND p.deleted IS FALSE
  AND COALESCE(gc.published_mcp_fingerprints ->> p.slug, '') <> ''
  AND EXISTS (
    SELECT 1
    FROM plugin_assignments pa
    WHERE pa.plugin_id = p.id
      AND pa.organization_id = @organization_id
      AND pa.principal_urn = ANY(@principal_urns::text[])
  )
  AND (NOT @use_after::boolean OR p.id > @after_id)
ORDER BY p.id ASC
LIMIT @result_limit;

-- name: ResolvePlatformMCPAssignedPluginTarget :many
-- Exact member target resolution over the same assigned, published set as the
-- member list. Missing, unpublished, unassigned, and cross-tenant targets all
-- collapse to the same not-found result.
SELECT
    p.id,
    p.name,
    p.slug,
    p.description,
    p.auto_created,
    COALESCE(p.is_default, FALSE) AS is_default,
    (SELECT count(*) FROM plugin_servers ps WHERE ps.plugin_id = p.id AND ps.deleted IS FALSE) AS server_count,
    (
      SELECT count(*)
      FROM skill_distributions sd
      JOIN skills sk
        ON sk.id = sd.skill_id
        AND sk.project_id = sd.project_id
        AND sk.archived_at IS NULL
      WHERE sd.plugin_id = p.id
        AND sd.project_id = p.project_id
        AND sd.channel = 'plugin'
        AND sd.assistant_id IS NULL
        AND sd.revoked_at IS NULL
    ) AS skill_count
FROM plugins p
JOIN projects
  ON projects.id = p.project_id
  AND projects.organization_id = p.organization_id
  AND projects.deleted IS FALSE
JOIN plugin_github_connections gc
  ON gc.project_id = p.project_id
  AND gc.marketplace_token IS NOT NULL
WHERE p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND p.deleted IS FALSE
  AND COALESCE(gc.published_mcp_fingerprints ->> p.slug, '') <> ''
  AND EXISTS (
    SELECT 1
    FROM plugin_assignments pa
    WHERE pa.plugin_id = p.id
      AND pa.organization_id = @organization_id
      AND pa.principal_urn = ANY(@principal_urns::text[])
  )
  AND (
    p.id::text = @target::text
    OR lower(p.slug) = lower(@target::text)
    OR lower(p.name) = lower(@target::text)
  )
ORDER BY p.id
LIMIT 2;

-- name: GetPlatformMCPInstallTarget :one
-- Tenant-scoped exact MCP target plus its canonical public endpoint. Disabled
-- and unproxied servers deliberately expose no endpoint even if an endpoint row
-- remains, because neither can be dispatched through Speakeasy's public MCP route.
SELECT
    m.name,
    m.slug,
    CASE
      WHEN m.visibility <> 'disabled'
        AND m.unproxied_mcp_server_id IS NULL
      THEN COALESCE(endpoint.slug, '')
      ELSE ''
    END::text AS endpoint_slug
FROM mcp_servers m
JOIN projects project
  ON project.id = m.project_id
  AND project.organization_id = @organization_id
  AND project.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT e.slug
  FROM mcp_endpoints e
  WHERE e.mcp_server_id = m.id
    AND e.project_id = m.project_id
    AND e.custom_domain_id IS NULL
    AND e.deleted IS FALSE
  ORDER BY e.created_at ASC, e.id ASC
  LIMIT 1
) endpoint ON TRUE
WHERE m.id = @mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE;

-- name: GetPlatformMCPConnectionSettings :one
-- One exact organization/project-scoped target and its connection dependencies.
-- Deliberately returns no upstream URL, credentials, or provider resource data.
SELECT
    CASE WHEN @target_kind::text = 'gateway' THEN gateway.name ELSE COALESCE(server.name, server.slug, '') END::text AS name,
    CASE WHEN @target_kind::text = 'gateway' THEN gateway.visibility ELSE server.visibility END::text AS visibility,
    COALESCE(
      CASE WHEN @target_kind::text = 'gateway' THEN gateway.network_access_mode ELSE server.network_access_mode END,
      'public_only'
    )::text AS network_mode,
    COALESCE(endpoints.items, '[]'::jsonb) AS endpoints,
    ingress.state AS ingress,
    COALESCE(memberships.items, '[]'::jsonb) AS plugin_memberships
FROM projects project
LEFT JOIN mcp_servers server
  ON @target_kind::text = 'mcp_server'
 AND server.id = @target_id
 AND server.project_id = project.id
 AND server.deleted IS FALSE
LEFT JOIN meta_mcp_servers gateway
  ON @target_kind::text = 'gateway'
 AND gateway.id = @target_id
 AND gateway.project_id = project.id
 AND gateway.organization_id = project.organization_id
 AND gateway.deleted IS FALSE
LEFT JOIN LATERAL (
    SELECT jsonb_agg(jsonb_build_object(
        'id', endpoint.id,
        'slug', endpoint.slug,
        'custom_domain_id', endpoint.custom_domain_id,
        'domain', domain.domain,
        'is_domain_root', COALESCE(endpoint.is_domain_root, FALSE)
    ) ORDER BY endpoint.id) AS items
    FROM mcp_endpoints endpoint
    LEFT JOIN custom_domains domain
      ON domain.id = endpoint.custom_domain_id
     AND domain.organization_id = @organization_id
     AND domain.deleted IS FALSE
    WHERE endpoint.project_id = project.id
      AND endpoint.deleted IS FALSE
      AND ((@target_kind::text = 'mcp_server' AND endpoint.mcp_server_id = server.id)
        OR (@target_kind::text = 'gateway' AND endpoint.meta_mcp_server_id = gateway.id))
) endpoints ON TRUE
LEFT JOIN LATERAL (
    SELECT jsonb_build_object(
        'enabled', ingress.enabled,
        'namespace_kind', ingress.endpoint_namespace_kind,
        'hostname', ingress.hostname,
        'custom_domain_id', ingress.custom_domain_id,
        'status', ingress.status,
        'dns_name', ingress.dns_name
    ) AS state
    FROM network_ingresses ingress
    WHERE ingress.organization_id = @organization_id
      AND ingress.deleted IS FALSE
    ORDER BY ingress.id
    LIMIT 1
) ingress ON TRUE
LEFT JOIN LATERAL (
    SELECT jsonb_agg(jsonb_build_object(
        'id', membership.id,
        'plugin_id', plugin.id,
        'plugin_slug', plugin.slug,
        'display_name', membership.display_name,
        'policy', membership.policy,
        'sort_order', membership.sort_order
    ) ORDER BY plugin.id, membership.id) AS items
    FROM plugin_servers membership
    JOIN plugins plugin
      ON plugin.id = membership.plugin_id
     AND plugin.project_id = project.id
     AND plugin.organization_id = @organization_id
     AND plugin.deleted IS FALSE
    WHERE membership.deleted IS FALSE
      AND ((@target_kind::text = 'mcp_server' AND membership.mcp_server_id = server.id)
        OR (@target_kind::text = 'gateway' AND membership.meta_mcp_server_id = gateway.id))
) memberships ON TRUE
WHERE project.id = @project_id
  AND project.organization_id = @organization_id
  AND project.deleted IS FALSE
  AND ((@target_kind::text = 'mcp_server' AND server.id IS NOT NULL)
    OR (@target_kind::text = 'gateway' AND gateway.id IS NOT NULL));

-- name: GetPlatformMCPNetworkIngressEntitlement :one
-- Mirrors the uncached product feature check so a status read reflects the
-- live private-network entitlement.
SELECT EXISTS (
    SELECT 1
    FROM organization_features feature
    WHERE feature.organization_id = @organization_id
      AND feature.feature_name = @feature_name
      AND feature.deleted IS FALSE
) AS entitled;

-- name: GetPlatformMCPActiveNetworkIngress :one
-- The organization's active private network ingress. Deliberately omits
-- provider credentials, provider resources, and attestor identities.
SELECT
    ingress.provider,
    ingress.hostname,
    ingress.endpoint_namespace_kind,
    ingress.custom_domain_id,
    ingress.enabled,
    ingress.identity_required,
    (ingress.credentials_encrypted IS NOT NULL)::boolean AS credentials_configured,
    ingress.status,
    ingress.dns_name,
    ingress.last_error,
    ingress.health_checked_at,
    ingress.connected_since
FROM network_ingresses ingress
WHERE ingress.organization_id = @organization_id
  AND ingress.deleted IS FALSE
ORDER BY ingress.id
LIMIT 1;

-- name: GetPlatformMCPPluginInventoryItem :one
SELECT
    p.id,
    p.name,
    p.slug,
    p.description,
    p.auto_created,
    COALESCE(p.is_default, FALSE) AS is_default,
    (SELECT count(*) FROM plugin_servers ps WHERE ps.plugin_id = p.id AND ps.deleted IS FALSE) AS server_count,
    (
      SELECT count(*)
      FROM skill_distributions sd
      JOIN skills sk
        ON sk.id = sd.skill_id
        AND sk.project_id = sd.project_id
        AND sk.archived_at IS NULL
      WHERE sd.plugin_id = p.id
        AND sd.project_id = p.project_id
        AND sd.channel = 'plugin'
        AND sd.assistant_id IS NULL
        AND sd.revoked_at IS NULL
    ) AS skill_count,
    COALESCE(assignment_counts.wildcard_count, 0)::bigint AS wildcard_assignment_count,
    COALESCE(assignment_counts.role_count, 0)::bigint AS role_assignment_count,
    COALESCE(assignment_counts.user_count, 0)::bigint AS user_assignment_count,
    (gc.id IS NOT NULL)::boolean AS repository_connected,
    (COALESCE(gc.published_mcp_fingerprints ->> p.slug, '') <> '')::boolean AS published
FROM plugins p
JOIN projects
  ON projects.id = p.project_id
LEFT JOIN plugin_github_connections gc
  ON gc.project_id = p.project_id
LEFT JOIN LATERAL (
  SELECT
    count(*) FILTER (WHERE pa.principal_urn = '*') AS wildcard_count,
    count(*) FILTER (WHERE pa.principal_urn LIKE 'role:%') AS role_count,
    count(*) FILTER (WHERE pa.principal_urn LIKE 'user:%') AS user_count
  FROM plugin_assignments pa
  JOIN plugins assignment_plugin
    ON assignment_plugin.id = pa.plugin_id
    AND assignment_plugin.organization_id = pa.organization_id
    AND assignment_plugin.project_id = @project_id
    AND assignment_plugin.deleted IS FALSE
  JOIN projects assignment_project
    ON assignment_project.id = assignment_plugin.project_id
    AND assignment_project.organization_id = assignment_plugin.organization_id
    AND assignment_project.deleted IS FALSE
  WHERE pa.plugin_id = p.id
    AND pa.organization_id = @organization_id
) assignment_counts ON TRUE
WHERE p.id = @plugin_id
  AND p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND p.deleted IS FALSE;

-- name: ListPlatformMCPPluginServers :many
-- Member-facing compatibility projection. It deliberately omits membership and
-- backend IDs, which are administrative identity and must not cross this path.
SELECT
    ps.id,
    ps.display_name,
    ps.policy,
    ps.sort_order,
    (ps.toolset_id IS NOT NULL)::boolean AS toolset_backed,
    (ps.meta_mcp_server_id IS NOT NULL)::boolean AS gateway_backed,
    COALESCE(t.mcp_slug, ep.slug, gateway_ep.slug, '')::text AS mcp_slug,
    COALESCE(t.mcp_enabled, s.visibility <> 'disabled', gateway.visibility <> 'disabled', FALSE)::boolean AS enabled
FROM plugin_servers ps
JOIN plugins p
  ON p.id = ps.plugin_id
  AND p.deleted IS FALSE
LEFT JOIN toolsets t
  ON t.id = ps.toolset_id
  AND t.project_id = p.project_id
  AND t.deleted IS FALSE
LEFT JOIN mcp_servers s
  ON s.id = ps.mcp_server_id
  AND s.project_id = p.project_id
  AND s.deleted IS FALSE
LEFT JOIN meta_mcp_servers gateway
  ON gateway.id = ps.meta_mcp_server_id
  AND gateway.project_id = p.project_id
  AND gateway.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT e.slug
  FROM mcp_endpoints e
  WHERE e.mcp_server_id = s.id
    AND e.project_id = p.project_id
    AND e.deleted IS FALSE
  ORDER BY e.created_at ASC
  LIMIT 1
) ep ON TRUE
LEFT JOIN LATERAL (
  SELECT e.slug FROM mcp_endpoints e
  WHERE e.meta_mcp_server_id = gateway.id AND e.project_id = p.project_id AND e.deleted IS FALSE
  ORDER BY e.created_at, e.id LIMIT 1
) gateway_ep ON TRUE
WHERE ps.plugin_id = @plugin_id
  AND p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND ps.deleted IS FALSE
ORDER BY ps.sort_order ASC, ps.display_name ASC
LIMIT @result_limit;

-- name: GetPlatformMCPPluginMembershipVersion :one
SELECT md5(COALESCE(jsonb_agg(
  jsonb_build_array(ps.id, ps.sort_order, ps.display_name, ps.policy, ps.toolset_id, ps.mcp_server_id, ps.meta_mcp_server_id)
  ORDER BY ps.sort_order, ps.display_name, ps.id
)::text, '[]'))::text AS membership_version
FROM plugin_servers ps
JOIN plugins p ON p.id = ps.plugin_id
WHERE ps.plugin_id = @plugin_id
  AND p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND ps.deleted IS FALSE;

-- name: ListPlatformMCPPluginMemberships :many
-- Administrative membership read. The opaque version is computed over the
-- complete live membership set, while the page itself uses a total keyset order.
-- Target IDs are typed; target_resolved guards against dangling or foreign
-- backend references before exposing them as actionable targets.
WITH membership_version AS (
  SELECT md5(COALESCE(jsonb_agg(
    jsonb_build_array(ps.id, ps.sort_order, ps.display_name, ps.policy, ps.toolset_id, ps.mcp_server_id, ps.meta_mcp_server_id)
    ORDER BY ps.sort_order, ps.display_name, ps.id
  )::text, '[]'))::text AS value
  FROM plugin_servers ps
  WHERE ps.plugin_id = @plugin_id
    AND ps.deleted IS FALSE
)
SELECT
    ps.id AS membership_id,
    ps.display_name,
    ps.policy,
    ps.sort_order,
    CASE
      WHEN ps.toolset_id IS NOT NULL THEN 'toolset'
      WHEN ps.mcp_server_id IS NOT NULL THEN 'mcp_server'
      WHEN ps.meta_mcp_server_id IS NOT NULL THEN 'gateway'
    END::text AS target_kind,
    COALESCE(ps.toolset_id, ps.mcp_server_id, ps.meta_mcp_server_id) AS target_id,
    (COALESCE(t.id, s.id, gateway.id) IS NOT NULL)::boolean AS target_resolved,
    COALESCE(t.mcp_slug, ep.slug, gateway_ep.slug, '')::text AS mcp_slug,
    COALESCE(t.mcp_enabled, s.visibility <> 'disabled', gateway.visibility <> 'disabled', FALSE)::boolean AS enabled,
    membership_version.value AS membership_version
FROM plugin_servers ps
CROSS JOIN membership_version
JOIN plugins p
  ON p.id = ps.plugin_id
  AND p.deleted IS FALSE
LEFT JOIN toolsets t
  ON t.id = ps.toolset_id
  AND t.project_id = p.project_id
  AND t.deleted IS FALSE
LEFT JOIN mcp_servers s
  ON s.id = ps.mcp_server_id
  AND s.project_id = p.project_id
  AND s.deleted IS FALSE
LEFT JOIN meta_mcp_servers gateway
  ON gateway.id = ps.meta_mcp_server_id
  AND gateway.project_id = p.project_id
  AND gateway.deleted IS FALSE
LEFT JOIN LATERAL (
  SELECT e.slug
  FROM mcp_endpoints e
  WHERE e.mcp_server_id = s.id
    AND e.project_id = p.project_id
    AND e.deleted IS FALSE
  ORDER BY e.created_at, e.id
  LIMIT 1
) ep ON TRUE
LEFT JOIN LATERAL (
  SELECT e.slug
  FROM mcp_endpoints e
  WHERE e.meta_mcp_server_id = gateway.id
    AND e.project_id = p.project_id
    AND e.deleted IS FALSE
  ORDER BY e.created_at, e.id
  LIMIT 1
) gateway_ep ON TRUE
WHERE ps.plugin_id = @plugin_id
  AND p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND ps.deleted IS FALSE
  AND (NOT @use_after::boolean OR (ps.sort_order, ps.display_name, ps.id) > (@after_sort_order::integer, @after_display_name::text, @after_id::uuid))
ORDER BY ps.sort_order, ps.display_name, ps.id
LIMIT @result_limit;

-- name: ListPlatformMCPPluginSkills :many
-- One plugin's skill membership. pinned_version_id is null when the
-- distribution follows the skill's latest valid version, which is the
-- difference between a plugin that moves with authoring and one that does not.
SELECT
    sk.id AS skill_id,
    sk.name AS skill_name,
    sd.pinned_version_id
FROM skill_distributions sd
JOIN plugins p
  ON p.id = sd.plugin_id
  AND p.deleted IS FALSE
JOIN skills sk
  ON sk.id = sd.skill_id
  AND sk.project_id = sd.project_id
  AND sk.archived_at IS NULL
WHERE sd.plugin_id = @plugin_id
  AND sd.project_id = @project_id
  AND p.organization_id = @organization_id
  AND sd.channel = 'plugin'
  AND sd.assistant_id IS NULL
  AND sd.revoked_at IS NULL
ORDER BY sk.name ASC
LIMIT @result_limit;

-- name: ListPlatformMCPPluginAssignments :many
-- Reads the exact plugin's complete assignment set only after proving the
-- plugin belongs to the caller's organization and project. The complete set is
-- required for the optimistic-concurrency version; no principal leaves this
-- internal query boundary.
SELECT pa.principal_urn
FROM plugin_assignments pa
JOIN plugins p
  ON p.id = pa.plugin_id
  AND p.organization_id = pa.organization_id
  AND p.deleted IS FALSE
JOIN projects
  ON projects.id = p.project_id
  AND projects.organization_id = p.organization_id
  AND projects.deleted IS FALSE
WHERE pa.plugin_id = @plugin_id
  AND pa.organization_id = @organization_id
  AND p.project_id = @project_id
ORDER BY pa.principal_urn;

-- name: ListPlatformMCPPluginAssignmentsWithModes :many
-- The same exact-plugin assignment set as ListPlatformMCPPluginAssignments,
-- with each assignment's install mode, which the optimistic-concurrency version
-- also covers.
SELECT pa.principal_urn, pa.install_mode
FROM plugin_assignments pa
JOIN plugins p
  ON p.id = pa.plugin_id
  AND p.organization_id = pa.organization_id
  AND p.deleted IS FALSE
JOIN projects
  ON projects.id = p.project_id
  AND projects.organization_id = p.organization_id
  AND projects.deleted IS FALSE
WHERE pa.plugin_id = @plugin_id
  AND pa.organization_id = @organization_id
  AND p.project_id = @project_id
ORDER BY pa.principal_urn;

-- name: ListPlatformMCPPluginAssignmentOptions :many
-- Resolves only the bounded assignment options this Platform response can use.
-- selected_principal_urns is empty for the organization-wide chooser and set to
-- one plugin's current assignments for the detail view.
WITH active_roles AS (
  SELECT id, workos_slug, workos_name, 'global'::text AS role_kind
  FROM global_roles
  WHERE deleted IS FALSE
    AND workos_deleted IS FALSE
  UNION ALL
  SELECT id, workos_slug, workos_name, 'organization'::text AS role_kind
  FROM organization_roles
  WHERE organization_roles.organization_id = @organization_id
    AND organization_roles.deleted IS FALSE
    AND organization_roles.workos_deleted IS FALSE
), assignment_options AS (
  SELECT
    0::int AS kind_order,
    ''::text AS sort_key,
    'everyone'::text AS kind,
    'Everyone'::text AS display_name,
    NULL::bigint AS member_count,
    '*'::text AS principal_urn
  WHERE COALESCE(cardinality(@selected_principal_urns::text[]), 0) = 0
     OR '*' = ANY(@selected_principal_urns::text[])
  UNION ALL
  SELECT
    1::int,
    active_roles.workos_slug,
    'role'::text,
    active_roles.workos_name,
    COUNT(DISTINCT ora.user_id)::bigint,
    ('role:' || active_roles.role_kind || ':' || active_roles.id::text)::text
  FROM active_roles
  LEFT JOIN organization_role_assignments ora
    ON ora.organization_id = @organization_id
    AND ora.role_urn = 'role:' || active_roles.role_kind || ':' || active_roles.id::text
    AND ora.user_id IS NOT NULL
    AND ora.deleted_at IS NULL
  WHERE COALESCE(cardinality(@selected_principal_urns::text[]), 0) = 0
     OR ('role:' || active_roles.role_kind || ':' || active_roles.id::text) = ANY(@selected_principal_urns::text[])
  GROUP BY active_roles.id, active_roles.role_kind, active_roles.workos_slug, active_roles.workos_name
  UNION ALL
  SELECT
    2::int,
    dg.name,
    'directory_group'::text,
    dg.name,
    COUNT(DISTINCT NULLIF(LOWER(TRIM(du.email)), ''))::bigint,
    ('directory_group:' || dg.id::text)::text
  FROM directory_groups dg
  LEFT JOIN directory_user_group_memberships m
    ON m.directory_group_id = dg.id
    AND m.deleted IS FALSE
  LEFT JOIN directory_users du
    ON du.id = m.directory_user_id
    AND du.organization_id = dg.organization_id
    AND du.deleted IS FALSE
    AND du.workos_deleted IS FALSE
  WHERE dg.organization_id = @organization_id
    AND dg.deleted IS FALSE
    AND dg.workos_deleted IS FALSE
    AND (
      COALESCE(cardinality(@selected_principal_urns::text[]), 0) = 0
      OR ('directory_group:' || dg.id::text) = ANY(@selected_principal_urns::text[])
    )
  GROUP BY dg.id, dg.name
  UNION ALL
  SELECT
    3::int,
    attribute.key || ':' || attribute.value,
    'directory_attribute'::text,
    attribute.key || ': ' || attribute.value,
    COUNT(DISTINCT NULLIF(LOWER(TRIM(du.email)), ''))::bigint,
    ('directory_attribute:' ||
      translate(rtrim(replace(encode(convert_to(attribute.key, 'UTF8'), 'base64'), E'\n', ''), '='), '+/', '-_') || ':' ||
      translate(rtrim(replace(encode(convert_to(attribute.value, 'UTF8'), 'base64'), E'\n', ''), '='), '+/', '-_'))::text
  FROM directory_users du
  CROSS JOIN LATERAL jsonb_each_text(
    CASE jsonb_typeof(du.attributes)
      WHEN 'object' THEN du.attributes
      ELSE '{}'::jsonb
    END
  ) attribute(key, value)
  WHERE du.organization_id = @organization_id
    AND du.deleted IS FALSE
    AND du.workos_deleted IS FALSE
    AND attribute.key IN ('cost_center_name', 'department_name', 'division_name', 'employee_type', 'job_title')
    AND attribute.value IS NOT NULL
    AND (
      COALESCE(cardinality(@selected_principal_urns::text[]), 0) = 0
      OR ('directory_attribute:' ||
        translate(rtrim(replace(encode(convert_to(attribute.key, 'UTF8'), 'base64'), E'\n', ''), '='), '+/', '-_') || ':' ||
        translate(rtrim(replace(encode(convert_to(attribute.value, 'UTF8'), 'base64'), E'\n', ''), '='), '+/', '-_')) = ANY(@selected_principal_urns::text[])
    )
  GROUP BY attribute.key, attribute.value
)
SELECT kind, display_name, member_count, principal_urn
FROM assignment_options
WHERE COALESCE(cardinality(@selected_principal_urns::text[]), 0) = 0
   OR principal_urn = ANY(@selected_principal_urns::text[])
ORDER BY kind_order, sort_key, principal_urn
LIMIT @result_limit;

-- name: ResolvePlatformMCPPluginTarget :many
-- Matches one plugin by id, slug, or whole name over the project's entire
-- plugin set. Matching in SQL rather than over a bounded page is what keeps a
-- plugin that exists from being refused as not_found, and an ambiguous name
-- from resolving to whichever match a page happened to include. Two rows are
-- enough to know a name is ambiguous.
SELECT
    p.id,
    p.name,
    p.slug,
    COALESCE(p.is_default, FALSE) AS is_default
FROM plugins p
JOIN projects
  ON projects.id = p.project_id
WHERE p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND p.deleted IS FALSE
  AND (
    p.id::text = @target::text
    OR lower(p.slug) = lower(@target::text)
    OR lower(p.name) = lower(@target::text)
  )
ORDER BY p.id ASC
LIMIT 2;

-- name: SearchPlatformMCPAccessMembers :many
-- Count distinct members and return only a bounded page from the same snapshot.
-- Active local role assignments follow the access roster's WorkOS identity join.
WITH member_roles AS (
  SELECT ora.workos_user_id,
    array_agg(DISTINCT COALESCE(r.id::text, g.id::text)) AS role_ids,
    array_agg(DISTINCT COALESCE(r.workos_name, g.workos_name)) AS role_names
  FROM organization_role_assignments ora
  LEFT JOIN organization_roles r ON ora.role_urn = 'role:organization:' || r.id::text
    AND r.organization_id = @organization_id AND r.deleted IS FALSE AND r.workos_deleted IS FALSE
  LEFT JOIN global_roles g ON ora.role_urn = 'role:global:' || g.id::text
    AND g.deleted IS FALSE AND g.workos_deleted IS FALSE
  WHERE ora.organization_id = @organization_id AND ora.deleted_at IS NULL
    AND COALESCE(r.id, g.id) IS NOT NULL
  GROUP BY ora.workos_user_id
), matching AS (
  SELECT DISTINCT u.id, u.display_name, u.email, COALESCE(m.role_ids, '{}'::text[])::text[] AS role_ids
  FROM organization_user_relationships rel
  JOIN users u ON u.id = rel.user_id AND u.deleted_at IS NULL
  LEFT JOIN member_roles m ON m.workos_user_id = u.workos_id
  WHERE rel.organization_id = @organization_id AND rel.deleted IS FALSE
    AND (@role_id::text = '' OR @role_id::text = ANY(m.role_ids))
    AND (@query::text = ''
      OR strpos(lower(trim(regexp_replace(u.display_name, '[[:space:]]+', ' ', 'g'))), @query::text) > 0
      OR strpos(lower(trim(regexp_replace(u.email, '[[:space:]]+', ' ', 'g'))), @query::text) > 0
      OR EXISTS (SELECT 1 FROM unnest(m.role_names) AS role_name
        WHERE strpos(lower(trim(regexp_replace(role_name, '[[:space:]]+', ' ', 'g'))), @query::text) > 0))
)
SELECT id, display_name, email, role_ids, count(*) OVER ()::bigint AS total_matches
FROM matching
ORDER BY email, id
LIMIT @result_limit;

-- name: GetPlatformMCPPluginForUpdate :one
-- Serializes an MCP distribution write against concurrent deletion of the
-- exact plugin the caller named. A deleted plugin deliberately returns no row,
-- which the caller reports as not_found rather than retargeting the default.
SELECT p.id, p.name, p.slug
FROM plugins p
WHERE p.id = @plugin_id
  AND p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND p.deleted IS FALSE
FOR UPDATE;

-- name: ListPlatformMCPProjectTools :many
-- The tools a project's latest completed deployment generated, with the source
-- that produced each one. This is the catalogue an agent picks from when it is
-- asked to expose a freshly pushed tool; it reports generated tool definitions
-- only and never reads an environment, a secret, or a runtime credential.
--
-- The deployment and all_deployment_ids CTEs below are duplicated verbatim in
-- ListPlatformMCPProjectToolURNs, because sqlc cannot share a CTE between
-- queries. The two MUST stay identical: this one decides what a caller is
-- offered and that one decides what the mutation accepts, so any divergence
-- shows a tool here that the change then refuses by name. Edit both together.
WITH deployment AS (
    SELECT d.id
    FROM deployments d
    JOIN deployment_statuses ds ON d.id = ds.deployment_id
    JOIN projects p ON p.id = d.project_id
    WHERE d.project_id = @project_id
      AND p.organization_id = @organization_id
      AND p.deleted IS FALSE
      AND ds.status = 'completed'
    ORDER BY d.seq DESC
    LIMIT 1
),
all_deployment_ids AS (
    SELECT id FROM deployment
    UNION
    SELECT DISTINCT pv.deployment_id
    FROM deployment d
    JOIN deployments_packages dp ON dp.deployment_id = d.id
    JOIN package_versions pv ON dp.version_id = pv.id
),
project_tools AS (
    SELECT
        ftd.tool_urn::TEXT AS tool_urn,
        ftd.name::TEXT AS tool_name,
        COALESCE(ftd.description, '')::TEXT AS summary,
        'function'::TEXT AS source_kind,
        COALESCE(df.slug, '')::TEXT AS source_slug,
        COALESCE(df.name, '')::TEXT AS source_name
    FROM function_tool_definitions ftd
    LEFT JOIN deployments_functions df ON ftd.function_id = df.id
    WHERE ftd.deployment_id = (SELECT id FROM deployment)
      AND ftd.deleted IS FALSE
    UNION ALL
    SELECT
        htd.tool_urn::TEXT AS tool_urn,
        htd.name::TEXT AS tool_name,
        COALESCE(NULLIF(htd.summary, ''), htd.description, '')::TEXT AS summary,
        'openapi'::TEXT AS source_kind,
        COALESCE(doa.slug, '')::TEXT AS source_slug,
        COALESCE(doa.name, '')::TEXT AS source_name
    FROM http_tool_definitions htd
    LEFT JOIN deployments_openapiv3_assets doa ON htd.openapiv3_document_id = doa.id
    WHERE htd.deployment_id IN (SELECT id FROM all_deployment_ids)
      AND htd.deleted IS FALSE
)
SELECT
    (SELECT id FROM deployment)::uuid AS deployment_id,
    project_tools.tool_urn,
    project_tools.tool_name,
    project_tools.summary,
    project_tools.source_kind,
    project_tools.source_slug,
    project_tools.source_name
FROM project_tools
WHERE (sqlc.narg(after_tool_urn)::text IS NULL OR project_tools.tool_urn > sqlc.narg(after_tool_urn)::text)
  AND (sqlc.narg(source_kind)::text IS NULL OR project_tools.source_kind = sqlc.narg(source_kind)::text)
  AND (
      @query_text::text = ''
      OR project_tools.tool_urn ILIKE '%' || @query_text::text || '%'
      OR project_tools.tool_name ILIKE '%' || @query_text::text || '%'
      OR project_tools.source_slug ILIKE '%' || @query_text::text || '%'
  )
ORDER BY project_tools.tool_urn ASC
LIMIT @limit_value;

-- name: ListPlatformMCPProjectToolURNs :many
-- Confirms that exactly the named tool URNs are generated by the project's
-- latest completed deployment. A URN missing from the result is named back to
-- the caller rather than silently skipped.
--
-- The deployment and all_deployment_ids CTEs below are duplicated verbatim from
-- ListPlatformMCPProjectTools, because sqlc cannot share a CTE between
-- queries. The two MUST stay identical: that one decides what a caller is
-- offered and this one decides what the mutation accepts, so any divergence
-- refuses a tool the listing just advertised. Edit both together.
WITH deployment AS (
    SELECT d.id
    FROM deployments d
    JOIN deployment_statuses ds ON d.id = ds.deployment_id
    JOIN projects p ON p.id = d.project_id
    WHERE d.project_id = @project_id
      AND p.organization_id = @organization_id
      AND p.deleted IS FALSE
      AND ds.status = 'completed'
    ORDER BY d.seq DESC
    LIMIT 1
),
all_deployment_ids AS (
    SELECT id FROM deployment
    UNION
    SELECT DISTINCT pv.deployment_id
    FROM deployment d
    JOIN deployments_packages dp ON dp.deployment_id = d.id
    JOIN package_versions pv ON dp.version_id = pv.id
)
SELECT ftd.tool_urn::TEXT AS tool_urn
FROM function_tool_definitions ftd
WHERE ftd.deployment_id = (SELECT id FROM deployment)
  AND ftd.deleted IS FALSE
  AND ftd.tool_urn = ANY(@tool_urns::text[])
UNION
SELECT htd.tool_urn::TEXT AS tool_urn
FROM http_tool_definitions htd
WHERE htd.deployment_id IN (SELECT id FROM all_deployment_ids)
  AND htd.deleted IS FALSE
  AND htd.tool_urn = ANY(@tool_urns::text[]);

-- name: LockPlatformMCPToolsetForToolExposure :one
-- Takes the toolset row lock, and must run BEFORE the server row is locked.
--
-- The order is the constraint, not the lock. toolsets.UpdateToolset holds this
-- same row (via GetToolsetForUpdate) and then, inside reconcileHostedNetworkAccess,
-- updates the hosted mcp_servers row — an exclusive row lock taken by a plain
-- UPDATE rather than an explicit FOR UPDATE. For a hosted server both ids are
-- the toolset id, so it is the same pair of rows this path touches. Locking the
-- server first here and the toolset first there is an ABBA cycle that
-- PostgreSQL resolves by aborting one side with deadlock_detected, so both
-- paths take toolsets before mcp_servers.
SELECT t.id
FROM toolsets AS t
JOIN projects AS p
  ON p.id = t.project_id
 AND p.organization_id = @organization_id
 AND p.deleted IS FALSE
WHERE t.id = @toolset_id
  AND t.project_id = @project_id
  AND t.deleted IS FALSE
FOR UPDATE OF t;

-- name: LockPlatformMCPServerToolsetBinding :one
-- Pins the named server's backing-toolset binding for the rest of the caller's
-- transaction, and must run before anything reads the exposure.
--
-- GetPlatformMCPServerToolExposure takes no lock, so without this the whole
-- decision — which toolset to write, which servers that write moves, whether
-- any of them sit outside the project — is made against an unpinned snapshot
-- of mcp_servers. UpdateMCPServer assigns toolset_id, so a concurrent
-- dashboard edit can repoint this server between the read and the write; the
-- exposure version token covers toolset_versions only and would not notice.
-- The change would then land on a toolset the named server no longer fronts.
--
-- FOR UPDATE OF m locks only the server row, which is what UpdateMCPServer and
-- DeleteMCPServer update by id, so both block until this transaction ends.
--
-- This runs AFTER LockPlatformMCPToolsetForToolExposure, never before: see
-- that query for the lock-order cycle it would otherwise form with
-- UpdateToolset. Because the toolset id can only be learned by reading this
-- binding first, the caller peeks at it unlocked, locks the toolset, locks
-- this row, and then re-reads — so a repoint in between is detected rather
-- than acted on.
SELECT m.id
FROM mcp_servers AS m
JOIN projects AS p
  ON p.id = m.project_id
 AND p.organization_id = @organization_id
 AND p.deleted IS FALSE
WHERE m.id = @mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE
  AND m.toolset_id IS NOT NULL
FOR UPDATE OF m;

-- name: GetPlatformMCPServerToolExposure :one
-- The tool list one hosted MCP server exposes, read through its modern server
-- record. A server whose backend is not a Speakeasy toolset, or a bare toolset with
-- no server record, deliberately returns no row: its tool list is not Speakeasy's
-- to change from here.
-- The columns this returns are only the ones the caller cannot already supply:
-- the organization, project and MCP server ids are query inputs, so echoing
-- them back would just be a second source of truth for the same values.
SELECT
    t.id AS toolset_id,
    t.slug AS toolset_slug,
    COALESCE(latest.version, 0)::bigint AS toolset_version,
    COALESCE(latest.tool_urns, ARRAY[]::TEXT[])::TEXT[] AS tool_urns,
    -- Every live server in THIS project fronting this same toolset. The tool
    -- list lives on the toolset, not on the server record, so these servers
    -- are aliases for one list: a write authorized against only the named
    -- server would move all of them. Nothing in the schema forbids the
    -- sharing, so the caller authorizes each of these before applying the
    -- change.
    --
    -- The project predicate is load-bearing, not tidiness. mcp_servers.
    -- toolset_id has no composite constraint pairing it with project_id, so a
    -- server in another project can front this toolset. Each id here is
    -- authorized with authz.MCPCheck(ScopeMCPWrite, id, <this project>), and
    -- that check injects the project as a selector dimension so a
    -- project-scoped grant matches — which means a project-wide mcp:write in
    -- THIS project would wave through a server id belonging to another one.
    (
        SELECT COALESCE(array_agg(fronting.id ORDER BY fronting.id), ARRAY[]::uuid[])
        FROM mcp_servers AS fronting
        WHERE fronting.toolset_id = t.id
          AND fronting.project_id = m.project_id
          AND fronting.deleted IS FALSE
    )::uuid[] AS fronting_server_ids,
    -- Scoping the list above makes the authorization sound but makes the
    -- out-of-project alias invisible, and the write would still move it. This
    -- counts them so the mutation can refuse instead: silently changing
    -- another project's server is worse than refusing and naming the
    -- dashboard, which can show the shared set and every server using it.
    (
        SELECT count(*)
        FROM mcp_servers AS outside
        WHERE outside.toolset_id = t.id
          AND outside.project_id <> m.project_id
          AND outside.deleted IS FALSE
    )::bigint AS foreign_fronting_server_count
FROM mcp_servers AS m
JOIN projects AS p
  ON p.id = m.project_id
 AND p.organization_id = @organization_id
 AND p.deleted IS FALSE
JOIN toolsets AS t
  ON t.id = m.toolset_id
 AND t.project_id = m.project_id
 AND t.deleted IS FALSE
LEFT JOIN LATERAL (
    SELECT tv.version, tv.tool_urns
    FROM toolset_versions AS tv
    WHERE tv.toolset_id = t.id
      AND tv.deleted IS FALSE
    ORDER BY tv.version DESC
    LIMIT 1
) AS latest ON TRUE
WHERE m.id = @mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE
  AND m.toolset_id IS NOT NULL;
