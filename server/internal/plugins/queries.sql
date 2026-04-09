-- Queries for managing plugins (org-scoped distributable MCP server bundles).
-- plugins is org-scoped (no project_id); every query is scoped to organization_id.

-- name: CreatePlugin :one
INSERT INTO plugins (organization_id, name, slug, description)
VALUES (@organization_id, @name, @slug, sqlc.narg('description'))
RETURNING *;

-- name: GetPlugin :one
SELECT *
FROM plugins
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: ListPlugins :many
SELECT
  p.*,
  (SELECT count(*) FROM plugin_servers ps WHERE ps.plugin_id = p.id AND ps.deleted IS FALSE) AS server_count,
  (SELECT count(*) FROM plugin_assignments pa WHERE pa.plugin_id = p.id) AS assignment_count
FROM plugins p
WHERE p.organization_id = @organization_id
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
  AND deleted IS FALSE
RETURNING *;

-- name: DeletePlugin :exec
UPDATE plugins
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: AddPluginServer :one
INSERT INTO plugin_servers (plugin_id, toolset_id, registry_id, registry_server_specifier, external_url, display_name, policy, sort_order)
VALUES (
  @plugin_id,
  sqlc.narg('toolset_id'),
  sqlc.narg('registry_id'),
  sqlc.narg('registry_server_specifier'),
  sqlc.narg('external_url'),
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
ON CONFLICT (plugin_id, organization_id, principal_urn) DO NOTHING
RETURNING *;

-- name: ListPluginAssignments :many
SELECT *
FROM plugin_assignments
WHERE plugin_id = @plugin_id;

-- name: RemovePluginAssignment :execrows
DELETE FROM plugin_assignments
WHERE id = @id
  AND plugin_id = @plugin_id;

-- name: RemoveAllPluginAssignments :execrows
DELETE FROM plugin_assignments
WHERE plugin_id = @plugin_id;

-- name: GetGitHubConnection :one
SELECT *
FROM plugin_github_connections
WHERE organization_id = @organization_id;

-- name: UpsertGitHubConnection :one
INSERT INTO plugin_github_connections (organization_id, installation_id, repo_owner, repo_name)
VALUES (@organization_id, @installation_id, @repo_owner, @repo_name)
ON CONFLICT (organization_id) DO UPDATE SET
  installation_id = EXCLUDED.installation_id,
  repo_owner = EXCLUDED.repo_owner,
  repo_name = EXCLUDED.repo_name,
  updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteGitHubConnection :exec
DELETE FROM plugin_github_connections
WHERE organization_id = @organization_id;

-- name: ListPluginsWithServersForOrg :many
-- Used during plugin generation: returns all active plugin servers joined with
-- their parent plugin and optional toolset mcp_slug for URL construction.
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
  ps.registry_id,
  ps.registry_server_specifier,
  ps.external_url,
  t.mcp_slug AS toolset_mcp_slug
FROM plugins p
JOIN plugin_servers ps ON ps.plugin_id = p.id AND ps.deleted IS FALSE
LEFT JOIN toolsets t ON t.id = ps.toolset_id AND t.deleted IS FALSE
WHERE p.organization_id = @organization_id
  AND p.deleted IS FALSE
ORDER BY p.slug, ps.sort_order ASC;

-- name: ListPluginAssignmentsForOrg :many
-- Used during plugin generation: returns all assignments for an org's plugins.
SELECT
  pa.plugin_id,
  pa.principal_urn
FROM plugin_assignments pa
JOIN plugins p ON p.id = pa.plugin_id AND p.deleted IS FALSE
WHERE pa.organization_id = @organization_id;

-- name: GetOrganizationName :one
SELECT name FROM organization_metadata WHERE id = @id;
