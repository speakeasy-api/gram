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
    active_generation
) VALUES (
    @id,
    @organization_id,
    @subject_urn,
    @oauth_client_id,
    @active_generation
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
    updated_at = @revoked_at
WHERE id = @id
  AND organization_id = @organization_id
  AND revoked_at IS NULL
RETURNING *;

-- name: RotatePlatformMCPConnectionGeneration :one
UPDATE platform_mcp_connections
SET active_generation = @active_generation,
    reauthorized_at = @reauthorized_at,
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

-- name: GetPlatformMCPAuthorizationGrantForConsume :one
SELECT auth_grant.*, connection.subject_urn, connection.active_generation, client.client_id
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
  AND client.revoked_at IS NULL
FOR UPDATE OF auth_grant;

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
  AND client.revoked_at IS NULL
FOR UPDATE OF session;

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
SELECT receipt.*
FROM platform_mcp_operation_receipts AS receipt
JOIN platform_mcp_connections AS connection
  ON connection.id = receipt.connection_id
 AND connection.organization_id = receipt.organization_id
WHERE receipt.organization_id = @organization_id
  AND connection.subject_urn = @subject_urn
  AND receipt.project_id = @project_id
  AND receipt.operation = @operation
  AND receipt.idempotency_key = @idempotency_key
ORDER BY receipt.created_at DESC, receipt.id DESC
LIMIT 1;

-- name: DeleteExpiredPlatformMCPOperationReceipt :execrows
DELETE FROM platform_mcp_operation_receipts AS receipt
USING platform_mcp_connections AS connection
WHERE receipt.connection_id = connection.id
  AND receipt.organization_id = connection.organization_id
  AND receipt.organization_id = @organization_id
  AND connection.subject_urn = @subject_urn
  AND receipt.project_id = @project_id
  AND receipt.operation = @operation
  AND receipt.idempotency_key = @idempotency_key
  AND receipt.expires_at <= clock_timestamp();

-- name: CreatePlatformMCPOperationReceipt :one
INSERT INTO platform_mcp_operation_receipts (
    organization_id,
    project_id,
    registration_id,
    connection_id,
    connection_generation,
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

-- name: GetPlatformMCPCatalogRegistrationForLifecycle :one
-- Registrations are project desired state, not permanently owned by the OAuth
-- client that originally created them. Lifecycle actions require the current
-- active Platform connection to belong to that same user subject.
SELECT registration.*
FROM platform_mcp_catalog_registrations AS registration
JOIN platform_mcp_connections AS created_connection
  ON created_connection.id = registration.connection_id
 AND created_connection.organization_id = registration.organization_id
JOIN platform_mcp_connections AS current_connection
  ON current_connection.id = @connection_id
 AND current_connection.organization_id = registration.organization_id
JOIN projects AS project
  ON project.id = registration.project_id
 AND project.organization_id = registration.organization_id
 AND project.deleted IS FALSE
WHERE registration.id = @registration_id
  AND registration.organization_id = @organization_id
  AND registration.project_id = @project_id
  AND registration.deleted IS FALSE
  AND created_connection.subject_urn = @subject_urn
  AND current_connection.subject_urn = @subject_urn
  AND current_connection.active_generation = @connection_generation
  AND current_connection.revoked_at IS NULL;

-- name: CreatePlatformMCPCatalogRegistration :one
INSERT INTO platform_mcp_catalog_registrations (
    organization_id,
    project_id,
    source_kind,
    catalog_provider,
    catalog_reference,
    status,
    connection_id,
    connection_generation
) VALUES (
    @organization_id,
    @project_id,
    @source_kind,
    @catalog_provider,
    @catalog_reference,
    @status,
    @connection_id,
    @connection_generation
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
    provider_key,
    intent,
    handoff_hash,
    expires_at
)
SELECT
    @organization_id,
    @project_id,
    @registration_id,
    @connection_id,
    @connection_generation,
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
  AND connection_id = @connection_id
  AND connection_generation = @connection_generation
  AND intent = @intent
  AND redeemed_at IS NULL
  AND invalidated_at IS NULL;

-- name: GetPlatformMCPSetupHandoffForDashboardStart :one
SELECT
    handoff.id,
    handoff.project_id,
    handoff.registration_id,
    handoff.provider_key,
    handoff.intent,
    handoff.connection_id,
    handoff.connection_generation,
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
JOIN platform_mcp_connections AS connection
  ON connection.id = handoff.connection_id
 AND connection.organization_id = handoff.organization_id
WHERE handoff.handoff_hash = @handoff_hash
  AND handoff.organization_id = @organization_id
  AND connection.subject_urn = @subject_urn
  AND connection.active_generation = handoff.connection_generation
  AND connection.revoked_at IS NULL
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
  AND handoff.connection_id = @connection_id
  AND handoff.connection_generation = @connection_generation
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
      JOIN platform_mcp_connections AS connection
        ON connection.id = handoff.connection_id
       AND connection.organization_id = handoff.organization_id
      WHERE registration.id = handoff.registration_id
        AND registration.organization_id = handoff.organization_id
        AND registration.project_id = handoff.project_id
        AND registration.deleted IS FALSE
        AND connection.subject_urn = @subject_urn
        AND connection.active_generation = handoff.connection_generation
        AND connection.revoked_at IS NULL
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
DELETE FROM platform_mcp_readiness AS stale
WHERE stale.organization_id = @organization_id
  AND stale.project_id = @project_id
  AND stale.registration_id = @registration_id
  AND stale.connection_id = @connection_id
  AND stale.connection_generation = @connection_generation
  AND stale.expires_at <= clock_timestamp()
  AND EXISTS (
      SELECT 1
      FROM platform_mcp_readiness AS newer
      WHERE newer.organization_id = stale.organization_id
        AND newer.project_id = stale.project_id
        AND newer.registration_id = stale.registration_id
        AND newer.connection_id = stale.connection_id
        AND newer.connection_generation = stale.connection_generation
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
  AND readiness.connection_id = @connection_id
  AND readiness.connection_generation = @connection_generation
  AND readiness.provider_authorization_fingerprint = @provider_authorization_fingerprint;

-- name: GetLatestPlatformMCPReadinessForLifecycle :one
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
 JOIN platform_mcp_connections AS connection
   ON connection.id = readiness.connection_id
 AND connection.organization_id = readiness.organization_id
WHERE readiness.organization_id = @organization_id
  AND readiness.project_id = @project_id
  AND readiness.registration_id = @registration_id
  AND readiness.connection_id = @connection_id
  AND readiness.connection_generation = @connection_generation
  AND connection.subject_urn = @subject_urn
  AND connection.active_generation = readiness.connection_generation
  AND connection.revoked_at IS NULL
ORDER BY readiness.checked_at DESC, readiness.id DESC
LIMIT 1;

-- name: UpsertPlatformMCPReadiness :one
INSERT INTO platform_mcp_readiness (
    organization_id,
    project_id,
    registration_id,
    connection_id,
    connection_generation,
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
DO UPDATE SET
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
        jsonb_build_array(@organization_id::text, @project_id::text, @registration_id::text, @default_plugin_id::text)::text,
        0
    )
);

-- name: GetPlatformMCPDistribution :one
SELECT distribution.*
FROM platform_mcp_distributions AS distribution
JOIN projects AS project
  ON project.id = distribution.project_id
 AND project.organization_id = distribution.organization_id
 AND project.deleted IS FALSE
WHERE distribution.organization_id = @organization_id
  AND distribution.project_id = @project_id
  AND distribution.registration_id = @registration_id
  AND distribution.default_plugin_id = @default_plugin_id;

-- name: CreatePlatformMCPDistribution :one
INSERT INTO platform_mcp_distributions (
    organization_id,
    project_id,
    registration_id,
    default_plugin_id,
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
    @default_plugin_id,
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
      ON plugin.id = @default_plugin_id
     AND plugin.organization_id = project.organization_id
     AND plugin.project_id = project.id
     AND plugin.is_default IS TRUE
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
  AND platform_mcp_distributions.default_plugin_id = @default_plugin_id
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
       AND plugin.is_default IS TRUE
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
  AND platform_mcp_distributions.default_plugin_id = @default_plugin_id
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
       AND plugin.is_default IS TRUE
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
SELECT
    distribution.id AS distribution_id,
    distribution.version AS distribution_version,
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
) VALUES (
    @organization_id,
    'first_value_achieved',
    @connection_id,
    @connection_generation,
    @project_id,
    @mcp_key
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

-- name: HasAttachedPlatformMCPOnboardingDistributionForProject :one
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
SET source_surface = @source_surface,
    client_family = @client_family,
    expires_at = @expires_at,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND initiating_subject_urn = @initiating_subject_urn
  AND status = 'active'
  AND expires_at > clock_timestamp()
RETURNING *;

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
WHERE connection.organization_id = @organization_id
  AND connection.subject_urn = @subject_urn
  AND connection.revoked_at IS NULL
  AND client.revoked_at IS NULL
ORDER BY COALESCE(connection.reauthorized_at, connection.authorized_at) DESC, connection.id DESC;

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
