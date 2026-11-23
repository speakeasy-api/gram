-- name: ListShadowMCPOfficialServerChoices :many
-- An administrator's choices about official servers in one project. The
-- chosen server comes back only while it is live and enabled, so a choice
-- whose server was removed falls back to the server found by URL. A choice
-- with no server (link_mcp_server_id NULL) keeps the Shadow MCP server in
-- direct use: nothing is linked, not even automatically.
SELECT
    c.server_url,
    c.mcp_server_id AS link_mcp_server_id,
    m.id AS chosen_mcp_server_id,
    m.name AS chosen_mcp_server_name,
    m.slug AS chosen_mcp_server_slug
FROM shadow_mcp_official_servers AS c
LEFT JOIN mcp_servers AS m
    ON m.id = c.mcp_server_id
    AND m.project_id = c.project_id
    AND m.deleted IS FALSE
    AND m.visibility <> 'disabled'
WHERE c.project_id = @project_id
    AND c.deleted IS FALSE;

-- name: GetShadowMCPOfficialServerChoice :one
-- One choice, read the same way as ListShadowMCPOfficialServerChoices.
SELECT
    c.server_url,
    c.mcp_server_id AS link_mcp_server_id,
    m.id AS chosen_mcp_server_id,
    m.name AS chosen_mcp_server_name,
    m.slug AS chosen_mcp_server_slug
FROM shadow_mcp_official_servers AS c
LEFT JOIN mcp_servers AS m
    ON m.id = c.mcp_server_id
    AND m.project_id = c.project_id
    AND m.deleted IS FALSE
    AND m.visibility <> 'disabled'
WHERE c.project_id = @project_id
    AND c.server_url = @server_url
    AND c.deleted IS FALSE;

-- name: LockShadowMCPOfficialServerURL :exec
-- Serializes every write to one URL's choice, including the first, which has
-- no row yet for LockShadowMCPOfficialServerChoice to lock.
SELECT pg_advisory_xact_lock(hashtextextended('shadow_mcp_official_servers:' || (@project_id::uuid)::text || ':' || @server_url::text, 0));

-- name: LockShadowMCPOfficialServerChoice :one
-- The live choice row for one URL, locked for the update that replaces it.
-- Columns are named so that dropping unused ones never breaks this query.
SELECT id, server_url, mcp_server_id
FROM shadow_mcp_official_servers
WHERE project_id = @project_id
    AND server_url = @server_url
    AND deleted IS FALSE
FOR UPDATE;

-- name: UpsertShadowMCPOfficialServerChoice :one
-- Links a URL to a server: inserts the link, or points the live link at
-- another server.
INSERT INTO shadow_mcp_official_servers (
    project_id,
    server_url,
    mcp_server_id,
    created_by_user_id,
    updated_by_user_id
) VALUES (
    @project_id,
    @server_url,
    sqlc.narg(mcp_server_id),
    sqlc.narg(user_id),
    sqlc.narg(user_id)
)
ON CONFLICT (project_id, server_url) WHERE deleted IS FALSE DO UPDATE SET
    mcp_server_id = EXCLUDED.mcp_server_id,
    updated_by_user_id = EXCLUDED.updated_by_user_id,
    updated_at = clock_timestamp()
RETURNING id, server_url, mcp_server_id;

-- name: RestoreShadowMCPOfficialServerChoice :one
-- Adding a removed link again restores the most recently removed row for the
-- URL, so the link keeps one id across its history. Callers hold
-- LockShadowMCPOfficialServerURL and have checked that no live row exists.
UPDATE shadow_mcp_official_servers
SET deleted_at = NULL,
    mcp_server_id = sqlc.narg(mcp_server_id),
    updated_by_user_id = sqlc.narg(user_id),
    updated_at = clock_timestamp()
WHERE id = (
    SELECT prev.id
    FROM shadow_mcp_official_servers AS prev
    WHERE prev.project_id = @project_id
        AND prev.server_url = @server_url
        AND prev.deleted IS TRUE
    ORDER BY prev.deleted_at DESC
    LIMIT 1
)
RETURNING id, server_url, mcp_server_id;

-- name: DeleteShadowMCPOfficialServerChoice :execrows
UPDATE shadow_mcp_official_servers
SET deleted_at = clock_timestamp(),
    updated_by_user_id = sqlc.narg(user_id),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
    AND server_url = @server_url
    AND deleted IS FALSE;

-- name: GetLiveMCPServerForShadowMCPChoice :one
-- The server an administrator chooses must be live in the same project.
SELECT id, name, slug, visibility
FROM mcp_servers
WHERE id = @id
    AND project_id = @project_id
    AND deleted IS FALSE;

-- name: ListBlockedShadowMCPServerURLs :many
-- The server URLs a standing decision blocks for one user in a project. With
-- no blocking Shadow MCP policy covering the user, the project is in the
-- built-in blocklist mode: every server connects except these. A denial
-- blocks the people or roles it names, or everyone when it names none. An
-- approval narrowed to people or roles lets only them through, so it blocks
-- everyone else. @principal_urns holds the user's own principals, user:all
-- included. A superseded request carries no standing decision.
SELECT r.target_key
FROM mcp_approval_requests r
JOIN LATERAL (
  SELECT d.decision, d.granted_principal_urns
  FROM mcp_approval_decisions d
  WHERE d.mcp_approval_request_id = r.id
    AND d.project_id = r.project_id
    AND d.deleted IS FALSE
  ORDER BY d.decided_at DESC, d.id DESC
  LIMIT 1
) latest ON TRUE
WHERE r.project_id = @project_id
  AND r.target_kind = 'server_url'
  AND r.deleted IS FALSE
  AND r.status <> 'superseded'
  AND (
    (
      latest.decision = 'denied'
      AND (
        cardinality(latest.granted_principal_urns) = 0
        OR latest.granted_principal_urns && @principal_urns::text[]
      )
    )
    OR (
      latest.decision = 'approved'
      AND cardinality(latest.granted_principal_urns) > 0
      AND NOT (latest.granted_principal_urns && @principal_urns::text[])
    )
  );

-- name: ListStandingShadowMCPDecisions :many
-- Every server URL with a standing decision in a project, with the decision
-- and the people or roles it covers (none means everyone).
SELECT r.target_key, latest.decision, latest.granted_principal_urns
FROM mcp_approval_requests r
JOIN LATERAL (
  SELECT d.decision, d.granted_principal_urns
  FROM mcp_approval_decisions d
  WHERE d.mcp_approval_request_id = r.id
    AND d.project_id = r.project_id
    AND d.deleted IS FALSE
  ORDER BY d.decided_at DESC, d.id DESC
  LIMIT 1
) latest ON TRUE
WHERE r.project_id = @project_id
  AND r.target_kind = 'server_url'
  AND r.deleted IS FALSE
  AND r.status <> 'superseded';
