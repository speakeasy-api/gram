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
     AND issuer.project_id = project.id
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
-- Registration is safe for a new organization: the selected project may be
-- empty. It remains unavailable for a project that already owns an active
-- toolset-backed MCP, because that legacy model must not be mixed with the
-- Platform registration lifecycle. Package admission retains its independent
-- organization-level cohort check.
SELECT EXISTS (
    SELECT 1
    FROM projects AS target
    WHERE target.id = @project_id
      AND target.organization_id = @organization_id
      AND target.deleted IS FALSE
      AND NOT EXISTS (
          SELECT 1
          FROM mcp_servers AS legacy_server
          WHERE legacy_server.project_id = target.id
            AND legacy_server.deleted IS FALSE
            AND legacy_server.visibility <> 'disabled'
            AND legacy_server.toolset_id IS NOT NULL
      )
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

-- name: LockPlatformMCPProjectRegistrationQuota :exec
-- Serialize active-registration counting and desired-state creation for one
-- project. Callers acquire the receipt lock first, then this quota lock, then
-- the candidate-specific desired-state lock.
SELECT pg_advisory_xact_lock(
    hashtextextended(
        jsonb_build_array('platform-mcp-registration-quota', @organization_id::text, @project_id::text)::text,
        0
    )
);

-- name: CountActiveRegisteredPlatformMCPCatalogRegistrations :one
SELECT COUNT(*)
FROM platform_mcp_catalog_registrations
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND status = 'registered'
  AND deleted IS FALSE;

-- name: SoftDeletePendingPlatformMCPCatalogRegistration :exec
UPDATE platform_mcp_catalog_registrations
SET deleted_at = clock_timestamp()
WHERE id = @registration_id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND status = 'pending'
  AND remote_mcp_server_id IS NULL
  AND user_session_issuer_id IS NULL
  AND mcp_server_id IS NULL
  AND mcp_endpoint_id IS NULL;

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

-- name: ListPlatformMCPInventory :many
-- One bounded, tenant-qualified inventory projection for every Platform MCP
-- read surface. It reads persisted readiness/distribution state only; it never
-- contacts a remote MCP or provider.
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

-- name: ListPlatformMCPInventoryDistributions :many
SELECT
    distribution.registration_id,
    COALESCE(distribution.plugin_id, distribution.default_plugin_id) AS plugin_id,
    distribution.state,
    distribution.publication_state
FROM platform_mcp_distributions AS distribution
JOIN projects AS project
  ON project.id = distribution.project_id
 AND project.organization_id = distribution.organization_id
 AND project.deleted IS FALSE
JOIN platform_mcp_catalog_registrations AS registration
  ON registration.id = distribution.registration_id
 AND registration.organization_id = distribution.organization_id
 AND registration.project_id = distribution.project_id
 AND registration.deleted IS FALSE
WHERE distribution.organization_id = @organization_id
  AND (sqlc.narg(project_id)::uuid IS NULL OR distribution.project_id = sqlc.narg(project_id)::uuid)
  AND distribution.registration_id = ANY(@registration_ids::uuid[])
ORDER BY distribution.registration_id, distribution.id ASC;

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

-- name: GetPlatformMCPDiagnosticsTarget :one
-- Resolves one configured MCP to the identities its telemetry is recorded
-- under: the toolset slug that calls arriving directly at Gram carry, and the
-- MCP slug that appears in the URL an agent-hook-observed client called.
-- Scoped to the organization's own project, so a caller cannot diagnose an MCP
-- it cannot already see through the inventory.
SELECT
    m.id AS mcp_server_id,
    m.project_id,
    COALESCE(m.slug, '') AS mcp_slug,
    COALESCE(toolset.slug, '') AS toolset_slug
FROM mcp_servers AS m
JOIN projects AS project
  ON project.id = m.project_id
 AND project.organization_id = @organization_id
 AND project.deleted IS FALSE
LEFT JOIN toolsets AS toolset
  ON toolset.id = m.toolset_id
 AND toolset.project_id = m.project_id
 AND toolset.organization_id = @organization_id
 AND toolset.deleted IS FALSE
WHERE m.id = @mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE;

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
-- rows only (the digest does not render content parts), with the canonical
-- suppression filters from risk's ListRiskResultsByChatFound: found, not
-- excluded, not swept as false positive, policy still enabled and not deleted.
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
    (SELECT count(*) FROM plugin_assignments pa WHERE pa.plugin_id = p.id AND pa.principal_urn = '*') AS wildcard_assignment_count,
    (SELECT count(*) FROM plugin_assignments pa WHERE pa.plugin_id = p.id AND pa.principal_urn LIKE 'role:%') AS role_assignment_count,
    (SELECT count(*) FROM plugin_assignments pa WHERE pa.plugin_id = p.id AND pa.principal_urn LIKE 'user:%') AS user_assignment_count,
    (gc.id IS NOT NULL)::boolean AS repository_connected,
    (COALESCE(gc.published_mcp_fingerprints ->> p.slug, '') <> '')::boolean AS published
FROM plugins p
JOIN projects
  ON projects.id = p.project_id
LEFT JOIN plugin_github_connections gc
  ON gc.project_id = p.project_id
WHERE p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND p.deleted IS FALSE
  AND (NOT @use_after::boolean OR p.id > @after_id)
ORDER BY p.id ASC
LIMIT @result_limit;

-- name: GetPlatformMCPPluginInventoryItem :one
SELECT
    p.id,
    p.name,
    p.slug,
    p.description,
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
    (SELECT count(*) FROM plugin_assignments pa WHERE pa.plugin_id = p.id AND pa.principal_urn = '*') AS wildcard_assignment_count,
    (SELECT count(*) FROM plugin_assignments pa WHERE pa.plugin_id = p.id AND pa.principal_urn LIKE 'role:%') AS role_assignment_count,
    (SELECT count(*) FROM plugin_assignments pa WHERE pa.plugin_id = p.id AND pa.principal_urn LIKE 'user:%') AS user_assignment_count,
    (gc.id IS NOT NULL)::boolean AS repository_connected,
    (COALESCE(gc.published_mcp_fingerprints ->> p.slug, '') <> '')::boolean AS published
FROM plugins p
JOIN projects
  ON projects.id = p.project_id
LEFT JOIN plugin_github_connections gc
  ON gc.project_id = p.project_id
WHERE p.id = @plugin_id
  AND p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND p.deleted IS FALSE;

-- name: ListPlatformMCPPluginServers :many
-- One plugin's MCP server membership. A plugin server is backed by exactly one
-- of a toolset or an mcp_server (plugin_servers_backend_exclusivity_check), so
-- the slug and enabled state are resolved from whichever backend is set. No URL
-- is constructed here: this surface names servers, it does not hand out
-- endpoints.
SELECT
    ps.id,
    ps.display_name,
    ps.policy,
    ps.sort_order,
    (ps.toolset_id IS NOT NULL)::boolean AS toolset_backed,
    COALESCE(t.mcp_slug, ep.slug, '')::text AS mcp_slug,
    COALESCE(t.mcp_enabled, s.visibility <> 'disabled', FALSE)::boolean AS enabled
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
LEFT JOIN LATERAL (
  SELECT e.slug
  FROM mcp_endpoints e
  WHERE e.mcp_server_id = s.id
    AND e.project_id = p.project_id
    AND e.deleted IS FALSE
  ORDER BY e.created_at ASC
  LIMIT 1
) ep ON TRUE
WHERE ps.plugin_id = @plugin_id
  AND p.project_id = @project_id
  AND p.organization_id = @organization_id
  AND ps.deleted IS FALSE
ORDER BY ps.sort_order ASC, ps.display_name ASC
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
