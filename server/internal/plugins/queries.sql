-- Queries for managing plugins (project-scoped distributable MCP server bundles).
-- plugins is project-scoped; every query is scoped to project_id (and organization_id where needed).

-- name: CreatePlugin :one
INSERT INTO plugins (organization_id, project_id, name, slug, description)
VALUES (@organization_id, @project_id, @name, @slug, sqlc.narg('description'))
RETURNING *;

-- name: GetPlugin :one
SELECT *
FROM plugins
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: ListPlugins :many
SELECT
  p.*,
  (SELECT count(*) FROM plugin_servers ps WHERE ps.plugin_id = p.id AND ps.deleted IS FALSE) AS server_count,
  (SELECT count(*) FROM plugin_assignments pa WHERE pa.plugin_id = p.id) AS assignment_count
FROM plugins p
WHERE p.organization_id = @organization_id
  AND p.project_id = @project_id
  AND p.deleted IS FALSE
ORDER BY p.created_at DESC;

-- name: UpdatePlugin :one
UPDATE plugins
SET name = @name,
    slug = @slug,
    description = sqlc.narg('description'),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: SoftDeletePluginServers :exec
-- Soft-deletes all servers belonging to a plugin.
UPDATE plugin_servers
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE plugin_id = @plugin_id
  AND deleted IS FALSE;

-- name: DeletePlugin :exec
UPDATE plugins
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: AddPluginServer :one
INSERT INTO plugin_servers (plugin_id, toolset_id, display_name, policy, sort_order)
VALUES (
  @plugin_id,
  @toolset_id,
  @display_name,
  @policy,
  @sort_order
)
RETURNING *;

-- name: ListPluginServers :many
SELECT *
FROM plugin_servers
WHERE plugin_id = @plugin_id
  AND deleted IS FALSE
ORDER BY sort_order ASC, created_at ASC;

-- name: UpdatePluginServer :one
UPDATE plugin_servers
SET display_name = @display_name,
    policy = @policy,
    sort_order = @sort_order,
    updated_at = clock_timestamp()
WHERE id = @id
  AND plugin_id = @plugin_id
  AND deleted IS FALSE
RETURNING *;

-- name: RemovePluginServer :exec
UPDATE plugin_servers
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND plugin_id = @plugin_id
  AND deleted IS FALSE;

-- name: AddPluginAssignment :one
INSERT INTO plugin_assignments (plugin_id, organization_id, principal_urn)
VALUES (@plugin_id, @organization_id, @principal_urn)
ON CONFLICT (plugin_id, principal_urn) DO UPDATE
  SET principal_urn = EXCLUDED.principal_urn
RETURNING *;

-- name: ListPluginAssignments :many
SELECT *
FROM plugin_assignments
WHERE plugin_id = @plugin_id;

-- name: RemoveAllPluginAssignments :execrows
DELETE FROM plugin_assignments
WHERE plugin_id = @plugin_id;

-- name: ListPluginsWithServersForProject :many
-- Used during plugin generation: returns all active plugin servers joined with
-- their parent plugin and toolset mcp_slug for URL construction.
SELECT
  p.id AS plugin_id,
  p.name AS plugin_name,
  p.slug AS plugin_slug,
  p.description AS plugin_description,
  ps.id AS server_id,
  ps.display_name AS server_display_name,
  ps.policy AS server_policy,
  ps.sort_order AS server_sort_order,
  ps.toolset_id,
  t.mcp_slug AS toolset_mcp_slug,
  t.mcp_is_public AS toolset_is_public
FROM plugins p
JOIN plugin_servers ps ON ps.plugin_id = p.id AND ps.deleted IS FALSE
JOIN toolsets t ON t.id = ps.toolset_id AND t.deleted IS FALSE AND t.mcp_enabled IS TRUE
WHERE p.project_id = @project_id
  AND p.deleted IS FALSE
ORDER BY p.slug, ps.sort_order ASC;

-- name: GetOrganizationName :one
SELECT name FROM organization_metadata WHERE id = @id;

-- name: GetGitHubConnection :one
SELECT *
FROM plugin_github_connections
WHERE project_id = @project_id;

-- name: GetGitHubConnectionByMarketplaceToken :one
-- Resolves a marketplace proxy URL token to the upstream connection. The token
-- is the auth credential — there's no project scope to apply ahead of it.
SELECT *
FROM plugin_github_connections
WHERE marketplace_token = @marketplace_token;

-- name: UpsertGitHubConnection :one
-- Inserts or refreshes a project's GitHub connection. The marketplace_token
-- argument is the candidate token to use if no token is currently set; on
-- conflict the existing token is preserved via COALESCE so callers can pass a
-- freshly-generated token on every publish without overwriting prior state.
-- Token rotation goes through a separate query.
INSERT INTO plugin_github_connections (project_id, installation_id, repo_owner, repo_name, marketplace_token)
VALUES (@project_id, @installation_id, @repo_owner, @repo_name, @marketplace_token)
ON CONFLICT (project_id) DO UPDATE
  SET installation_id = EXCLUDED.installation_id,
      repo_owner = EXCLUDED.repo_owner,
      repo_name = EXCLUDED.repo_name,
      marketplace_token = COALESCE(plugin_github_connections.marketplace_token, EXCLUDED.marketplace_token),
      updated_at = clock_timestamp()
RETURNING *;
