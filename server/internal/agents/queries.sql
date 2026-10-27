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

-- name: LockAgentIncludingDeleted :one
-- Locks an agent whatever its lifecycle, for cleanup that must still run
-- after the agent is deleted.
SELECT *
FROM agents
WHERE organization_id = @organization_id
  AND id = @id
LIMIT 1
FOR UPDATE;

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
ORDER BY scope, selectors, id;

-- name: GetAgentPolicyGrantForUpdate :one
SELECT id, scope, selectors, created_at, updated_at
FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = concat('agent:', @agent_id::uuid)
  AND id = @grant_id
LIMIT 1
FOR UPDATE;

-- name: CreateAgentPolicyGrant :one
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
VALUES (@organization_id, concat('agent:', @agent_id::uuid), @scope, @selectors)
RETURNING id, scope, selectors, created_at, updated_at;

-- name: UpdateAgentPolicyGrant :one
UPDATE principal_grants
SET scope = @scope,
    selectors = @selectors,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND principal_urn = concat('agent:', @agent_id::uuid)
  AND id = @grant_id
RETURNING id, scope, selectors, created_at, updated_at;

-- name: DeleteAgentPolicyGrant :one
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = concat('agent:', @agent_id::uuid)
  AND id = @grant_id
RETURNING id, scope, selectors, created_at, updated_at;

-- name: ListManagedAgents :many
-- Keyset paginated on the same (LOWER(name), id) tuple it orders by, so a page
-- boundary is a position in that ordering rather than an offset: inserts and
-- deletes elsewhere in the list cannot shift the rows a later page returns,
-- which is what an OFFSET would do. A rename is the exception, because it
-- moves the row itself — one renamed across the boundary can still be seen
-- twice or not at all. The caller drops rows it may not read, so it asks for
-- more than one page and walks the cursor until it has filled one.
SELECT * FROM agents
WHERE organization_id = @organization_id AND deleted IS FALSE
  AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search')::text || '%'
  )
  -- Lifecycle is derived from the same two columns the Go side derives it
  -- from, so a filter cannot disagree with the badge the row shows.
  AND (
    cardinality(@lifecycles::text[]) = 0
    OR (CASE
          WHEN revoked_at IS NOT NULL THEN 'revoked'
          WHEN suspended_at IS NOT NULL THEN 'suspended'
          ELSE 'active'
        END) = ANY(@lifecycles::text[])
  )
  AND (
    cardinality(@owner_user_ids::text[]) = 0
    OR owner_user_id = ANY(@owner_user_ids::text[])
  )
  AND (
    sqlc.narg('registered_after')::timestamptz IS NULL
    OR created_at >= sqlc.narg('registered_after')::timestamptz
  )
  AND (
    sqlc.narg('registered_before')::timestamptz IS NULL
    OR created_at < sqlc.narg('registered_before')::timestamptz
  )
  AND (
    sqlc.narg('cursor_name')::text IS NULL
    OR (
      @sort_descending::boolean IS FALSE
      AND (LOWER(name), id) > (sqlc.narg('cursor_name')::text, sqlc.narg('cursor_id')::uuid)
    )
    OR (
      @sort_descending::boolean IS TRUE
      AND (LOWER(name), id) < (sqlc.narg('cursor_name')::text, sqlc.narg('cursor_id')::uuid)
    )
  )
ORDER BY
  CASE WHEN @sort_descending::boolean THEN LOWER(name) END DESC,
  CASE WHEN NOT @sort_descending::boolean THEN LOWER(name) END ASC,
  CASE WHEN @sort_descending::boolean THEN id END DESC,
  CASE WHEN NOT @sort_descending::boolean THEN id END ASC
LIMIT @page_limit;

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

-- name: HasOwnedPersonalConnection :one
-- Whether the user holds a live connection through the upstream OAuth client
-- that belongs to this organization, and the organization uses that client for
-- personal connections. The client and its issuer must be live and reach the
-- organization, and the consent must have come through a sign-in of this
-- organization, so another organization's connection through a shared
-- registration never counts.
SELECT (EXISTS (
  SELECT 1
  FROM remote_sessions s
  JOIN remote_session_clients c ON c.id = s.remote_session_client_id AND NOT c.deleted
  JOIN remote_session_issuers i ON i.id = c.remote_session_issuer_id AND NOT i.deleted
  -- A deleted sign-in still names the organization the consent was made in.
  JOIN user_session_issuers u ON u.id = s.user_session_issuer_id
  LEFT JOIN projects up ON up.id = u.project_id
  LEFT JOIN projects cp ON cp.id = c.project_id
  LEFT JOIN projects ip ON ip.id = i.project_id
  JOIN organization_metadata org ON org.id = @organization_id::text AND org.disabled_at IS NULL
  JOIN users person ON person.id = @user_id::text AND person.deleted_at IS NULL AND person.workos_deleted_at IS NULL
  JOIN organization_user_relationships membership ON membership.organization_id = org.id AND membership.user_id = person.id AND NOT membership.deleted
  WHERE s.subject_urn = concat('user:', @user_id::text)
    AND s.remote_session_client_id = @remote_session_client_id
    AND NOT s.deleted
    AND (
      (c.project_id IS NOT NULL AND cp.organization_id = org.id AND NOT cp.deleted)
      OR (c.project_id IS NULL AND (c.organization_id IS NULL OR c.organization_id = org.id))
    )
    AND (
      (i.project_id IS NOT NULL AND ip.organization_id = org.id AND NOT ip.deleted)
      OR (i.project_id IS NULL AND (i.organization_id IS NULL OR i.organization_id = org.id))
    )
    AND (up.organization_id = org.id OR (u.project_id IS NULL AND u.organization_id = org.id))
) AND EXISTS (
  SELECT 1
  FROM mcp_auth_clients ac
  WHERE ac.organization_id = @organization_id::text
    AND ac.remote_session_client_id = @remote_session_client_id
    AND ac.client_type = 'remote_session'
    AND ac.deleted IS FALSE
))::boolean AS owned;

-- name: CreateConnectionBlockGrant :one
-- Writes the block's grant. Inserted is false when an identical grant
-- already existed.
WITH inserted AS (
  INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
  VALUES (@organization_id, concat('agent:', @agent_id::uuid), 'mcp:blocked_auth', @selectors)
  ON CONFLICT DO NOTHING
  RETURNING id
)
SELECT EXISTS (SELECT 1 FROM inserted)::boolean AS inserted;

-- name: RecordConnectionBlock :one
INSERT INTO agent_connection_blocks (organization_id, agent_id, owner_user_id, remote_session_client_id, taken_over_at)
VALUES (
  @organization_id,
  @agent_id,
  @owner_user_id,
  @remote_session_client_id,
  CASE WHEN @grant_existed::boolean THEN clock_timestamp() END
)
ON CONFLICT (organization_id, agent_id, owner_user_id, remote_session_client_id) DO NOTHING
RETURNING *;

-- name: GetConnectionBlock :one
SELECT *
FROM agent_connection_blocks
WHERE organization_id = @organization_id
  AND agent_id = @agent_id
  AND owner_user_id = @owner_user_id
  AND remote_session_client_id = @remote_session_client_id;

-- name: ListConnectionBlocks :many
SELECT *
FROM agent_connection_blocks
WHERE organization_id = @organization_id
  AND owner_user_id = @owner_user_id
  AND (sqlc.narg('cursor')::uuid IS NULL OR id > sqlc.narg('cursor')::uuid)
ORDER BY id
LIMIT @limit_value;

-- name: GetOwnedConnectionBlock :one
SELECT *
FROM agent_connection_blocks
WHERE organization_id = @organization_id
  AND owner_user_id = @owner_user_id
  AND id = @id;

-- name: DeleteConnectionBlock :one
DELETE FROM agent_connection_blocks
WHERE organization_id = @organization_id
  AND owner_user_id = @owner_user_id
  AND id = @id
RETURNING *;

-- name: DeleteConnectionBlockGrant :exec
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = concat('agent:', @agent_id::uuid)
  AND scope = 'mcp:blocked_auth'
  AND selectors = @selectors;

-- name: IsConnectionOwnerBlock :one
-- Whether a connection owner recorded the agent grant, which only that owner
-- may then remove.
SELECT EXISTS (
  SELECT 1
  FROM agent_connection_blocks r
  WHERE r.organization_id = @organization_id
    AND r.agent_id = @agent_id
    AND @scope::text = 'mcp:blocked_auth'
    AND @selectors::jsonb = jsonb_build_object(
      'resource_kind', 'mcp_auth_client',
      'resource_id', '*',
      'client_type', 'remote_session',
      'upstream_client', r.remote_session_client_id::text,
      'connection_owner', r.owner_user_id
    )
);

-- name: ListAgentsNeedingConnectionRefresh :many
-- One tenant-scoped read for the visible page, without exposing source credentials.
SELECT DISTINCT a.id
FROM agents a
JOIN principal_remote_session_bindings b ON b.principal_id = a.id AND b.organization_id = a.organization_id AND b.revoked_at IS NULL
JOIN projects p ON p.id = b.project_id AND p.organization_id = a.organization_id AND NOT p.deleted
LEFT JOIN remote_sessions s ON s.id = b.remote_session_id
WHERE a.organization_id = @organization_id
  AND a.id = ANY(@agent_ids::uuid[])
  AND NOT a.deleted
  AND (
    s.id IS NULL OR s.deleted
    OR NOT EXISTS (SELECT 1 FROM users lender JOIN organization_user_relationships lm ON lm.user_id = lender.id AND lm.organization_id = a.organization_id AND lm.deleted_at IS NULL WHERE 'user:' || lender.id = s.subject_urn AND lender.deleted_at IS NULL)
    OR b.attached_by_subject_id <> s.subject_urn
    OR b.grant_generation <> s.grant_generation
    OR (s.authorization_expires_at IS NOT NULL AND s.authorization_expires_at <= now())
    OR (s.access_expires_at IS NOT NULL AND s.access_expires_at <= now()
      AND (s.refresh_token_encrypted IS NULL OR s.refresh_expires_at <= now()))
  );
