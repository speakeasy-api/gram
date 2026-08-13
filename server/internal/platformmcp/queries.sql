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
-- Registration requires the existing organization-level new-model cohort, while
-- the selected project may be empty. An active toolset-backed MCP server marks a
-- project as legacy-bound and prevents mixing the Platform registration path.
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
      AND EXISTS (
          SELECT 1
          FROM mcp_servers AS cohort_server
          JOIN projects AS cohort_project
            ON cohort_project.id = cohort_server.project_id
           AND cohort_project.organization_id = @organization_id
           AND cohort_project.deleted IS FALSE
          JOIN user_session_issuers AS issuer
            ON issuer.id = cohort_server.user_session_issuer_id
           AND issuer.project_id = cohort_project.id
           AND issuer.deleted IS FALSE
          JOIN mcp_endpoints AS endpoint
            ON endpoint.mcp_server_id = cohort_server.id
           AND endpoint.project_id = cohort_project.id
           AND endpoint.deleted IS FALSE
          WHERE cohort_server.deleted IS FALSE
            AND cohort_server.visibility <> 'disabled'
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
