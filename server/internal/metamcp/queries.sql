-- name: LockUserSessionIssuerForMetaMCP :one
-- Lock a live user session issuer in the caller's project. Meta MCP
-- create/update hold this lock while validating the reference so a concurrent
-- issuer delete cannot race the attach.
SELECT id
FROM user_session_issuers
WHERE id = @user_session_issuer_id
  AND project_id = @project_id::uuid
  AND deleted IS FALSE
FOR UPDATE;

-- name: CreateMetaMCPServer :one
INSERT INTO meta_mcp_servers (
    organization_id,
    project_id,
    name,
    user_session_issuer_id
)
VALUES (
    @organization_id,
    @project_id,
    @name,
    sqlc.narg('user_session_issuer_id')
)
RETURNING *;

-- name: GetMetaMCPServer :one
SELECT *
FROM meta_mcp_servers
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetMetaMCPServerByIDAndProjectID :one
-- Project-scoped lookup for the public endpoint resolution path, which
-- holds an mcp_endpoints row (and so a trusted project id) but no
-- organization context.
SELECT *
FROM meta_mcp_servers
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: LockMetaMCPServer :one
SELECT *
FROM meta_mcp_servers
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: ListMetaMCPServers :many
SELECT *
FROM meta_mcp_servers
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC, id DESC;

-- name: UpdateMetaMCPServer :one
-- Full-record replace: a null user_session_issuer_id clears the reference.
UPDATE meta_mcp_servers
SET name = @name,
    user_session_issuer_id = sqlc.narg('user_session_issuer_id'),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteMetaMCPServer :one
UPDATE meta_mcp_servers
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteMetaMCPMembersByMetaMCPServerID :many
-- Soft-delete all live memberships of a meta MCP server. Used when the parent
-- meta MCP is soft-deleted (FK cascades only fire on hard deletes). Returns
-- the affected rows so the caller can emit per-membership audit events.
UPDATE meta_mcp_server_members
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE meta_mcp_server_id = @meta_mcp_server_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteMetaMCPMembersByMCPServerID :many
-- Soft-delete all live memberships that reference a generic MCP server. Used
-- when the member server is soft-deleted so meta MCPs don't keep live
-- membership rows pointing at a tombstoned server. Returns the affected rows
-- with the owning meta's name so the caller can emit per-membership audit
-- events without re-reading each meta.
UPDATE meta_mcp_server_members
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM meta_mcp_servers
WHERE meta_mcp_servers.id = meta_mcp_server_members.meta_mcp_server_id
  AND meta_mcp_server_members.mcp_server_id = @mcp_server_id
  AND meta_mcp_server_members.project_id = @project_id
  AND meta_mcp_server_members.deleted IS FALSE
RETURNING meta_mcp_server_members.*, meta_mcp_servers.name AS meta_mcp_server_name;

-- name: CreateMetaMCPMember :one
INSERT INTO meta_mcp_server_members (
    project_id,
    meta_mcp_server_id,
    mcp_server_id,
    sort_order
)
VALUES (
    @project_id,
    @meta_mcp_server_id,
    @mcp_server_id,
    @sort_order
)
RETURNING *;

-- name: CountMetaMCPMembersSharingBackend :one
-- Count live members of @meta_mcp_server_id, other than @mcp_server_id, that
-- front one of the given backends. Two mcp_servers rows may name the same
-- backend, and a meta MCP server holding both would serve identical tools
-- under two slugs with nothing to route between them.
--
-- A null argument never matches: `column = NULL` evaluates to NULL, so an
-- unset backend kind cannot pair with a member's null column.
SELECT count(*)
FROM meta_mcp_server_members m
JOIN mcp_servers s
  ON s.id = m.mcp_server_id
 AND s.project_id = m.project_id
 AND s.deleted IS FALSE
WHERE m.meta_mcp_server_id = @meta_mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE
  AND m.mcp_server_id <> @mcp_server_id
  AND (s.remote_mcp_server_id = sqlc.narg('remote_mcp_server_id')
    OR s.tunneled_mcp_server_id = sqlc.narg('tunneled_mcp_server_id')
    OR s.toolset_id = sqlc.narg('toolset_id')
    OR s.unproxied_mcp_server_id = sqlc.narg('unproxied_mcp_server_id'));

-- name: FindMetaMCPSiblingSharingBackend :one
-- Same rule as CountMetaMCPMembersSharingBackend, asked from the member
-- server's side: name a meta MCP server where @mcp_server_id already sits
-- alongside a live co-member fronting one of the given backends. Guards a
-- backend repoint on an already-attached server.
SELECT meta.name
FROM meta_mcp_server_members mine
JOIN meta_mcp_server_members sibling
  ON sibling.meta_mcp_server_id = mine.meta_mcp_server_id
 AND sibling.project_id = mine.project_id
 AND sibling.deleted IS FALSE
 AND sibling.mcp_server_id <> mine.mcp_server_id
JOIN mcp_servers s
  ON s.id = sibling.mcp_server_id
 AND s.project_id = sibling.project_id
 AND s.deleted IS FALSE
JOIN meta_mcp_servers meta
  ON meta.id = mine.meta_mcp_server_id
 AND meta.project_id = mine.project_id
 AND meta.deleted IS FALSE
WHERE mine.mcp_server_id = @mcp_server_id
  AND mine.project_id = @project_id
  AND mine.deleted IS FALSE
  AND (s.remote_mcp_server_id = sqlc.narg('remote_mcp_server_id')
    OR s.tunneled_mcp_server_id = sqlc.narg('tunneled_mcp_server_id')
    OR s.toolset_id = sqlc.narg('toolset_id')
    OR s.unproxied_mcp_server_id = sqlc.narg('unproxied_mcp_server_id'))
LIMIT 1;

-- name: GetMetaMCPMember :one
SELECT *
FROM meta_mcp_server_members
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: LockMetaMCPMember :one
SELECT *
FROM meta_mcp_server_members
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: ListMetaMCPMembers :many
-- List live memberships whose member server is itself still live, with the
-- member server's name and slug joined in for display.
SELECT
    m.id,
    m.mcp_server_id,
    m.sort_order,
    s.name AS mcp_server_name,
    s.slug AS mcp_server_slug
FROM meta_mcp_server_members m
JOIN mcp_servers s
  ON s.id = m.mcp_server_id
 AND s.project_id = m.project_id
 AND s.deleted IS FALSE
WHERE m.meta_mcp_server_id = @meta_mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE
ORDER BY m.sort_order, m.created_at, m.id;

-- name: ListServableMetaMCPMembers :many
-- Serving-path variant of ListMetaMCPMembers: additionally hides members
-- whose server is disabled, matching the resolution path's rule that a
-- disabled server does not exist for unauthenticated callers, and members
-- whose server has no slug (legacy pre-2026-05 rows), which the qualified
-- serverslug--toolname contract cannot address. The dashboard listing keeps
-- the unfiltered query so admins still see every member. Carries the backend
-- and dispatch columns the gateway runtime needs to classify and execute
-- against each member.
SELECT
    m.id,
    m.mcp_server_id,
    m.sort_order,
    s.name AS mcp_server_name,
    s.slug AS mcp_server_slug,
    s.visibility AS mcp_server_visibility,
    s.toolset_id AS mcp_server_toolset_id,
    s.remote_mcp_server_id AS mcp_server_remote_mcp_server_id,
    s.tunneled_mcp_server_id AS mcp_server_tunneled_mcp_server_id,
    s.unproxied_mcp_server_id AS mcp_server_unproxied_mcp_server_id,
    s.environment_id AS mcp_server_environment_id,
    s.tool_variations_group_id AS mcp_server_tool_variations_group_id
FROM meta_mcp_server_members m
JOIN mcp_servers s
  ON s.id = m.mcp_server_id
 AND s.project_id = m.project_id
 AND s.deleted IS FALSE
 AND s.visibility <> 'disabled'
 AND s.slug IS NOT NULL
WHERE m.meta_mcp_server_id = @meta_mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE
ORDER BY m.sort_order, m.created_at, m.id;

-- name: UpdateMetaMCPMemberSortOrder :one
UPDATE meta_mcp_server_members
SET sort_order = @sort_order,
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteMetaMCPMember :one
UPDATE meta_mcp_server_members
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;
