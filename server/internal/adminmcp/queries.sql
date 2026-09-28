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

-- name: RenameOrganizationFixture :execrows
-- Test-only: a synthetic business write run inside proposal execution.
UPDATE organization_metadata
SET name = @name
WHERE id = @id;
