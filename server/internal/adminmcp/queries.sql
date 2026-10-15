-- name: RecordWriteEvent :exec
-- Staff-only lifecycle trail. Only bounded reason codes are stored.
INSERT INTO admin_mcp_write_events (proposal_id, subject_urn, oauth_client_id, event, reason_code)
VALUES (@proposal_id, @subject_urn, @oauth_client_id, @event, @reason_code);

-- name: LockLiveOAuthClient :one
-- Consent takes the client row before the connection row. Proposal work uses
-- the same order and never takes an exclusive lock on a client shared by other
-- staff.
SELECT id
FROM admin_mcp_oauth_clients
WHERE id = @id
  AND revoked_at IS NULL
  AND (client_secret_expires_at IS NULL OR client_secret_expires_at > clock_timestamp())
FOR SHARE;

-- name: LockWriteConnectionShared :one
-- Lets concurrent executes on one connection proceed while still blocking a
-- generation change from reconsent or revocation.
SELECT active_generation
FROM admin_mcp_connections
WHERE id = @id
  AND oauth_client_id = @oauth_client_id
  AND subject_urn = @subject_urn
  AND revoked_at IS NULL
  AND reauthorization_required_at IS NULL
  AND authorization_expires_at > clock_timestamp()
  AND scopes @> ARRAY['admin:read', 'admin:write']::text[]
FOR SHARE;

-- name: LockWriteConnectionExclusive :one
-- Serialises proposal creation with reconsent and revocation.
SELECT active_generation
FROM admin_mcp_connections
WHERE id = @id
  AND oauth_client_id = @oauth_client_id
  AND subject_urn = @subject_urn
  AND revoked_at IS NULL
  AND reauthorization_required_at IS NULL
  AND authorization_expires_at > clock_timestamp()
  AND scopes @> ARRAY['admin:read', 'admin:write']::text[]
FOR UPDATE;

-- name: GetLinkedWriteConnection :one
-- Reads the active connection's encrypted browser session and scopes for
-- same-staff approval checks.
SELECT connection.admin_session_id_enc, connection.scopes
FROM admin_mcp_connections AS connection
JOIN admin_mcp_oauth_clients AS client ON client.id = connection.oauth_client_id
WHERE connection.id = @connection_id
  AND connection.oauth_client_id = @oauth_client_id
  AND connection.active_generation = @generation
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND connection.authorization_expires_at > clock_timestamp()
  AND client.revoked_at IS NULL
  AND (client.client_secret_expires_at IS NULL OR client.client_secret_expires_at > clock_timestamp());

-- name: CountPendingProposals :one
SELECT count(*)
FROM admin_mcp_write_proposals
WHERE subject_urn = @subject_urn
  AND oauth_client_id = @oauth_client_id
  AND status = 'pending_approval'
  AND expires_at > @now;

-- name: InsertProposal :one
-- Returns no row when a concurrent request already used the retry key.
INSERT INTO admin_mcp_write_proposals (
  subject_urn, oauth_client_id, connection_id, connection_generation, operation, operation_schema_version,
  platform_global, organization_id, project_id, resource_kind, resource_id, idempotency_key, arguments,
  expected_state_digest, proposal_digest, preview, status, expires_at
) VALUES (
  @subject_urn, @oauth_client_id, @connection_id, @connection_generation, @operation, @operation_schema_version,
  @platform_global, @organization_id, @project_id, @resource_kind, @resource_id, @idempotency_key, @arguments,
  @expected_state_digest, @proposal_digest, @preview, 'pending_approval', @expires_at
)
ON CONFLICT (subject_urn, oauth_client_id, operation, idempotency_key) DO NOTHING
RETURNING *;

-- name: GetProposalByKey :one
SELECT *
FROM admin_mcp_write_proposals
WHERE subject_urn = @subject_urn
  AND oauth_client_id = @oauth_client_id
  AND operation = @operation
  AND idempotency_key = @idempotency_key;

-- name: GetProposal :one
SELECT *
FROM admin_mcp_write_proposals
WHERE id = @id;

-- name: LockProposal :one
SELECT *
FROM admin_mcp_write_proposals
WHERE id = @id
FOR UPDATE;

-- name: CloseProposal :execrows
-- Records a terminal refusal state. Only invalidation stores its reason on the
-- proposal; every close also writes a staff event.
UPDATE admin_mcp_write_proposals
SET status = @status::text,
    invalidated_at = CASE WHEN @status::text = 'invalidated' THEN clock_timestamp() ELSE invalidated_at END,
    invalidation_reason = CASE WHEN @status::text = 'invalidated' THEN @reason::text ELSE invalidation_reason END,
    updated_at = clock_timestamp()
WHERE id = @id
  AND status IN ('pending_approval', 'approved');

-- name: ApproveProposal :one
UPDATE admin_mcp_write_proposals
SET status = 'approved',
    approved_by_subject_urn = @approved_by_subject_urn::text,
    approved_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- name: RejectProposal :one
UPDATE admin_mcp_write_proposals
SET status = 'rejected',
    rejected_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- name: RecordProposalReceipt :one
-- Consumes the approval. Returns no row unless the proposal is still approved.
UPDATE admin_mcp_write_proposals
SET status = 'succeeded',
    executed_at = clock_timestamp(),
    result_code = @result_code::text,
    result_payload = @result_payload,
    updated_at = clock_timestamp()
WHERE id = @id
  AND status = 'approved'
RETURNING *;

-- name: RegisterOAuthClient :exec
INSERT INTO admin_mcp_oauth_clients (client_id, client_name, client_secret_hash, redirect_uris)
VALUES (@client_id, @client_name, NULLIF(@client_secret_hash::text, ''), @redirect_uris::text[]);

-- name: GetLiveOAuthClient :one
SELECT client_id, client_name, client_secret_hash, redirect_uris, client_secret_expires_at
FROM admin_mcp_oauth_clients
WHERE client_id = @client_id
  AND revoked_at IS NULL;

-- name: LockOAuthClientForConsent :one
-- Consent locks the client row before the connection row. Proposal work takes
-- the same order.
SELECT id
FROM admin_mcp_oauth_clients
WHERE client_id = @client_id
  AND revoked_at IS NULL
  AND (client_secret_expires_at IS NULL OR client_secret_expires_at > clock_timestamp())
FOR UPDATE;

-- name: UpsertStaffConnection :one
-- Reconsent rotates the active generation, invalidating every earlier session
-- and grant for this staff subject and client.
INSERT INTO admin_mcp_connections
  (subject_urn, oauth_client_id, admin_session_id_enc, scopes, resource_uri, authorization_expires_at)
VALUES (@subject_urn, @oauth_client_id, @admin_session_id_enc, @scopes, @resource_uri, @authorization_expires_at)
ON CONFLICT (subject_urn, oauth_client_id) WHERE revoked_at IS NULL
DO UPDATE SET admin_session_id_enc = EXCLUDED.admin_session_id_enc,
  scopes = EXCLUDED.scopes,
  resource_uri = EXCLUDED.resource_uri,
  active_generation = generate_uuidv7(),
  authorization_expires_at = EXCLUDED.authorization_expires_at,
  authorized_at = clock_timestamp(),
  reauthorized_at = clock_timestamp(),
  reauthorization_required_at = NULL,
  reauthorization_reason = NULL,
  updated_at = clock_timestamp()
RETURNING id;

-- name: InsertAuthorizationGrant :exec
-- Binds the one-use code to the connection's current generation.
INSERT INTO admin_mcp_authorization_grants
  (authorization_code_hash, oauth_client_id, connection_id, connection_generation,
   redirect_uri, code_challenge, scopes, resource_uri, expires_at)
SELECT @authorization_code_hash::text, @oauth_client_id::uuid, connection.id, connection.active_generation,
  @redirect_uri::text, @code_challenge::text, @scopes::text[], @resource_uri::text, @expires_at::timestamptz
FROM admin_mcp_connections AS connection
WHERE connection.id = @connection_id
  AND connection.revoked_at IS NULL;

-- name: GetAuthorizationGrant :one
SELECT sqlc.embed(connection), auth_grant.code_challenge, auth_grant.redirect_uri
FROM admin_mcp_authorization_grants AS auth_grant
JOIN admin_mcp_connections AS connection
  ON connection.id = auth_grant.connection_id AND connection.oauth_client_id = auth_grant.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = auth_grant.oauth_client_id
WHERE auth_grant.authorization_code_hash = @authorization_code_hash
  AND client.client_id = @client_id
  AND auth_grant.consumed_at IS NULL
  AND auth_grant.revoked_at IS NULL
  AND auth_grant.expires_at > clock_timestamp()
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND connection.active_generation = auth_grant.connection_generation
  AND connection.authorization_expires_at > clock_timestamp()
  AND connection.scopes = auth_grant.scopes
  AND connection.resource_uri = auth_grant.resource_uri
  AND client.revoked_at IS NULL
  AND (client.client_secret_expires_at IS NULL OR client.client_secret_expires_at > clock_timestamp());

-- name: LockAuthorizationGrant :one
-- Same checks as GetAuthorizationGrant, locking the grant and connection for
-- the one-use code exchange.
SELECT sqlc.embed(connection), auth_grant.code_challenge, auth_grant.redirect_uri
FROM admin_mcp_authorization_grants AS auth_grant
JOIN admin_mcp_connections AS connection
  ON connection.id = auth_grant.connection_id AND connection.oauth_client_id = auth_grant.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = auth_grant.oauth_client_id
WHERE auth_grant.authorization_code_hash = @authorization_code_hash
  AND client.client_id = @client_id
  AND auth_grant.consumed_at IS NULL
  AND auth_grant.revoked_at IS NULL
  AND auth_grant.expires_at > clock_timestamp()
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND connection.active_generation = auth_grant.connection_generation
  AND connection.authorization_expires_at > clock_timestamp()
  AND connection.scopes = auth_grant.scopes
  AND connection.resource_uri = auth_grant.resource_uri
  AND client.revoked_at IS NULL
  AND (client.client_secret_expires_at IS NULL OR client.client_secret_expires_at > clock_timestamp())
FOR UPDATE OF auth_grant, connection;

-- name: ConsumeAuthorizationGrant :exec
UPDATE admin_mcp_authorization_grants
SET consumed_at = @now,
    updated_at = @now
WHERE authorization_code_hash = @authorization_code_hash;

-- name: InsertStaffSession :exec
INSERT INTO admin_mcp_sessions
  (id, connection_id, oauth_client_id, connection_generation, jti, refresh_token_hash, expires_at, refresh_expires_at)
VALUES (@id, @connection_id, @oauth_client_id, @connection_generation, @jti, @refresh_token_hash, @expires_at, @refresh_expires_at);

-- name: GetRefreshSession :one
SELECT sqlc.embed(connection), session.rotated_at, session.revoked_at, session.refresh_expires_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
  ON connection.id = session.connection_id AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = session.oauth_client_id
WHERE session.refresh_token_hash = @refresh_token_hash
  AND client.client_id = @client_id
  AND client.revoked_at IS NULL
  AND (client.client_secret_expires_at IS NULL OR client.client_secret_expires_at > clock_timestamp())
  AND connection.revoked_at IS NULL
  AND connection.active_generation = session.connection_generation;

-- name: LockReusedRefreshSession :one
-- Loads a presented refresh token regardless of liveness so reuse of a rotated
-- token can terminalize the generation that issued it.
SELECT connection.id AS connection_id,
  session.connection_generation,
  connection.active_generation,
  session.rotated_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection ON connection.id = session.connection_id
JOIN admin_mcp_oauth_clients AS client ON client.id = session.oauth_client_id
WHERE session.refresh_token_hash = @refresh_token_hash
  AND client.client_id = @client_id
FOR UPDATE OF connection, session;

-- name: LockRefreshSession :one
SELECT sqlc.embed(connection),
  session.id AS session_id,
  session.connection_generation AS session_generation,
  session.rotated_at,
  session.revoked_at AS session_revoked_at,
  session.refresh_expires_at,
  client.revoked_at AS client_revoked_at,
  client.client_secret_expires_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
  ON connection.id = session.connection_id AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = session.oauth_client_id
WHERE session.refresh_token_hash = @refresh_token_hash
  AND client.client_id = @client_id
FOR UPDATE OF session, connection;

-- name: RequireReauthorizationForRefreshReuse :exec
-- Terminalizes only the generation that issued the reused token, not a later
-- staff reauthorization that happened while the request waited.
UPDATE admin_mcp_connections
SET reauthorization_required_at = @now,
    reauthorization_reason = 'refresh_reuse',
    updated_at = @now
WHERE id = @id
  AND active_generation = @generation
  AND revoked_at IS NULL;

-- name: RotateStaffSession :exec
UPDATE admin_mcp_sessions
SET rotated_at = @now,
    revoked_at = @now,
    replaced_by_session_id = @replaced_by_session_id,
    updated_at = @now
WHERE id = @id;

-- name: GetActiveStaffAccessSession :one
SELECT connection.subject_urn,
  client.client_id,
  client.id::text AS client_row_id,
  connection.id::text AS connection_id,
  session.connection_generation::text AS session_generation,
  connection.active_generation::text AS active_generation,
  connection.resource_uri,
  connection.scopes,
  connection.admin_session_id_enc,
  session.expires_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
  ON connection.id = session.connection_id AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = connection.oauth_client_id
WHERE session.jti = @jti
  AND session.revoked_at IS NULL
  AND session.expires_at > clock_timestamp()
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND connection.authorization_expires_at > clock_timestamp()
  AND client.revoked_at IS NULL;

-- name: CountWriteEventsFixture :one
-- Test-only: counts staff event trail rows for one proposal and event.
SELECT count(*)
FROM admin_mcp_write_events
WHERE proposal_id = @proposal_id::uuid
  AND event = @event;

-- name: RevokeOAuthClientFixture :exec
-- Test-only: simulates a staff client revoked after a proposal was approved.
UPDATE admin_mcp_oauth_clients
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: SetConnectionScopesFixture :exec
-- Test-only: simulates a scope change between proposal preview and approval.
UPDATE admin_mcp_connections
SET scopes = @scopes,
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: GetConnectionGenerationFixture :one
-- Test-only: reads the connection generation after reconsent.
SELECT active_generation
FROM admin_mcp_connections
WHERE id = @id;

-- name: GetReauthorizationReasonFixture :one
-- Test-only: reads why a connection now requires staff reauthorization.
SELECT reauthorization_reason
FROM admin_mcp_connections
WHERE id = @id;

-- name: RenameOrganizationFixture :execrows
-- Test-only: a synthetic business write run inside proposal execution.
UPDATE organization_metadata
SET name = @name
WHERE id = @id;
