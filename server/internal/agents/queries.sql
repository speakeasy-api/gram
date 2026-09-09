-- name: CreateAgent :one
INSERT INTO agents (
  organization_id,
  owner_user_id,
  name
) VALUES (
  @organization_id,
  @owner_user_id,
  @name
)
RETURNING *;

-- name: CreateAgentWithID :one
INSERT INTO agents (
  id,
  organization_id,
  owner_user_id,
  name
) VALUES (
  @id,
  @organization_id,
  @owner_user_id,
  @name
)
RETURNING *;

-- name: GetAgentByID :one
SELECT *
FROM agents
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
LIMIT 1;

-- name: GetAgentByIDForUpdate :one
SELECT *
FROM agents
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
LIMIT 1
FOR UPDATE;

-- name: ListActiveAgentsForAuthorization :many
-- Candidate selection excludes every lifecycle and owner-admission state that
-- cannot authorize a credential. Caller and live policy checks remain in the
-- service because they require the normal authorization evaluator.
SELECT a.*
FROM agents AS a
JOIN users AS u ON u.id = a.owner_user_id
JOIN organization_user_relationships AS our
  ON our.organization_id = a.organization_id
 AND our.user_id = a.owner_user_id
WHERE a.organization_id = @organization_id
  AND a.deleted IS FALSE
  AND a.suspended_at IS NULL
  AND a.revoked_at IS NULL
  AND a.owner_reassignment_required_at IS NULL
  AND u.deleted_at IS NULL
  AND our.deleted_at IS NULL
ORDER BY LOWER(a.name), a.id;

-- name: RenameAgent :one
UPDATE agents
SET name = @name,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: TransferAgent :one
UPDATE agents
SET owner_user_id = @owner_user_id,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND owner_reassignment_required_at IS NULL
  AND owner_user_id <> @owner_user_id
RETURNING *;

-- name: ReassignAgent :one
UPDATE agents
SET owner_user_id = @owner_user_id,
    owner_reassignment_required_at = NULL,
    owner_reassignment_reason = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND owner_reassignment_required_at IS NOT NULL
RETURNING *;

-- name: LatchAgentsForOwnerLossByUser :many
UPDATE agents
SET owner_reassignment_required_at = clock_timestamp(),
    owner_reassignment_reason = @owner_reassignment_reason,
    updated_at = clock_timestamp()
WHERE owner_user_id = @owner_user_id
  AND owner_reassignment_required_at IS NULL
RETURNING *;

-- name: LatchAgentsForOwnerLossByMembership :many
UPDATE agents
SET owner_reassignment_required_at = clock_timestamp(),
    owner_reassignment_reason = @owner_reassignment_reason,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND owner_user_id = @owner_user_id
  AND owner_reassignment_required_at IS NULL
RETURNING *;

-- name: SuspendAgent :one
UPDATE agents
SET suspended_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND suspended_at IS NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: ResumeAgent :one
UPDATE agents
SET suspended_at = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND suspended_at IS NOT NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeAgent :one
UPDATE agents
SET suspended_at = NULL,
    revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND revoked_at IS NULL
RETURNING *;

-- name: DeleteAgent :one
UPDATE agents
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- Agent direct-policy queries construct the canonical principal from the typed
-- agent ID and bind every operation to the organization and selected agent.

-- name: ListAgentPolicyGrants :many
SELECT id, scope, selectors, created_at, updated_at
FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = concat('agent:', @agent_id::uuid)
  AND COALESCE(effect, 'allow') = 'allow'
ORDER BY scope, selectors, id;

-- name: GetAgentPolicyGrantForUpdate :one
SELECT id, scope, selectors, created_at, updated_at
FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = concat('agent:', @agent_id::uuid)
  AND id = @grant_id
  AND COALESCE(effect, 'allow') = 'allow'
LIMIT 1
FOR UPDATE;

-- name: CreateAgentPolicyGrant :one
INSERT INTO principal_grants (organization_id, principal_urn, scope, effect, selectors)
VALUES (@organization_id, concat('agent:', @agent_id::uuid), @scope, NULL, @selectors)
RETURNING id, scope, selectors, created_at, updated_at;

-- name: UpdateAgentPolicyGrant :one
UPDATE principal_grants
SET scope = @scope,
    selectors = @selectors,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND principal_urn = concat('agent:', @agent_id::uuid)
  AND id = @grant_id
  AND COALESCE(effect, 'allow') = 'allow'
RETURNING id, scope, selectors, created_at, updated_at;

-- name: DeleteAgentPolicyGrant :one
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = concat('agent:', @agent_id::uuid)
  AND id = @grant_id
  AND COALESCE(effect, 'allow') = 'allow'
RETURNING id, scope, selectors, created_at, updated_at;

-- name: ListManagedAgents :many
SELECT * FROM agents
WHERE organization_id = @organization_id AND deleted IS FALSE
ORDER BY LOWER(name), id;

-- name: GetAgentOwnerProfile :one
SELECT u.display_name, u.photo_url
FROM users AS u
JOIN organization_user_relationships AS membership
  ON membership.organization_id = @organization_id
 AND membership.user_id = u.id
 AND membership.deleted_at IS NULL
WHERE u.id = @owner_user_id AND u.deleted_at IS NULL;

-- name: ListManagedAgentSessions :many
SELECT s.id, s.project_id, s.user_session_issuer_id, iss.slug AS issuer_slug,
       c.client_name, s.authorizer_user_id, s.created_at, s.expires_at,
       s.refresh_expires_at, s.last_used_at
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
LEFT JOIN user_session_clients AS c ON c.id = s.user_session_client_id AND c.user_session_issuer_id = iss.id
WHERE s.organization_id = @organization_id
  AND s.subject_urn = @agent_subject::text
  AND s.deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR s.id < sqlc.narg('cursor')::uuid)
ORDER BY s.id DESC
LIMIT @limit_value;

-- name: RevokeManagedAgentSession :one
-- Repeating a revoke retries the post-commit cache push if an earlier push failed.
UPDATE user_sessions AS s
SET deleted_at = COALESCE(s.deleted_at, clock_timestamp())
WHERE s.organization_id = @organization_id
  AND s.subject_urn = @agent_subject::text
  AND s.id = @id
RETURNING s.id, s.project_id, s.user_session_issuer_id, s.jti;
