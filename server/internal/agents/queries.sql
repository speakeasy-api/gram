-- name: CreateAgent :one
-- project_id is nullable while agents created before project scoping carry
-- none. The composite foreign key pins it to the agent's own organization, so
-- a project from another tenant is rejected by the database rather than here.
INSERT INTO agents (
  organization_id,
  owner_user_id,
  project_id,
  name
) VALUES (
  @organization_id,
  @owner_user_id,
  sqlc.narg('project_id'),
  @name
)
RETURNING *;

-- name: CreateAgentWithID :one
INSERT INTO agents (
  id,
  organization_id,
  owner_user_id,
  project_id,
  name
) VALUES (
  @id,
  @organization_id,
  @owner_user_id,
  sqlc.narg('project_id'),
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
WITH target AS MATERIALIZED (
  SELECT a.id, a.organization_id FROM agents a
  WHERE a.organization_id = @organization_id
  AND a.id = @id
  AND a.deleted IS FALSE
  AND a.owner_reassignment_required_at IS NULL
  AND a.owner_user_id <> @owner_user_id
  ORDER BY a.id
  FOR UPDATE
), revoked AS (
  UPDATE user_sessions s
  SET deleted_at = clock_timestamp()
  FROM target, user_session_issuers si
LEFT JOIN projects ip ON ip.id = si.project_id
  WHERE si.id = s.user_session_issuer_id
    AND s.deleted IS FALSE
  AND COALESCE(s.organization_id, si.organization_id, (SELECT organization_id FROM projects WHERE id = s.project_id), ip.organization_id) = target.organization_id
  AND (s.organization_id IS NULL OR s.organization_id = target.organization_id)
  AND (si.organization_id IS NULL OR si.organization_id = target.organization_id)
  AND ((SELECT organization_id FROM projects WHERE id = s.project_id) IS NULL OR (SELECT organization_id FROM projects WHERE id = s.project_id) = target.organization_id)
  AND (ip.organization_id IS NULL OR ip.organization_id = target.organization_id)
    AND (SELECT wa.agent_id
    FROM workload_agent_assignments wa
    JOIN workload_issuers wi ON wi.id = wa.workload_issuer_id AND wi.organization_id = wa.organization_id
    WHERE wa.organization_id = target.organization_id AND wa.deleted IS FALSE
      AND ((wa.match_kind = 'exact' AND s.subject_urn = 'workload:' || wa.workload_issuer_id::text || ':' || wa.subject)
        OR (wa.match_kind = 'wildcard' AND wi.issuer_kind = 'remote' AND wi.allow_wildcard_admission
            AND length(wa.subject) > 1 AND right(wa.subject, 1) = '*'
            AND starts_with(s.subject_urn, 'workload:' || wa.workload_issuer_id::text || ':' || left(wa.subject, length(wa.subject) - 1))))
    ORDER BY (wa.match_kind = 'exact') DESC, length(wa.subject) DESC
    LIMIT 1) = target.id
  RETURNING s.id
)
UPDATE agents
SET owner_user_id = @owner_user_id,
    identity_epoch = identity_epoch + 1,
    updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM target)
RETURNING *;

-- name: ReassignAgent :one
WITH target AS MATERIALIZED (
  SELECT a.id, a.organization_id FROM agents a
  WHERE a.organization_id = @organization_id
  AND a.id = @id
  AND a.deleted IS FALSE
  AND a.owner_reassignment_required_at IS NOT NULL
  ORDER BY a.id
  FOR UPDATE
), revoked AS (
  UPDATE user_sessions s
  SET deleted_at = clock_timestamp()
  FROM target, user_session_issuers si
LEFT JOIN projects ip ON ip.id = si.project_id
  WHERE si.id = s.user_session_issuer_id
    AND s.deleted IS FALSE
  AND COALESCE(s.organization_id, si.organization_id, (SELECT organization_id FROM projects WHERE id = s.project_id), ip.organization_id) = target.organization_id
  AND (s.organization_id IS NULL OR s.organization_id = target.organization_id)
  AND (si.organization_id IS NULL OR si.organization_id = target.organization_id)
  AND ((SELECT organization_id FROM projects WHERE id = s.project_id) IS NULL OR (SELECT organization_id FROM projects WHERE id = s.project_id) = target.organization_id)
  AND (ip.organization_id IS NULL OR ip.organization_id = target.organization_id)
    AND (SELECT wa.agent_id
    FROM workload_agent_assignments wa
    JOIN workload_issuers wi ON wi.id = wa.workload_issuer_id AND wi.organization_id = wa.organization_id
    WHERE wa.organization_id = target.organization_id AND wa.deleted IS FALSE
      AND ((wa.match_kind = 'exact' AND s.subject_urn = 'workload:' || wa.workload_issuer_id::text || ':' || wa.subject)
        OR (wa.match_kind = 'wildcard' AND wi.issuer_kind = 'remote' AND wi.allow_wildcard_admission
            AND length(wa.subject) > 1 AND right(wa.subject, 1) = '*'
            AND starts_with(s.subject_urn, 'workload:' || wa.workload_issuer_id::text || ':' || left(wa.subject, length(wa.subject) - 1))))
    ORDER BY (wa.match_kind = 'exact') DESC, length(wa.subject) DESC
    LIMIT 1) = target.id
  RETURNING s.id
)
UPDATE agents
SET owner_user_id = @owner_user_id,
    owner_reassignment_required_at = NULL,
    owner_reassignment_reason = NULL,
    identity_epoch = identity_epoch + 1,
    updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM target)
RETURNING *;

-- name: LatchAgentsForOwnerLossByUser :many
WITH target AS MATERIALIZED (
  SELECT a.id, a.organization_id FROM agents a
  WHERE a.owner_user_id = @owner_user_id
  AND a.owner_reassignment_required_at IS NULL
  ORDER BY a.id
  FOR UPDATE
), revoked AS (
  UPDATE user_sessions s
  SET deleted_at = clock_timestamp()
  FROM target, user_session_issuers si
LEFT JOIN projects ip ON ip.id = si.project_id
  WHERE si.id = s.user_session_issuer_id
    AND s.deleted IS FALSE
  AND COALESCE(s.organization_id, si.organization_id, (SELECT organization_id FROM projects WHERE id = s.project_id), ip.organization_id) = target.organization_id
  AND (s.organization_id IS NULL OR s.organization_id = target.organization_id)
  AND (si.organization_id IS NULL OR si.organization_id = target.organization_id)
  AND ((SELECT organization_id FROM projects WHERE id = s.project_id) IS NULL OR (SELECT organization_id FROM projects WHERE id = s.project_id) = target.organization_id)
  AND (ip.organization_id IS NULL OR ip.organization_id = target.organization_id)
    AND (SELECT wa.agent_id
    FROM workload_agent_assignments wa
    JOIN workload_issuers wi ON wi.id = wa.workload_issuer_id AND wi.organization_id = wa.organization_id
    WHERE wa.organization_id = target.organization_id AND wa.deleted IS FALSE
      AND ((wa.match_kind = 'exact' AND s.subject_urn = 'workload:' || wa.workload_issuer_id::text || ':' || wa.subject)
        OR (wa.match_kind = 'wildcard' AND wi.issuer_kind = 'remote' AND wi.allow_wildcard_admission
            AND length(wa.subject) > 1 AND right(wa.subject, 1) = '*'
            AND starts_with(s.subject_urn, 'workload:' || wa.workload_issuer_id::text || ':' || left(wa.subject, length(wa.subject) - 1))))
    ORDER BY (wa.match_kind = 'exact') DESC, length(wa.subject) DESC
    LIMIT 1) = target.id
  RETURNING s.id
)
UPDATE agents
SET owner_reassignment_required_at = clock_timestamp(),
    owner_reassignment_reason = @owner_reassignment_reason,
    identity_epoch = identity_epoch + 1,
    updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM target)
RETURNING *;

-- name: LatchAgentsForOwnerLossByMembership :many
WITH target AS MATERIALIZED (
  SELECT a.id, a.organization_id FROM agents a
  WHERE a.organization_id = @organization_id
  AND a.owner_user_id = @owner_user_id
  AND a.owner_reassignment_required_at IS NULL
  ORDER BY a.id
  FOR UPDATE
), revoked AS (
  UPDATE user_sessions s
  SET deleted_at = clock_timestamp()
  FROM target, user_session_issuers si
LEFT JOIN projects ip ON ip.id = si.project_id
  WHERE si.id = s.user_session_issuer_id
    AND s.deleted IS FALSE
  AND COALESCE(s.organization_id, si.organization_id, (SELECT organization_id FROM projects WHERE id = s.project_id), ip.organization_id) = target.organization_id
  AND (s.organization_id IS NULL OR s.organization_id = target.organization_id)
  AND (si.organization_id IS NULL OR si.organization_id = target.organization_id)
  AND ((SELECT organization_id FROM projects WHERE id = s.project_id) IS NULL OR (SELECT organization_id FROM projects WHERE id = s.project_id) = target.organization_id)
  AND (ip.organization_id IS NULL OR ip.organization_id = target.organization_id)
    AND (SELECT wa.agent_id
    FROM workload_agent_assignments wa
    JOIN workload_issuers wi ON wi.id = wa.workload_issuer_id AND wi.organization_id = wa.organization_id
    WHERE wa.organization_id = target.organization_id AND wa.deleted IS FALSE
      AND ((wa.match_kind = 'exact' AND s.subject_urn = 'workload:' || wa.workload_issuer_id::text || ':' || wa.subject)
        OR (wa.match_kind = 'wildcard' AND wi.issuer_kind = 'remote' AND wi.allow_wildcard_admission
            AND length(wa.subject) > 1 AND right(wa.subject, 1) = '*'
            AND starts_with(s.subject_urn, 'workload:' || wa.workload_issuer_id::text || ':' || left(wa.subject, length(wa.subject) - 1))))
    ORDER BY (wa.match_kind = 'exact') DESC, length(wa.subject) DESC
    LIMIT 1) = target.id
  RETURNING s.id
)
UPDATE agents
SET owner_reassignment_required_at = clock_timestamp(),
    owner_reassignment_reason = @owner_reassignment_reason,
    identity_epoch = identity_epoch + 1,
    updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM target)
RETURNING *;

-- name: SuspendAgent :one
WITH target AS MATERIALIZED (
  SELECT a.id, a.organization_id FROM agents a
  WHERE a.organization_id = @organization_id
  AND a.id = @id
  AND a.deleted IS FALSE
  AND a.suspended_at IS NULL
  AND a.revoked_at IS NULL
  ORDER BY a.id
  FOR UPDATE
), revoked AS (
  UPDATE user_sessions s
  SET deleted_at = clock_timestamp()
  FROM target, user_session_issuers si
LEFT JOIN projects ip ON ip.id = si.project_id
  WHERE si.id = s.user_session_issuer_id
    AND s.deleted IS FALSE
  AND COALESCE(s.organization_id, si.organization_id, (SELECT organization_id FROM projects WHERE id = s.project_id), ip.organization_id) = target.organization_id
  AND (s.organization_id IS NULL OR s.organization_id = target.organization_id)
  AND (si.organization_id IS NULL OR si.organization_id = target.organization_id)
  AND ((SELECT organization_id FROM projects WHERE id = s.project_id) IS NULL OR (SELECT organization_id FROM projects WHERE id = s.project_id) = target.organization_id)
  AND (ip.organization_id IS NULL OR ip.organization_id = target.organization_id)
    AND (SELECT wa.agent_id
    FROM workload_agent_assignments wa
    JOIN workload_issuers wi ON wi.id = wa.workload_issuer_id AND wi.organization_id = wa.organization_id
    WHERE wa.organization_id = target.organization_id AND wa.deleted IS FALSE
      AND ((wa.match_kind = 'exact' AND s.subject_urn = 'workload:' || wa.workload_issuer_id::text || ':' || wa.subject)
        OR (wa.match_kind = 'wildcard' AND wi.issuer_kind = 'remote' AND wi.allow_wildcard_admission
            AND length(wa.subject) > 1 AND right(wa.subject, 1) = '*'
            AND starts_with(s.subject_urn, 'workload:' || wa.workload_issuer_id::text || ':' || left(wa.subject, length(wa.subject) - 1))))
    ORDER BY (wa.match_kind = 'exact') DESC, length(wa.subject) DESC
    LIMIT 1) = target.id
  RETURNING s.id
)
UPDATE agents
SET suspended_at = clock_timestamp(),
    identity_epoch = identity_epoch + 1,
    updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM target)
RETURNING *;

-- name: ResumeAgent :one
WITH target AS MATERIALIZED (
  SELECT a.id, a.organization_id FROM agents a
  WHERE a.organization_id = @organization_id
  AND a.id = @id
  AND a.deleted IS FALSE
  AND a.suspended_at IS NOT NULL
  AND a.revoked_at IS NULL
  ORDER BY a.id
  FOR UPDATE
), revoked AS (
  UPDATE user_sessions s
  SET deleted_at = clock_timestamp()
  FROM target, user_session_issuers si
LEFT JOIN projects ip ON ip.id = si.project_id
  WHERE si.id = s.user_session_issuer_id
    AND s.deleted IS FALSE
  AND COALESCE(s.organization_id, si.organization_id, (SELECT organization_id FROM projects WHERE id = s.project_id), ip.organization_id) = target.organization_id
  AND (s.organization_id IS NULL OR s.organization_id = target.organization_id)
  AND (si.organization_id IS NULL OR si.organization_id = target.organization_id)
  AND ((SELECT organization_id FROM projects WHERE id = s.project_id) IS NULL OR (SELECT organization_id FROM projects WHERE id = s.project_id) = target.organization_id)
  AND (ip.organization_id IS NULL OR ip.organization_id = target.organization_id)
    AND (SELECT wa.agent_id
    FROM workload_agent_assignments wa
    JOIN workload_issuers wi ON wi.id = wa.workload_issuer_id AND wi.organization_id = wa.organization_id
    WHERE wa.organization_id = target.organization_id AND wa.deleted IS FALSE
      AND ((wa.match_kind = 'exact' AND s.subject_urn = 'workload:' || wa.workload_issuer_id::text || ':' || wa.subject)
        OR (wa.match_kind = 'wildcard' AND wi.issuer_kind = 'remote' AND wi.allow_wildcard_admission
            AND length(wa.subject) > 1 AND right(wa.subject, 1) = '*'
            AND starts_with(s.subject_urn, 'workload:' || wa.workload_issuer_id::text || ':' || left(wa.subject, length(wa.subject) - 1))))
    ORDER BY (wa.match_kind = 'exact') DESC, length(wa.subject) DESC
    LIMIT 1) = target.id
  RETURNING s.id
)
UPDATE agents
SET suspended_at = NULL,
    identity_epoch = identity_epoch + 1,
    updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM target)
RETURNING *;

-- name: RevokeAgent :one
WITH target AS MATERIALIZED (
  SELECT a.id, a.organization_id FROM agents a
  WHERE a.organization_id = @organization_id
  AND a.id = @id
  AND a.deleted IS FALSE
  AND a.revoked_at IS NULL
  ORDER BY a.id
  FOR UPDATE
), revoked AS (
  UPDATE user_sessions s
  SET deleted_at = clock_timestamp()
  FROM target, user_session_issuers si
LEFT JOIN projects ip ON ip.id = si.project_id
  WHERE si.id = s.user_session_issuer_id
    AND s.deleted IS FALSE
  AND COALESCE(s.organization_id, si.organization_id, (SELECT organization_id FROM projects WHERE id = s.project_id), ip.organization_id) = target.organization_id
  AND (s.organization_id IS NULL OR s.organization_id = target.organization_id)
  AND (si.organization_id IS NULL OR si.organization_id = target.organization_id)
  AND ((SELECT organization_id FROM projects WHERE id = s.project_id) IS NULL OR (SELECT organization_id FROM projects WHERE id = s.project_id) = target.organization_id)
  AND (ip.organization_id IS NULL OR ip.organization_id = target.organization_id)
    AND (SELECT wa.agent_id
    FROM workload_agent_assignments wa
    JOIN workload_issuers wi ON wi.id = wa.workload_issuer_id AND wi.organization_id = wa.organization_id
    WHERE wa.organization_id = target.organization_id AND wa.deleted IS FALSE
      AND ((wa.match_kind = 'exact' AND s.subject_urn = 'workload:' || wa.workload_issuer_id::text || ':' || wa.subject)
        OR (wa.match_kind = 'wildcard' AND wi.issuer_kind = 'remote' AND wi.allow_wildcard_admission
            AND length(wa.subject) > 1 AND right(wa.subject, 1) = '*'
            AND starts_with(s.subject_urn, 'workload:' || wa.workload_issuer_id::text || ':' || left(wa.subject, length(wa.subject) - 1))))
    ORDER BY (wa.match_kind = 'exact') DESC, length(wa.subject) DESC
    LIMIT 1) = target.id
  RETURNING s.id
)
UPDATE agents
SET suspended_at = NULL,
    revoked_at = clock_timestamp(),
    identity_epoch = identity_epoch + 1,
    updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM target)
RETURNING *;

-- name: DeleteAgent :one
WITH target AS MATERIALIZED (
  SELECT a.id, a.organization_id FROM agents a
  WHERE a.organization_id = @organization_id
  AND a.id = @id
  AND a.deleted IS FALSE
  ORDER BY a.id
  FOR UPDATE
), revoked AS (
  UPDATE user_sessions s
  SET deleted_at = clock_timestamp()
  FROM target, user_session_issuers si
LEFT JOIN projects ip ON ip.id = si.project_id
  WHERE si.id = s.user_session_issuer_id
    AND s.deleted IS FALSE
  AND COALESCE(s.organization_id, si.organization_id, (SELECT organization_id FROM projects WHERE id = s.project_id), ip.organization_id) = target.organization_id
  AND (s.organization_id IS NULL OR s.organization_id = target.organization_id)
  AND (si.organization_id IS NULL OR si.organization_id = target.organization_id)
  AND ((SELECT organization_id FROM projects WHERE id = s.project_id) IS NULL OR (SELECT organization_id FROM projects WHERE id = s.project_id) = target.organization_id)
  AND (ip.organization_id IS NULL OR ip.organization_id = target.organization_id)
    AND (SELECT wa.agent_id
    FROM workload_agent_assignments wa
    JOIN workload_issuers wi ON wi.id = wa.workload_issuer_id AND wi.organization_id = wa.organization_id
    WHERE wa.organization_id = target.organization_id AND wa.deleted IS FALSE
      AND ((wa.match_kind = 'exact' AND s.subject_urn = 'workload:' || wa.workload_issuer_id::text || ':' || wa.subject)
        OR (wa.match_kind = 'wildcard' AND wi.issuer_kind = 'remote' AND wi.allow_wildcard_admission
            AND length(wa.subject) > 1 AND right(wa.subject, 1) = '*'
            AND starts_with(s.subject_urn, 'workload:' || wa.workload_issuer_id::text || ':' || left(wa.subject, length(wa.subject) - 1))))
    ORDER BY (wa.match_kind = 'exact') DESC, length(wa.subject) DESC
    LIMIT 1) = target.id
  RETURNING s.id
)
UPDATE agents
SET deleted_at = clock_timestamp(),
    identity_epoch = identity_epoch + 1,
    updated_at = clock_timestamp()
WHERE id IN (SELECT id FROM target)
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

-- name: ListAgentOwnerProfiles :many
SELECT u.id, u.display_name, u.photo_url
FROM users AS u
JOIN organization_user_relationships AS membership
  ON membership.organization_id = @organization_id
 AND membership.user_id = u.id
 AND membership.deleted_at IS NULL
WHERE u.id = ANY(@owner_user_ids::text[]) AND u.deleted_at IS NULL;

-- name: ListManagedAgentSessions :many
-- All known tenancy sources must agree before legacy fallback.
SELECT s.id, s.project_id, s.user_session_issuer_id, iss.slug AS issuer_slug,
       c.client_name, s.authorizer_user_id, s.created_at, s.expires_at,
       s.refresh_expires_at, s.last_used_at
FROM user_sessions AS s
JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
LEFT JOIN projects AS p ON p.id = iss.project_id
LEFT JOIN projects AS session_project ON session_project.id = s.project_id
LEFT JOIN user_session_clients AS c ON c.id = s.user_session_client_id AND c.user_session_issuer_id = iss.id
WHERE COALESCE(s.organization_id, iss.organization_id, p.organization_id, session_project.organization_id) = @organization_id::text
  AND (s.organization_id IS NULL OR s.organization_id = @organization_id::text)
  AND (iss.organization_id IS NULL OR iss.organization_id = @organization_id::text)
  AND (p.organization_id IS NULL OR p.organization_id = @organization_id::text)
  AND (session_project.organization_id IS NULL OR session_project.organization_id = @organization_id::text)
  AND s.subject_urn = @agent_subject::text
  AND s.deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR s.id < sqlc.narg('cursor')::uuid)
ORDER BY s.id DESC
LIMIT @limit_value;

-- name: RevokeManagedAgentSession :one
-- Lock the pre-update row so retries invalidate caches without duplicating audit.
-- All known tenancy sources must agree before legacy fallback.
WITH target AS MATERIALIZED (
  SELECT s.id, s.deleted, iss.project_id AS issuer_project_id
  FROM user_sessions AS s
  JOIN user_session_issuers AS iss ON iss.id = s.user_session_issuer_id
  LEFT JOIN projects AS p ON p.id = iss.project_id
  LEFT JOIN projects AS session_project ON session_project.id = s.project_id
  WHERE COALESCE(s.organization_id, iss.organization_id, p.organization_id, session_project.organization_id) = @organization_id::text
    AND (s.organization_id IS NULL OR s.organization_id = @organization_id::text)
    AND (iss.organization_id IS NULL OR iss.organization_id = @organization_id::text)
    AND (p.organization_id IS NULL OR p.organization_id = @organization_id::text)
    AND (session_project.organization_id IS NULL OR session_project.organization_id = @organization_id::text)
    AND s.subject_urn = @agent_subject::text
    AND s.id = @id
  FOR UPDATE OF s
)
UPDATE user_sessions AS s
SET deleted_at = COALESCE(s.deleted_at, clock_timestamp())
FROM target
WHERE s.id = target.id
RETURNING s.id, s.project_id, s.user_session_issuer_id, s.jti, s.deleted_at, target.issuer_project_id, target.deleted AS already_revoked;

-- name: AdvanceAgentIdentityEpoch :exec
UPDATE agents SET identity_epoch = identity_epoch + 1, updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND id = @agent_id;
